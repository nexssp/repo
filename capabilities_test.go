package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
