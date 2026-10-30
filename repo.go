// Package repo provides read-only, typed repository capabilities shared by CLI
// tools and Nexss Flow bundles.
package repo

import (
	"context"
	"net/http"
	"time"
)

const (
	DefaultMaxFiles  = 200
	DefaultMaxBytes  = 1 << 20
	DefaultDiffBytes = 2 << 20
)

// Provider is the shared repository capability surface used by repopeek and
// srcpack. Implementations must not mutate the target repository or GitHub data.
type Provider interface {
	Inspect(context.Context, InspectRequest) (Inspection, error)
	Files(context.Context, FilesRequest) (FilesResult, error)
	Diff(context.Context, DiffRequest) (DiffResult, error)
	Pack(context.Context, PackRequest) (PackResult, error)
	InspectGitHub(context.Context, GitHubRequest) (GitHubInspection, error)
}

// GitHubOptions configures the GitHub REST API client. Leave APIBaseURL empty to
// use github.com. Token overrides GITHUB_TOKEN/GH_TOKEN and gh auth login.
type GitHubOptions struct {
	APIBaseURL string
	Token      string
	HTTPClient *http.Client
}

// Service implements Provider using Git and the GitHub REST API.
type Service struct {
	GitPath string
	GitHub  GitHubOptions
}

// New returns the default Git-backed and GitHub-backed provider.
func New() *Service {
	return &Service{
		GitPath: "git",
		GitHub: GitHubOptions{
			APIBaseURL: "https://api.github.com",
			HTTPClient: &http.Client{Timeout: 20 * time.Second},
		},
	}
}

type InspectRequest struct {
	Root string `json:"root,omitempty"`
}

type Inspection struct {
	Root            string `json:"root"`
	Branch          string `json:"branch,omitempty"`
	Head            string `json:"head"`
	Remote          string `json:"remote,omitempty"`
	Dirty           bool   `json:"dirty"`
	StagedFiles     int    `json:"staged_files"`
	ModifiedFiles   int    `json:"modified_files"`
	UntrackedFiles  int    `json:"untracked_files"`
	ConflictedFiles int    `json:"conflicted_files"`
}

type FilesRequest struct {
	Root string `json:"root,omitempty"`
	Path string `json:"path,omitempty"` // Optional path relative to the repository root.
}

type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

type FilesResult struct {
	Root  string `json:"root"`
	Files []File `json:"files"`
	Count int    `json:"count"`
}

type DiffRequest struct {
	Root     string `json:"root,omitempty"`
	Staged   bool   `json:"staged,omitempty"` // Show only the index-to-HEAD diff.
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type DiffResult struct {
	Root      string `json:"root"`
	Base      string `json:"base"`
	Patch     string `json:"patch"`
	Files     int    `json:"files_changed"`
	Added     int    `json:"lines_added"`
	Removed   int    `json:"lines_removed"`
	Truncated bool   `json:"truncated"`
}

type PackRequest struct {
	Root     string `json:"root,omitempty"`
	Path     string `json:"path,omitempty"` // Optional subdirectory or file path.
	MaxFiles int    `json:"max_files,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type PackedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Bytes   int    `json:"bytes"`
}

type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type PackResult struct {
	Root      string        `json:"root"`
	Files     []PackedFile  `json:"files"`
	Skipped   []SkippedFile `json:"skipped,omitempty"`
	Bytes     int           `json:"bytes"`
	Truncated bool          `json:"truncated"`
	MaxFiles  int           `json:"max_files"`
	MaxBytes  int           `json:"max_bytes"`
}

// GitHubRequest identifies a public or token-accessible GitHub repository. It
// accepts owner/name, a github.com repository URL, or an SSH clone URL.
type GitHubRequest struct {
	Repository string `json:"repository"`
}

// GitHubInspection contains the narrow metadata set needed by repopeek.
type GitHubInspection struct {
	Repository    GitHubRepository `json:"repository"`
	Head          GitHubCommit     `json:"head"`
	LatestTag     *GitHubTag       `json:"latest_tag,omitempty"`
	LatestRelease *GitHubRelease   `json:"latest_release,omitempty"`
	CI            GitHubCI         `json:"ci"`
	Warnings      []string         `json:"warnings,omitempty"`
}

type GitHubRepository struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description,omitempty"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility,omitempty"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Stars         int    `json:"stars"`
	Forks         int    `json:"forks"`
	Language      string `json:"language,omitempty"`
	PushedAt      string `json:"pushed_at,omitempty"`
}

type GitHubCommit struct {
	SHA     string `json:"sha"`
	Date    string `json:"date,omitempty"`
	Subject string `json:"subject,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

type GitHubTag struct {
	Name   string `json:"name"`
	SHA    string `json:"sha"`
	Date   string `json:"date,omitempty"`
	OnHead bool   `json:"on_head"`
}

type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name,omitempty"`
	HTMLURL     string `json:"html_url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Prerelease  bool   `json:"prerelease"`
}

const (
	GitHubCIStatePassed  = "PASSED"
	GitHubCIStateFailed  = "FAILED"
	GitHubCIStateRunning = "RUNNING"
	GitHubCIStateNoCI    = "NO CI"
	GitHubCIStateUnknown = "UNKNOWN"
)

type GitHubCI struct {
	State          string               `json:"state"`
	Checks         []GitHubCheck        `json:"check_runs,omitempty"`
	CommitStatuses []GitHubCommitStatus `json:"commit_statuses,omitempty"`
	Truncated      bool                 `json:"truncated,omitempty"`
	Warnings       []string             `json:"warnings,omitempty"`
}

type GitHubCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	HTMLURL    string `json:"html_url,omitempty"`
}

type GitHubCommitStatus struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
}
