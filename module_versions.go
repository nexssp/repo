package repo

import (
	"strconv"
	"strings"
)

// GitHubModuleLatestVersion pairs a module path with its latest published version.
type GitHubModuleLatestVersion struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// GitHubModuleVersionStatus reports a repository's requirement and its latest
// known version for one tracked module.
type GitHubModuleVersionStatus struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Latest   string `json:"latest"`
	Severity string `json:"severity"`
}

// GitHubModuleVersionRequest compares the caller's go.mod requirements with
// the latest version data supplied for the tracked modules.
type GitHubModuleVersionRequest struct {
	Requirements   []GitHubModuleRequirement   `json:"requirements"`
	LatestVersions []GitHubModuleLatestVersion `json:"latest_versions"`
}

// GitHubModuleVersionResult contains the tracked module versions declared by
// the repository. Modules absent from requirements are omitted.
type GitHubModuleVersionResult struct {
	Modules []GitHubModuleVersionStatus `json:"modules"`
}

// GitHubModuleVersionStatuses compares repository go.mod requirements against
// caller-supplied latest versions. Only modules present in both inputs are returned.
func GitHubModuleVersionStatuses(
	requirements []GitHubModuleRequirement,
	latest []GitHubModuleLatestVersion,
) []GitHubModuleVersionStatus {
	versions := make(map[string]string, len(requirements))
	for _, requirement := range requirements {
		versions[requirement.Path] = requirement.Version
	}

	statuses := make([]GitHubModuleVersionStatus, 0, len(latest))
	for _, current := range latest {
		version, ok := versions[current.Path]
		if !ok {
			continue
		}
		statuses = append(statuses, GitHubModuleVersionStatus{
			Path: current.Path, Version: version, Latest: current.Version,
			Severity: ClassifyGoModuleVersion(version, current.Version),
		})
	}
	return statuses
}

// ClassifyGoModuleVersion matches RepoPeek's established dependency-badge
// policy: patch-only differences are current, one or two minor releases old
// are yellow, and larger minor/major gaps are stale.
func ClassifyGoModuleVersion(current, latest string) string {
	if current == "" || latest == "" {
		return "absent"
	}
	currentMajor, currentMinor, currentOK := parseGoModuleMajorMinor(current)
	latestMajor, latestMinor, latestOK := parseGoModuleMajorMinor(latest)
	if !currentOK || !latestOK {
		return "old"
	}
	if currentMajor != latestMajor {
		if currentMajor < latestMajor {
			return "stale"
		}
		return "current"
	}
	switch diff := latestMinor - currentMinor; {
	case diff <= 0:
		return "current"
	case diff <= 2:
		return "old"
	default:
		return "stale"
	}
}

// CompareGitHubModuleVersions returns the RepoPeek-compatible classifications
// for every tracked module declared by a repository.
func CompareGitHubModuleVersions(req GitHubModuleVersionRequest) GitHubModuleVersionResult {
	return GitHubModuleVersionResult{
		Modules: GitHubModuleVersionStatuses(req.Requirements, req.LatestVersions),
	}
}

func parseGoModuleMajorMinor(version string) (major, minor int, ok bool) {
	if !strings.HasPrefix(version, "v") {
		return 0, 0, false
	}
	trimmed := version[1:]
	if index := strings.IndexAny(trimmed, "-+"); index >= 0 {
		trimmed = trimmed[:index]
	}
	parts := strings.SplitN(trimmed, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}
