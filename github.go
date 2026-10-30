package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubAPIVersion      = "2026-03-10"
	githubUserAgent       = "nexssp-repo"
	githubBodyLimit       = 4 << 20
	githubTagCandidates   = 10
	githubTagConcurrency  = 4
	githubAuthLookupLimit = 2 * time.Second
)

// GitHubAPIError describes an HTTP error returned by the GitHub REST API.
type GitHubAPIError struct {
	StatusCode int
	Message    string
}

func (e *GitHubAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("GitHub API returned HTTP %d: %s", e.StatusCode, e.Message)
}

func (s *Service) InspectGitHub(ctx context.Context, req GitHubRequest) (GitHubInspection, error) {
	owner, name, slug, err := parseGitHubRepository(req.Repository)
	if err != nil {
		return GitHubInspection{}, err
	}
	token := s.githubToken(ctx)
	repoPath := githubPath("repos", owner, name)

	var rawRepo struct {
		FullName        string `json:"full_name"`
		HTMLURL         string `json:"html_url"`
		Description     string `json:"description"`
		DefaultBranch   string `json:"default_branch"`
		Visibility      string `json:"visibility"`
		Private         bool   `json:"private"`
		Archived        bool   `json:"archived"`
		StargazersCount int    `json:"stargazers_count"`
		ForksCount      int    `json:"forks_count"`
		Language        string `json:"language"`
		PushedAt        string `json:"pushed_at"`
	}
	if _, err := s.githubGetJSON(ctx, token, repoPath, &rawRepo); err != nil {
		return GitHubInspection{}, fmt.Errorf("inspect GitHub repository %s: %w", slug, err)
	}
	if rawRepo.FullName == "" {
		rawRepo.FullName = slug
	}
	result := GitHubInspection{
		Repository: GitHubRepository{
			FullName: rawRepo.FullName, HTMLURL: rawRepo.HTMLURL, Description: rawRepo.Description,
			DefaultBranch: rawRepo.DefaultBranch, Visibility: rawRepo.Visibility,
			Private: rawRepo.Private, Archived: rawRepo.Archived,
			Stars: rawRepo.StargazersCount, Forks: rawRepo.ForksCount,
			Language: rawRepo.Language, PushedAt: rawRepo.PushedAt,
		},
	}
	if rawRepo.DefaultBranch == "" {
		return result, fmt.Errorf("inspect GitHub repository %s: default branch is empty", slug)
	}

	var rawCommit struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message   string `json:"message"`
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	commitPath := githubPath("repos", owner, name, "commits", rawRepo.DefaultBranch)
	if _, err := s.githubGetJSON(ctx, token, commitPath, &rawCommit); err != nil {
		return result, fmt.Errorf("read GitHub head for %s: %w", slug, err)
	}
	result.Head = GitHubCommit{
		SHA: rawCommit.SHA, Date: rawCommit.Commit.Committer.Date,
		Subject: strings.SplitN(rawCommit.Commit.Message, "\n", 2)[0], HTMLURL: rawCommit.HTMLURL,
	}
	if result.Head.SHA == "" {
		return result, fmt.Errorf("read GitHub head for %s: response omitted commit SHA", slug)
	}

	result.CI = s.inspectGitHubCI(ctx, token, owner, name, result.Head.SHA)
	result.LatestTag, result.Warnings = s.latestGitHubTag(ctx, token, owner, name, result.Head.SHA)
	result.LatestRelease, result.Warnings = s.latestGitHubRelease(ctx, token, owner, name, result.Warnings)
	return result, nil
}

