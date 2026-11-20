package repo

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Tag sync states reported by Sync.
const (
	TagSyncStatePushed     = "pushed"      // Same object exists locally and on the remote.
	TagSyncStateLocalOnly  = "local_only"  // Tag exists locally but not on the remote.
	TagSyncStateRemoteOnly = "remote_only" // Tag exists on the remote but not locally.
	TagSyncStateDiverged   = "diverged"    // Same tag name points at different objects.
)

// SyncRequest compares the local branch head and tags with a Git remote. The
// check is read-only: it uses git ls-remote and never fetches or mutates refs.
type SyncRequest struct {
	Root   string `json:"root,omitempty"`
	Remote string `json:"remote,omitempty"` // Remote name; defaults to "origin".
}

// TagSync reports where one tag exists and whether the object IDs agree.
type TagSync struct {
	Name      string `json:"name"`
	LocalSHA  string `json:"local_sha,omitempty"`
	RemoteSHA string `json:"remote_sha,omitempty"`
	State     string `json:"state"`
}

// SyncResult is the local-versus-remote comparison for one repository.
type SyncResult struct {
	Root         string    `json:"root"`
	Remote       string    `json:"remote"`
	RemoteURL    string    `json:"remote_url,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	LocalHead    string    `json:"local_head"`
	RemoteHead   string    `json:"remote_head,omitempty"`
	HeadMatch    bool      `json:"head_match"`
	Ahead        int       `json:"ahead,omitempty"`
	Behind       int       `json:"behind,omitempty"`
	CountsKnown  bool      `json:"counts_known"`
	Tags         []TagSync `json:"tags"`
	UnpushedTags int       `json:"unpushed_tags"`
	Synced       bool      `json:"synced"`
	Warnings     []string  `json:"warnings,omitempty"`
}

// Sync compares the local worktree's branch head and tags with the remote
// without changing anything: remote refs come from git ls-remote, and ahead
// and behind counts are computed only when the remote head exists locally.
func (s *Service) Sync(ctx context.Context, req SyncRequest) (SyncResult, error) {
	root, err := s.repositoryRoot(ctx, req.Root)
	if err != nil {
		return SyncResult{}, err
	}
	remoteName := strings.TrimSpace(req.Remote)
	if remoteName == "" {
		remoteName = "origin"
	}
	result := SyncResult{Root: root, Remote: remoteName}

	remoteURL, err := s.git(ctx, root, "remote", "get-url", remoteName)
	if err != nil {
		return SyncResult{}, fmt.Errorf("remote %q: %w", remoteName, err)
	}
	result.RemoteURL = strings.TrimSpace(string(remoteURL))

	if out, e := s.git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); e == nil {
		result.Branch = strings.TrimSpace(string(out))
	}
	out, err := s.git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return SyncResult{}, err
	}
	result.LocalHead = strings.TrimSpace(string(out))

	refs, truncated, err := s.gitCapped(ctx, root, DefaultRefsBytes, "ls-remote", remoteName)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list remote refs: %w", err)
	}
	if truncated {
		result.Warnings = append(result.Warnings, "remote ref list truncated")
	}
	remoteHeads, remoteTags := parseRemoteRefs(string(refs))

	result.RemoteHead = remoteHeads[result.Branch]
	if result.RemoteHead == "" && result.Branch == "" {
		result.RemoteHead = remoteHeads["HEAD"]
	}
	if result.RemoteHead == "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("branch %q was not found on the remote", result.Branch))
	}
	result.HeadMatch = result.RemoteHead != "" && result.RemoteHead == result.LocalHead

	if result.RemoteHead != "" && result.RemoteHead != result.LocalHead {
		if _, e := s.git(ctx, root, "rev-parse", "--verify", "--quiet", result.RemoteHead+"^{commit}"); e == nil {
			ahead, behind, countErr := s.aheadBehind(ctx, root, result.RemoteHead, result.LocalHead)
			if countErr != nil {
				result.Warnings = append(result.Warnings, countErr.Error())
			} else {
				result.Ahead, result.Behind, result.CountsKnown = ahead, behind, true
			}
		} else {
			result.Warnings = append(result.Warnings, "remote head is not present locally; ahead/behind counts are unavailable")
		}
	}
	if result.HeadMatch {
		result.Ahead, result.Behind, result.CountsKnown = 0, 0, true
	}

	localTags, err := s.localTags(ctx, root)
	if err != nil {
		return SyncResult{}, err
	}
	result.Tags = mergeTags(localTags, remoteTags)
	for _, tag := range result.Tags {
		if tag.State == TagSyncStateLocalOnly || tag.State == TagSyncStateDiverged {
			result.UnpushedTags++
		}
	}
	result.Synced = result.HeadMatch && result.UnpushedTags == 0
	return result, nil
}

// aheadBehind counts commits only reachable from one side. Both revisions
// must already resolve locally.
func (s *Service) aheadBehind(ctx context.Context, root, remoteHead, localHead string) (int, int, error) {
	count := func(spec string) (int, error) {
		out, e := s.git(ctx, root, "rev-list", "--count", spec)
		if e != nil {
			return 0, e
		}
		var n int
		if _, e := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); e != nil {
			return 0, fmt.Errorf("parse rev-list count %q: %w", strings.TrimSpace(string(out)), e)
		}
		return n, nil
	}
	ahead, err := count(remoteHead + ".." + localHead)
	if err != nil {
		return 0, 0, fmt.Errorf("count ahead commits: %w", err)
	}
	behind, err := count(localHead + ".." + remoteHead)
	if err != nil {
		return 0, 0, fmt.Errorf("count behind commits: %w", err)
	}
	return ahead, behind, nil
}

// localTags returns tag names with the object each tag points at, matching the
// object ID that git ls-remote reports for refs/tags/<name>.
func (s *Service) localTags(ctx context.Context, root string) (map[string]string, error) {
	out, err := s.git(ctx, root, "for-each-ref", "refs/tags", "--format=%(refname:strip=2) %(objectname)")
	if err != nil {
		return nil, fmt.Errorf("list local tags: %w", err)
	}
	tags := make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		tags[fields[0]] = fields[1]
	}
	return tags, nil
}

// parseRemoteRefs splits ls-remote output into branch heads (including the
// symbolic HEAD line) and tags. Peeled tag entries are ignored so annotated
// and lightweight tags compare the same object form.
func parseRemoteRefs(output string) (heads, tags map[string]string) {
	heads, tags = make(map[string]string), make(map[string]string)
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sha, ref := fields[0], fields[1]
		switch {
		case ref == "HEAD":
			heads["HEAD"] = sha
		case strings.HasPrefix(ref, "refs/heads/"):
			heads[strings.TrimPrefix(ref, "refs/heads/")] = sha
		case strings.HasPrefix(ref, "refs/tags/") && !strings.HasSuffix(ref, "^{}"):
			tags[strings.TrimPrefix(ref, "refs/tags/")] = sha
		}
	}
	return heads, tags
}

// mergeTags joins local and remote tags by name and classifies each side.
func mergeTags(local, remote map[string]string) []TagSync {
	names := make([]string, 0, len(local)+len(remote))
	for name := range local {
		names = append(names, name)
	}
	for name := range remote {
		if _, ok := local[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	merged := make([]TagSync, 0, len(names))
	for _, name := range names {
		localSHA, hasLocal := local[name]
		remoteSHA, hasRemote := remote[name]
		tag := TagSync{Name: name, LocalSHA: localSHA, RemoteSHA: remoteSHA}
		switch {
		case hasLocal && !hasRemote:
			tag.State = TagSyncStateLocalOnly
		case !hasLocal && hasRemote:
			tag.State = TagSyncStateRemoteOnly
		case localSHA == remoteSHA:
			tag.State = TagSyncStatePushed
		default:
			tag.State = TagSyncStateDiverged
		}
		merged = append(merged, tag)
	}
	return merged
}
