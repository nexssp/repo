package nexssflow

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/include"
	"github.com/nexssp/flow/extensions/pipeline"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestUntrackedPathsNFlowFixture(t *testing.T) {
	source, err := fs.ReadFile(Bundle(nil).SourceModules, "nflows/untracked_paths_test.nflow")
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	gitNFlowTest(t, root, "init")
	gitNFlowTest(t, root, "config", "user.email", "repo-test@example.invalid")
	gitNFlowTest(t, root, "config", "user.name", "Repository Test")
	writeNFlowTestFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")
	writeNFlowTestFile(t, filepath.Join(root, "tracked.go"), "package example\n")
	gitNFlowTest(t, root, "add", "-A")
	gitNFlowTest(t, root, "commit", "-m", "initial")
	writeNFlowTestFile(t, filepath.Join(root, "untracked.go"), "package example\n")
	writeNFlowTestFile(t, filepath.Join(root, "ignored.go"), "package example\n")

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		include.Bundle(nil),
		pipeline.Bundle(nil),
		require.Bundle(nil),
		Bundle(nil),
	})
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runner.RunSource(t.Context(), cfg, string(source), "untracked_paths_test.nflow", map[string]any{"root": root}, runner.Opts{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("RunSource returned %d; stdout=%q; stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func gitNFlowTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeNFlowTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