func (s *Service) inspectGitHubCI(ctx context.Context, token, owner, name, sha string) GitHubCI {
	ci := GitHubCI{Checks: make([]GitHubCheck, 0), CommitStatuses: make([]GitHubCommitStatus, 0)}
	checkPath := githubPath("repos", owner, name, "commits", sha, "check-runs") + "?per_page=100"
	statusPath := githubPath("repos", owner, name, "commits", sha, "status") + "?per_page=100"
	var checks struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	var statuses struct {
		State      string `json:"state"`
		TotalCount int    `json:"total_count"`
		Statuses   []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	_, checkErr := s.githubGetJSON(ctx, token, checkPath, &checks)
	_, statusErr := s.githubGetJSON(ctx, token, statusPath, &statuses)
	for _, item := range checks.CheckRuns {
		ci.Checks = append(ci.Checks, GitHubCheck{Name: item.Name, Status: item.Status, Conclusion: item.Conclusion, HTMLURL: item.HTMLURL})
	}
	for _, item := range statuses.Statuses {
		ci.CommitStatuses = append(ci.CommitStatuses, GitHubCommitStatus{
			Context: item.Context, State: item.State, Description: item.Description, TargetURL: item.TargetURL,
		})
	}
	ci.Truncated = checks.TotalCount > len(checks.CheckRuns) || statuses.TotalCount > len(statuses.Statuses)
	if checkErr != nil {
		ci.Warnings = append(ci.Warnings, "check runs: "+checkErr.Error())
	}
	if statusErr != nil {
		ci.Warnings = append(ci.Warnings, "commit statuses: "+statusErr.Error())
	}
	ci.State = summarizeGitHubCI(checks.TotalCount, ci.Checks, statuses.TotalCount, statuses.State, checkErr == nil, statusErr == nil, ci.Truncated)
	return ci
}

func summarizeGitHubCI(checkCount int, checks []GitHubCheck, statusCount int, combinedStatus string, checksAvailable, statusesAvailable, truncated bool) string {
	failed, running := false, false
	for _, check := range checks {
		if check.Status != "completed" {
			running = true
			continue
		}
		switch check.Conclusion {
		case "success", "skipped", "neutral":
		default:
			failed = true
		}
	}
	if statusCount > 0 {
		switch combinedStatus {
		case "failure", "error":
			failed = true
		case "pending":
			running = true
		}
	}
	switch {
	case failed:
		return GitHubCIStateFailed
	case running:
		return GitHubCIStateRunning
	case truncated:
		return GitHubCIStateUnknown
	case (checkCount > 0 || statusCount > 0) && checksAvailable && statusesAvailable:
		return GitHubCIStatePassed
	case checkCount == 0 && statusCount == 0 && checksAvailable && statusesAvailable:
		return GitHubCIStateNoCI
	default:
		return GitHubCIStateUnknown
	}
}

func (s *Service) latestGitHubTag(ctx context.Context, token, owner, name, headSHA string) (*GitHubTag, []string) {
	var tags []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	tagPath := githubPath("repos", owner, name, "tags") + "?per_page=" + strconv.Itoa(githubTagCandidates)
	if _, err := s.githubGetJSON(ctx, token, tagPath, &tags); err != nil {
		return nil, []string{"tags: " + err.Error()}
	}
	if len(tags) == 0 {
		return nil, nil
	}

	type candidate struct {
		date      string
		attempted bool
		err       error
	}
	candidates := make([]candidate, len(tags))
	attempted := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, githubTagConcurrency)
	for i, tag := range tags {
		if tag.Commit.SHA == "" {
			continue
		}
		candidates[i].attempted = true
		attempted++
		wg.Add(1)
		go func(i int, sha string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var commit struct {
				Commit struct {
					Committer struct {
						Date string `json:"date"`
					} `json:"committer"`
				} `json:"commit"`
			}
			if _, err := s.githubGetJSON(ctx, token, githubPath("repos", owner, name, "commits", sha), &commit); err != nil {
				candidates[i].err = err
				return
			}
			candidates[i].date = commit.Commit.Committer.Date
		}(i, tag.Commit.SHA)
	}
	wg.Wait()

	var warnings []string
	dateFailures := 0
	for _, candidate := range candidates {
		if candidate.attempted && (candidate.err != nil || candidate.date == "") {
			dateFailures++
		}
	}
	if dateFailures > 0 {
		warnings = append(warnings, fmt.Sprintf("tag dates: %d of %d candidate commit lookups failed or omitted dates", dateFailures, attempted))
	}
	best := 0
	bestTime, bestTimeOK := parseGitHubDate(candidates[0].date)
	for i := 1; i < len(candidates); i++ {
		date := candidates[i].date
		parsed, ok := parseGitHubDate(date)
		if (ok && (!bestTimeOK || parsed.After(bestTime))) || (!ok && !bestTimeOK && date > candidates[best].date) {
			best, bestTime, bestTimeOK = i, parsed, ok
		}
	}
	tag := tags[best]
	if tag.Name == "" {
		return nil, warnings
	}
	return &GitHubTag{Name: tag.Name, SHA: tag.Commit.SHA, Date: candidates[best].date, OnHead: tag.Commit.SHA != "" && tag.Commit.SHA == headSHA}, warnings
}

