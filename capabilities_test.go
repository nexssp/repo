package repo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestCapabilitiesReadOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, root, "config", "user.name", "Repository Test")
	writeTestFile(t, root, "README.md", "hello\n")
	writeTestFile(t, root, "src/main.go", "package main\n")
	gitTest(t, root, "add", "README.md", "src/main.go")
	gitTest(t, root, "commit", "-m", "initial")
	writeTestFile(t, root, "README.md", "hello changed\n")
	writeTestFile(t, root, "notes.txt", "untracked\n")

	service := New()
	ctx := context.Background()
	inspection, err := service.Inspect(ctx, InspectRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Branch != "main" || inspection.Head == "" || !inspection.Dirty || inspection.ModifiedFiles != 1 || inspection.UntrackedFiles != 1 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}

	files, err := service.Files(ctx, FilesRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if files.Count != 3 {
		t.Fatalf("got %d files, want 3: %+v", files.Count, files.Files)
	}
	if _, err := service.Files(ctx, FilesRequest{Root: root, Path: "../outside"}); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}

	diff, err := service.Diff(ctx, DiffRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "hello changed") || diff.Files != 1 || diff.Added != 1 || diff.Removed != 1 {
		t.Fatalf("unexpected diff: %+v", diff)
	}

	packed, err := service.Pack(ctx, PackRequest{Root: root, MaxFiles: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(packed.Files) != 2 || !packed.Truncated || len(packed.Skipped) != 1 {
		t.Fatalf("unexpected bounded pack: %+v", packed)
	}
}

func TestUntrackedPathsListsOnlyNonIgnoredPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, root, "config", "user.name", "Repository Test")
	writeTestFile(t, root, ".gitignore", "ignored.go\n")
	writeTestFile(t, root, "tracked.go", "package example\n")
	gitTest(t, root, "add", "-A")
	gitTest(t, root, "commit", "-m", "initial")

	specialName := "line\nbreak.go"
	if runtime.GOOS == "windows" {
		specialName = "line-break.go"
	}
	writeTestFile(t, root, specialName, "package example\n")
	writeTestFile(t, root, "untracked.go", "package example\n")
	writeTestFile(t, root, "ignored.go", "package example\n")

	result, err := New().UntrackedPaths(context.Background(), UntrackedPathsRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	want := absoluteTestPaths(root, specialName, "untracked.go")
	if result.Root != root || !equalPaths(result.Paths, want) {
		t.Fatalf("untracked paths = %+v, want root %q and paths %#v", result, root, want)
	}
}

func TestDiffRespectsMaxBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init")
	gitTest(t, root, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, root, "config", "user.name", "Repository Test")
	writeTestFile(t, root, "large.txt", strings.Repeat("before\n", 4000))
	gitTest(t, root, "add", "large.txt")
	gitTest(t, root, "commit", "-m", "initial")
	writeTestFile(t, root, "large.txt", strings.Repeat("after\n", 4000))

	result, err := New().Diff(context.Background(), DiffRequest{Root: root, MaxBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Patch) > 128 {
		t.Fatalf("diff was not bounded: truncated=%v bytes=%d", result.Truncated, len(result.Patch))
	}
}

func TestPackSkipsBinaryAndSymlinks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init")
	writeTestFile(t, root, "text.txt", "text")
	writeTestFile(t, root, "image.bin", "\x00binary")
	if err := os.Symlink("text.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	result, err := New().Pack(context.Background(), PackRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || len(result.Skipped) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCleanRepoPath(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", false}, {"docs", false}, {"docs/../src", false}, {"../outside", true}, {"/etc/passwd", true}, {`C:\\outside`, true},
	}
	for _, tc := range cases {
		_, err := cleanRepoPath(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("cleanRepoPath(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestChangedPathsPreservesGitFilterSemantics(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.email", "repo-test@example.invalid")
	gitTest(t, root, "config", "user.name", "Repository Test")
	writeTestFile(t, root, ".gitignore", "ignored.go\n")
	for _, name := range []string{"modified.go", "staged.go", "deleted.go", "keep.go"} {
		writeTestFile(t, root, name, "package example\n")
	}
	gitTest(t, root, "add", "-A")
	gitTest(t, root, "commit", "-m", "initial")
	writeTestFile(t, root, "since-only.go", "package example\n")
	gitTest(t, root, "add", "since-only.go")
	gitTest(t, root, "commit", "-m", "since boundary")

	writeTestFile(t, root, "modified.go", "package example\n// modified\n")
	writeTestFile(t, root, "staged.go", "package example\n// staged\n")
	gitTest(t, root, "add", "staged.go")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	specialName := "line\nbreak.go"
	if runtime.GOOS == "windows" {
		specialName = "line-break.go"
	}
	writeTestFile(t, root, specialName, "package example\n")
	writeTestFile(t, root, "untracked.go", "package example\n")
	writeTestFile(t, root, "ignored.go", "package example\n")

	service := New()
	ctx := context.Background()
	modified, err := service.ChangedPaths(ctx, ChangedPathsRequest{Root: root, Modified: true})
	if err != nil {
		t.Fatal(err)
	}
	if modified.Root != root || !equalPaths(modified.Paths, absoluteTestPaths(root, "deleted.go", "modified.go", "staged.go", specialName, "untracked.go")) {
		t.Fatalf("modified paths = %#v, want tracked changes plus non-ignored untracked paths", modified)
	}

	staged, err := service.ChangedPaths(ctx, ChangedPathsRequest{Root: root, Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	if !equalPaths(staged.Paths, absoluteTestPaths(root, "staged.go")) {
		t.Fatalf("staged paths = %#v, want only staged.go", staged.Paths)
	}

	since, err := service.ChangedPaths(ctx, ChangedPathsRequest{Root: root, Since: "HEAD^", Modified: true, Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	if !equalPaths(since.Paths, absoluteTestPaths(root, "deleted.go", "modified.go", "since-only.go", "staged.go")) {
		t.Fatalf("since paths = %#v, want since and staged paths without untracked files", since.Paths)
	}

	empty, err := service.ChangedPaths(ctx, ChangedPathsRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Paths == nil || len(empty.Paths) != 0 {
		t.Fatalf("empty filter paths = %#v, want a non-nil empty slice", empty.Paths)
	}
}

func TestChangedPathsHandlesUnbornRepositoryAndErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
	root := t.TempDir()
	gitTest(t, root, "init")
	writeTestFile(t, root, "untracked.go", "package example\n")
	result, err := New().ChangedPaths(context.Background(), ChangedPathsRequest{Root: root, Modified: true})
	if err != nil {
		t.Fatal(err)
	}
	if !equalPaths(result.Paths, absoluteTestPaths(root, "untracked.go")) {
		t.Fatalf("unborn repository paths = %#v, want untracked.go", result.Paths)
	}

	if _, err := New().ChangedPaths(context.Background(), ChangedPathsRequest{Root: root, Since: "does-not-exist"}); err == nil {
		t.Fatal("expected an invalid since revision to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().ChangedPaths(ctx, ChangedPathsRequest{Root: root, Modified: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context.Canceled", err)
	}
}

func absoluteTestPaths(root string, names ...string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(root, name))
	}
	sort.Strings(paths)
	return paths
}

func equalPaths(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func gitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
