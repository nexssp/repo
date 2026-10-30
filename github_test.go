package repo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInspectGitHub(t *testing.T) {
	const headSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const tagOldSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const tagNewSHA = "cccccccccccccccccccccccccccccccccccccccc"
	var sawAuth, sawAPIVersion atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-token" {
			sawAuth.Store(true)
		}
		if r.Header.Get("X-GitHub-Api-Version") == githubAPIVersion {
			sawAPIVersion.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api":
			writeJSONTest(t, w, map[string]any{
				"full_name": "acme/api", "html_url": "https://github.com/acme/api",
				"description": "Example API", "default_branch": "main", "visibility": "public",
				"private": false, "archived": false, "stargazers_count": 12, "forks_count": 3,
				"language": "Go", "pushed_at": "2026-10-01T12:00:00Z",
			})
		case "/repos/acme/api/commits/main":
			writeJSONTest(t, w, map[string]any{
				"sha": headSHA, "html_url": "https://github.com/acme/api/commit/" + headSHA,
				"commit": map[string]any{
					"message":   "Add shared client\n\nMore details",
					"committer": map[string]string{"date": "2026-10-02T10:11:12Z"},
				},
			})
		case "/repos/acme/api/commits/" + headSHA + "/check-runs":
			if r.URL.Query().Get("per_page") != "100" {
				t.Errorf("check runs per_page = %q, want 100", r.URL.Query().Get("per_page"))
			}
			writeJSONTest(t, w, map[string]any{
				"total_count": 2,
				"check_runs": []map[string]any{
					{"name": "tests", "status": "completed", "conclusion": "success", "html_url": "https://github.com/acme/api/runs/1"},
					{"name": "lint", "status": "in_progress", "conclusion": nil, "html_url": "https://github.com/acme/api/runs/2"},
				},
			})
		case "/repos/acme/api/commits/" + headSHA + "/status":
			writeJSONTest(t, w, map[string]any{
				"state": "success", "total_count": 1,
				"statuses": []map[string]any{{"context": "legacy-ci", "state": "success", "description": "passed", "target_url": "https://ci.example/run/1"}},
			})
		case "/repos/acme/api/tags":
			if r.URL.Query().Get("per_page") != "10" {
				t.Errorf("tags per_page = %q, want 10", r.URL.Query().Get("per_page"))
			}
			writeJSONTest(t, w, []map[string]any{
				{"name": "v1.0.0", "commit": map[string]string{"sha": tagOldSHA}},
				{"name": "v1.1.0", "commit": map[string]string{"sha": tagNewSHA}},
			})
		case "/repos/acme/api/commits/" + tagOldSHA:
			writeJSONTest(t, w, map[string]any{"commit": map[string]any{"committer": map[string]string{"date": "2025-01-01T00:00:00Z"}}})
		case "/repos/acme/api/commits/" + tagNewSHA:
			writeJSONTest(t, w, map[string]any{"commit": map[string]any{"committer": map[string]string{"date": "2026-02-01T00:00:00Z"}}})
		case "/repos/acme/api/releases/latest":
			writeJSONTest(t, w, map[string]any{
				"tag_name": "v1.1.0", "name": "Release 1.1", "html_url": "https://github.com/acme/api/releases/tag/v1.1.0",
				"published_at": "2026-02-01T02:00:00Z", "prerelease": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New()
	service.GitHub = GitHubOptions{APIBaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()}
	result, err := service.InspectGitHub(context.Background(), GitHubRequest{Repository: "https://github.com/acme/api.git"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth.Load() || !sawAPIVersion.Load() {
		t.Fatalf("expected token and API-version headers (auth=%v version=%v)", sawAuth.Load(), sawAPIVersion.Load())
	}
	if result.Repository.FullName != "acme/api" || result.Repository.Description != "Example API" || result.Repository.DefaultBranch != "main" || result.Repository.Stars != 12 {
		t.Fatalf("unexpected repository metadata: %+v", result.Repository)
	}
	if result.Head.SHA != headSHA || result.Head.Subject != "Add shared client" || result.Head.Date != "2026-10-02T10:11:12Z" {
		t.Fatalf("unexpected head: %+v", result.Head)
	}
	if result.LatestTag == nil || result.LatestTag.Name != "v1.1.0" || result.LatestTag.SHA != tagNewSHA || result.LatestTag.Date != "2026-02-01T00:00:00Z" || result.LatestTag.OnHead {
		t.Fatalf("unexpected latest tag: %+v", result.LatestTag)
	}
	if result.LatestRelease == nil || result.LatestRelease.TagName != "v1.1.0" || result.LatestRelease.PublishedAt != "2026-02-01T02:00:00Z" {
		t.Fatalf("unexpected latest release: %+v", result.LatestRelease)
	}
	if result.CI.State != GitHubCIStateRunning || len(result.CI.Checks) != 2 || len(result.CI.CommitStatuses) != 1 || result.CI.Truncated {
		t.Fatalf("unexpected CI summary: %+v", result.CI)
	}
	if len(result.Warnings) != 0 || len(result.CI.Warnings) != 0 {
		t.Fatalf("unexpected warnings: result=%v ci=%v", result.Warnings, result.CI.Warnings)
	}
}

func TestInspectGitHubPartialCheckFailure(t *testing.T) {
	const headSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api":
			writeJSONTest(t, w, map[string]any{"full_name": "acme/api", "default_branch": "main"})
		case "/repos/acme/api/commits/main":
			writeJSONTest(t, w, map[string]any{"sha": headSHA, "commit": map[string]any{"message": "initial"}})
		case "/repos/acme/api/commits/" + headSHA + "/check-runs", "/repos/acme/api/commits/" + headSHA + "/status":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		case "/repos/acme/api/tags":
			writeJSONTest(t, w, []any{})
		case "/repos/acme/api/releases/latest":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New()
	service.GitHub = GitHubOptions{APIBaseURL: server.URL, HTTPClient: server.Client()}
	result, err := service.InspectGitHub(context.Background(), GitHubRequest{Repository: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CI.State != GitHubCIStateUnknown || len(result.CI.Warnings) != 2 {
		t.Fatalf("expected unknown CI with warnings, got %+v", result.CI)
	}
	if result.LatestTag != nil || result.LatestRelease != nil || len(result.Warnings) != 0 {
		t.Fatalf("unexpected optional data: tag=%+v release=%+v warnings=%v", result.LatestTag, result.LatestRelease, result.Warnings)
	}
}

func TestSummarizeGitHubCI(t *testing.T) {
	tests := []struct {
		name                    string
		checkCount, statusCount int
		checks                  []GitHubCheck
		combined                string
		checksAvailable         bool
		statusesAvailable       bool
		truncated               bool
		want                    string
	}{
		{name: "none", checksAvailable: true, statusesAvailable: true, want: GitHubCIStateNoCI},
		{name: "all passed", checkCount: 1, checks: []GitHubCheck{{Status: "completed", Conclusion: "success"}}, checksAvailable: true, statusesAvailable: true, want: GitHubCIStatePassed},
		{name: "legacy passed", statusCount: 1, combined: "success", checksAvailable: true, statusesAvailable: true, want: GitHubCIStatePassed},
		{name: "running", checkCount: 1, checks: []GitHubCheck{{Status: "in_progress"}}, checksAvailable: true, statusesAvailable: true, want: GitHubCIStateRunning},
		{name: "failure wins", checkCount: 2, checks: []GitHubCheck{{Status: "completed", Conclusion: "failure"}, {Status: "in_progress"}}, checksAvailable: true, statusesAvailable: true, want: GitHubCIStateFailed},
		{name: "unknown when check endpoint fails", checksAvailable: false, statusesAvailable: true, want: GitHubCIStateUnknown},
		{name: "unknown when list is truncated", checkCount: 2, checks: []GitHubCheck{{Status: "completed", Conclusion: "success"}}, checksAvailable: true, statusesAvailable: true, truncated: true, want: GitHubCIStateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeGitHubCI(tt.checkCount, tt.checks, tt.statusCount, tt.combined, tt.checksAvailable, tt.statusesAvailable, tt.truncated)
			if got != tt.want {
				t.Errorf("summarizeGitHubCI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLatestGitHubTagWarnsWhenCommitDateCannotBeRead(t *testing.T) {
	const tagSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/tags":
			writeJSONTest(t, w, []map[string]any{{"name": "v1.0.0", "commit": map[string]string{"sha": tagSHA}}})
		case "/repos/acme/api/commits/" + tagSHA:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"not accessible"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New()
	service.GitHub = GitHubOptions{APIBaseURL: server.URL, HTTPClient: server.Client()}
	tag, warnings := service.latestGitHubTag(context.Background(), "", "acme", "api", "head-sha")
	if tag == nil || tag.Name != "v1.0.0" || tag.Date != "" {
		t.Fatalf("expected tag without date, got %+v", tag)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "tag dates") {
		t.Fatalf("expected tag-date warning, got %v", warnings)
	}
}

func TestParseGitHubRepository(t *testing.T) {
	tests := []struct {
		input string
		want  string
		bad   bool
	}{
		{input: "acme/api", want: "acme/api"},
		{input: "github.com/acme/api", want: "acme/api"},
		{input: "https://github.com/acme/api", want: "acme/api"},
		{input: "https://github.com/acme/api.git", want: "acme/api"},
		{input: "git@github.com:acme/api.git", want: "acme/api"},
		{input: "ssh://git@github.com/acme/api.git", want: "acme/api"},
		{input: "https://gitlab.com/acme/api", bad: true},
		{input: "acme/api/tree/main", bad: true},
		{input: "../acme/api", bad: true},
		{input: "", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, _, got, err := parseGitHubRepository(tt.input)
			if tt.bad {
				if err == nil {
					t.Fatalf("expected invalid input %q to fail", tt.input)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseGitHubRepository(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

func writeJSONTest(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}