func (s *Service) latestGitHubRelease(ctx context.Context, token, owner, name string, warnings []string) (*GitHubRelease, []string) {
	var release struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Prerelease  bool   `json:"prerelease"`
	}
	_, err := s.githubGetJSON(ctx, token, githubPath("repos", owner, name, "releases", "latest"), &release)
	if err != nil {
		var apiErr *GitHubAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, warnings
		}
		return nil, append(warnings, "latest release: "+err.Error())
	}
	if release.TagName == "" {
		return nil, warnings
	}
	return &GitHubRelease{
		TagName: release.TagName, Name: release.Name, HTMLURL: release.HTMLURL,
		PublishedAt: release.PublishedAt, Prerelease: release.Prerelease,
	}, warnings
}

func parseGitHubDate(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func (s *Service) githubGetJSON(ctx context.Context, token, path string, target any) (int, error) {
	base := strings.TrimRight(strings.TrimSpace(s.GitHub.APIBaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return 0, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", githubUserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := s.GitHub.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GitHub request %s: %w", path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, githubBodyLimit+1))
	if err != nil {
		return response.StatusCode, fmt.Errorf("read GitHub response: %w", err)
	}
	if len(body) > githubBodyLimit {
		return response.StatusCode, fmt.Errorf("GitHub response exceeded %d-byte limit", githubBodyLimit)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &payload)
		message := payload.Message
		if response.Header.Get("X-RateLimit-Remaining") == "0" {
			if reset := response.Header.Get("X-RateLimit-Reset"); reset != "" {
				message += " (rate limit resets at " + reset + ")"
			}
		}
		return response.StatusCode, &GitHubAPIError{StatusCode: response.StatusCode, Message: message}
	}
	if target == nil || len(body) == 0 {
		return response.StatusCode, nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return response.StatusCode, fmt.Errorf("decode GitHub response: %w", err)
	}
	return response.StatusCode, nil
}

func (s *Service) githubToken(ctx context.Context) string {
	if token := strings.TrimSpace(s.GitHub.Token); token != "" {
		return token
	}
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token
		}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, githubAuthLookupLimit)
	defer cancel()
	output, err := exec.CommandContext(lookupCtx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func githubPath(parts ...string) string {
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "/" + strings.Join(parts, "/")
}

func parseGitHubRepository(input string) (owner, name, slug string, err error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", "", fmt.Errorf("GitHub repository is required (owner/name)")
	}
	switch {
	case strings.HasPrefix(value, "git@github.com:"):
		value = strings.TrimPrefix(value, "git@github.com:")
	case strings.Contains(value, "://"):
		parsed, parseErr := url.Parse(value)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid GitHub repository URL: %w", parseErr)
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "github.com" && host != "www.github.com" {
			return "", "", "", fmt.Errorf("repository URL must use github.com")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", "", "", fmt.Errorf("GitHub repository URL must not include a query or fragment")
		}
		value = parsed.Path
	case strings.HasPrefix(value, "github.com/"):
		value = strings.TrimPrefix(value, "github.com/")
	case strings.HasPrefix(value, "www.github.com/"):
		value = strings.TrimPrefix(value, "www.github.com/")
	}
	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validGitHubPart(parts[0]) || !validGitHubPart(parts[1]) {
		return "", "", "", fmt.Errorf("invalid GitHub repository %q; use owner/name or a github.com repository URL", input)
	}
	return parts[0], parts[1], parts[0] + "/" + parts[1], nil
}

func validGitHubPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
