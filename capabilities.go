package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// git runs Git without a shell and keeps optional index refreshes disabled.
func (s *Service) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitPath := s.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	argv := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, gitPath, argv...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return out, nil
}

func (s *Service) repositoryRoot(ctx context.Context, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		input = "."
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	out, err := s.git(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%q is not a Git repository: %w", input, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("%q is not a Git repository", input)
	}
	return filepath.Clean(root), nil
}

func cleanRepoPath(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	// Accept slash-separated paths consistently on every OS, and reject
	// absolute paths and traversal before passing a pathspec to Git.
	p := strings.ReplaceAll(strings.TrimSpace(input), `\`, "/")
	if strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') {
		return "", fmt.Errorf("path must be relative to the repository root")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must not escape the repository root")
	}
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

func (s *Service) Inspect(ctx context.Context, req InspectRequest) (Inspection, error) {
	root, err := s.repositoryRoot(ctx, req.Root)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{Root: root}
	if out, e := s.git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); e == nil {
		result.Branch = strings.TrimSpace(string(out))
	}
	out, err := s.git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Inspection{}, err
	}
	result.Head = strings.TrimSpace(string(out))
	if out, e := s.git(ctx, root, "remote", "get-url", "origin"); e == nil {
		result.Remote = strings.TrimSpace(string(out))
	}
	status, err := s.git(ctx, root, "status", "--porcelain=v1", "--no-renames", "-z")
	if err != nil {
		return Inspection{}, err
	}
	for _, record := range bytes.Split(status, []byte{0}) {
		if len(record) < 2 {
			continue
		}
		x, y := record[0], record[1]
		if x == '?' && y == '?' {
			result.UntrackedFiles++
			continue
		}
		if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
			result.ConflictedFiles++
			continue
		}
		if x != ' ' {
			result.StagedFiles++
		}
		if y != ' ' {
			result.ModifiedFiles++
		}
	}
	result.Dirty = result.StagedFiles+result.ModifiedFiles+result.UntrackedFiles+result.ConflictedFiles > 0
	return result, nil
}

func (s *Service) Files(ctx context.Context, req FilesRequest) (FilesResult, error) {
	root, err := s.repositoryRoot(ctx, req.Root)
	if err != nil {
		return FilesResult{}, err
	}
	path, err := cleanRepoPath(req.Path)
	if err != nil {
		return FilesResult{}, err
	}
	args := []string{"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}
	if path != "" {
		args = append(args, path)
	}
	out, err := s.git(ctx, root, args...)
	if err != nil {
		return FilesResult{}, err
	}
	result := FilesResult{Root: root, Files: make([]File, 0)}
	for _, raw := range bytes.Split(out, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		name := filepath.FromSlash(string(raw))
		full := filepath.Join(root, name)
		info, statErr := os.Lstat(full)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return FilesResult{}, fmt.Errorf("stat %q: %w", string(raw), statErr)
		}
		if info.IsDir() {
			continue
		}
		result.Files = append(result.Files, File{Path: filepath.ToSlash(name), Size: info.Size(), Mode: info.Mode().String()})
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	result.Count = len(result.Files)
	return result, nil
}

func (s *Service) Diff(ctx context.Context, req DiffRequest) (DiffResult, error) {
	root, err := s.repositoryRoot(ctx, req.Root)
	if err != nil {
		return DiffResult{}, err
	}
	limit := req.MaxBytes
	if limit <= 0 {
		limit = DefaultDiffBytes
	}
	args := []string{"diff", "--no-ext-diff", "--no-color", "--no-renames"}
	base := "HEAD (tracked files)"
	if req.Staged {
		args = append(args, "--cached")
		base = "index to HEAD"
	} else {
		args = append(args, "HEAD")
	}
	args = append(args, "--")
	patch, truncated, err := s.gitCapped(ctx, root, limit, args...)
	if err != nil {
		return DiffResult{}, err
	}
	result := DiffResult{Root: root, Base: base, Patch: string(patch), Truncated: truncated}
	result.Files, result.Added, result.Removed = diffCounts(result.Patch)
	return result, nil
}

func (s *Service) Pack(ctx context.Context, req PackRequest) (PackResult, error) {
	maxFiles := req.MaxFiles
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	listed, err := s.Files(ctx, FilesRequest{Root: req.Root, Path: req.Path})
	if err != nil {
		return PackResult{}, err
	}
	result := PackResult{Root: listed.Root, Files: make([]PackedFile, 0), MaxFiles: maxFiles, MaxBytes: maxBytes}
	for _, file := range listed.Files {
		if len(result.Files) >= maxFiles {
			result.Skipped = append(result.Skipped, SkippedFile{Path: file.Path, Reason: "max_files"})
			result.Truncated = true
			continue
		}
		full := filepath.Join(listed.Root, filepath.FromSlash(file.Path))
		info, err := os.Lstat(full)
		if err != nil {
			return PackResult{}, fmt.Errorf("inspect %q: %w", file.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			result.Skipped = append(result.Skipped, SkippedFile{Path: file.Path, Reason: "symlink"})
			continue
		}
		if !info.Mode().IsRegular() {
			result.Skipped = append(result.Skipped, SkippedFile{Path: file.Path, Reason: "not_regular_file"})
			continue
		}
		remaining := maxBytes - result.Bytes
		if info.Size() > int64(remaining) {
			result.Skipped = append(result.Skipped, SkippedFile{Path: file.Path, Reason: "max_bytes"})
			result.Truncated = true
			continue
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return PackResult{}, fmt.Errorf("read %q: %w", file.Path, err)
		}
		if bytes.IndexByte(body, 0) >= 0 {
			result.Skipped = append(result.Skipped, SkippedFile{Path: file.Path, Reason: "binary"})
			continue
		}
		result.Files = append(result.Files, PackedFile{Path: file.Path, Content: string(body), Bytes: len(body)})
		result.Bytes += len(body)
	}
	return result, nil
}

func (s *Service) gitCapped(ctx context.Context, dir string, limit int, args ...string) ([]byte, bool, error) {
	gitPath := s.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	cmd := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out cappedBuffer
	out.limit = limit
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, false, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return out.buf.Bytes(), out.truncated, nil
}

type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func diffCounts(patch string) (files, added, removed int) {
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			removed++
		}
	}
	return
}
