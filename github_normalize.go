package repo

// GitHubRepositoryRef is a validated, canonical GitHub repository reference.
type GitHubRepositoryRef struct {
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

// NormalizeGitHubRepository validates a GitHub repository name or URL and
// returns its canonical owner/name identity without making a network request.
func NormalizeGitHubRepository(input string) (GitHubRepositoryRef, error) {
	owner, name, fullName, err := parseGitHubRepository(input)
	if err != nil {
		return GitHubRepositoryRef{}, err
	}
	return GitHubRepositoryRef{
		Owner: owner, Name: name, FullName: fullName,
		HTMLURL: "https://github.com/" + fullName,
	}, nil
}
