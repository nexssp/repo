package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newSyncFixture builds a work repository with a bare remote over the local
// filesystem so Sync can be tested without network access.
func newSyncFixture(t *testing.T) (work string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	work = t.TempDir()
	remote := filepath.Join(filepath.Dir(work), "sync-remote.git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTest(t, remote, "init", "--bare", "-b", "main")
	gitTest(t, work, "init", "-b", "main")
	gitTest(t, work, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, work, "config", "user.name", "Repository Test")
	writeTestFile(t, work, "README.md", "hello\n")
	gitTest(t, work, "add", "README.md")
	gitTest(t, work, "commit", "-m", "initial")
	gitTest(t, work, "remote", "add", "origin", remote)
	return work
}

func TestSyncReportsPushedState(t *testing.T) {
	work := newSyncFixture(t)
	gitTest(t, work, "tag", "v1.0.0")
	gitTest(t, work, "push", "origin", "main", "--tags")
	gitTest(t, work, "tag", "v1.1.0") // deliberately unpushed

	result, err := New().Sync(context.Background(), SyncRequest{Root: work})
	if err != nil {
		t.Fatal(err)
	}
	if result.Remote != "origin" || result.RemoteURL == "" {
		t.Fatalf("unexpected remote fields: %+v", result)
	}
	if result.Branch != "main" || result.LocalHead == "" || !result.HeadMatch {
		t.Fatalf("unexpected head comparison: %+v", result)
	}
	if !result.CountsKnown || result.Ahead != 0 || result.Behind != 0 {
		t.Fatalf("expected clean counts, got %+v", result)
	}
	states := map[string]string{}
	for _, tag := range result.Tags {
		states[tag.Name] = tag.State
	}
	if states["v1.0.0"] != TagSyncStatePushed || states["v1.1.0"] != TagSyncStateLocalOnly {
		t.Fatalf("unexpected tag states: %v", states)
	}
	if result.UnpushedTags != 1 || result.Synced {
		t.Fatalf("expected one unpushed tag and synced=false: %+v", result)
	}
}

func TestSyncDetectsAheadAndDivergedTag(t *testing.T) {
	work := newSyncFixture(t)
	gitTest(t, work, "tag", "v1.0.0")
	gitTest(t, work, "push", "origin", "main", "--tags")
	writeTestFile(t, work, "README.md", "second\n")
	gitTest(t, work, "add", "README.md")
	gitTest(t, work, "commit", "-m", "second")
	gitTest(t, work, "tag", "-f", "v1.0.0") // retargeted after the push

	result, err := New().Sync(context.Background(), SyncRequest{Root: work})
	if err != nil {
		t.Fatal(err)
	}
	if result.HeadMatch || result.Ahead != 1 || result.Behind != 0 || !result.CountsKnown {
		t.Fatalf("unexpected ahead comparison: %+v", result)
	}
	states := map[string]string{}
	for _, tag := range result.Tags {
		states[tag.Name] = tag.State
	}
	if states["v1.0.0"] != TagSyncStateDiverged {
		t.Fatalf("expected diverged tag, got %v", states)
	}
	if result.Synced {
		t.Fatal("diverged tag must not report synced")
	}
}

func TestSyncUnknownRemoteHeadStaysUnknown(t *testing.T) {
	work := newSyncFixture(t)
	gitTest(t, work, "push", "origin", "main")
	// A second clone receives the remote state but not future objects.
	clone := t.TempDir()
	gitTest(t, clone, "clone", "--quiet", resultRemoteURL(t, work), clone)
	gitTest(t, clone, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, clone, "config", "user.name", "Repository Test")
	writeTestFile(t, work, "README.md", "newer\n")
	gitTest(t, work, "add", "README.md")
	gitTest(t, work, "commit", "-m", "newer")
	gitTest(t, work, "push", "origin", "main")

	result, err := New().Sync(context.Background(), SyncRequest{Root: clone})
	if err != nil {
		t.Fatal(err)
	}
	if result.HeadMatch {
		t.Fatal("clone should be behind the remote")
	}
	if result.CountsKnown {
		t.Fatalf("counts require the remote head locally, got %+v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected a warning about the missing remote head: %+v", result)
	}
}

func TestSyncRejectsUnknownRemote(t *testing.T) {
	work := newSyncFixture(t)
	if _, err := New().Sync(context.Background(), SyncRequest{Root: work, Remote: "nonexistent"}); err == nil {
		t.Fatal("expected an error for an unknown remote")
	}
}

func TestSyncDetectsRemoteOnlyTag(t *testing.T) {
	work := newSyncFixture(t)
	gitTest(t, work, "tag", "v1.0.0")
	gitTest(t, work, "push", "origin", "main", "--tags")
	clone := t.TempDir()
	gitTest(t, clone, "clone", "--quiet", "--no-tags", resultRemoteURL(t, work), clone)

	result, err := New().Sync(context.Background(), SyncRequest{Root: clone})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, tag := range result.Tags {
		states[tag.Name] = tag.State
	}
	if states["v1.0.0"] != TagSyncStateRemoteOnly {
		t.Fatalf("expected remote_only tag, got %v", states)
	}
}

func TestParseRemoteRefsIgnoresPeeledTags(t *testing.T) {
	heads, tags := parseRemoteRefs("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\tHEAD\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/heads/main\n" +
		"cccccccccccccccccccccccccccccccccccccccc\trefs/tags/v2.0.0\n" +
		"dddddddddddddddddddddddddddddddddddddddd\trefs/tags/v2.0.0^{}\n")
	if heads["HEAD"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || heads["main"] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected heads: %v", heads)
	}
	if len(tags) != 1 || tags["v2.0.0"] != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("unexpected tags: %v", tags)
	}
}

func TestSyncDoesNotMutateRepository(t *testing.T) {
	work := newSyncFixture(t)
	gitTest(t, work, "push", "origin", "main", "--tags")
	before, err := New().Inspect(context.Background(), InspectRequest{Root: work})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New().Sync(context.Background(), SyncRequest{Root: work}); err != nil {
		t.Fatal(err)
	}
	after, err := New().Inspect(context.Background(), InspectRequest{Root: work})
	if err != nil {
		t.Fatal(err)
	}
	if after.Head != before.Head || after.UntrackedFiles != before.UntrackedFiles {
		t.Fatalf("Sync mutated the repository: %+v -> %+v", before, after)
	}
	remotes, err := exec.Command("git", "-C", work, "remote").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(remotes) != "origin\n" {
		t.Fatalf("Sync changed remote configuration: %q", string(remotes))
	}
}

// resultRemoteURL reads the configured origin URL of a fixture repository.
func resultRemoteURL(t *testing.T, work string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", work, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("read origin URL: %v", err)
	}
	return strings.TrimSpace(string(out))
}
