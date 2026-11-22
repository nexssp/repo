# Nexss Repository Capabilities

A read-only Go module for local Git and GitHub repository inspection. Its typed `Provider` is the common contract for thin consumers: `repopeek` uses GitHub inspection, while `srcpack` can reuse local `Files`, `ChangedPaths`, `Diff`, and `Pack` without reimplementing Git traversal or safety limits. The supplemental `UntrackedPathsProvider` lists non-ignored untracked paths without expanding the existing `Provider` interface. `Sync` compares local branch heads and tags with a remote so publishing workflows can verify that everything is pushed. Nexss Flow loads the same provider through the `nexssflow/` bundle and its embedded `.nflow` definitions.

## Quick start

```sh
go run ./cmd/repo inspect --root .
go run ./cmd/repo files --root . --path src
go run ./cmd/repo diff --root .
go run ./cmd/repo pack --root . --max-files 100 --max-bytes 500000
go run ./cmd/repo sync --root . --remote origin

go run ./cmd/repo github nexssp/flow
go run ./cmd/repopeek nexssp/flow
```

`repo` emits JSON on stdout and diagnostics on stderr. Local commands are read-only. `files` returns tracked and non-ignored untracked files. `diff` reports tracked changes against `HEAD` by default, or staged changes with `--staged`; it does not synthesize patches for untracked files. `pack` reads text files only, skips symlinks and binary files, and enforces file/byte limits. `sync` reads remote refs with `git ls-remote` — without fetching — and reports whether the local `HEAD` matches the remote branch, ahead/behind counts when both sides are known locally, and whether every local tag exists on the remote with the same object ID. `repopeek` accepts one or more `owner/name` values or GitHub repository URLs; `-f repos.txt` reads a list, one repository per line, with blank lines and `#` comments ignored.

## GitHub data

`Provider.InspectGitHub` returns one typed result with repository metadata (name, description, URL, visibility, default branch, stars, forks, language, archive state and last-push time), the default-branch head commit (SHA, date, subject and URL), the latest tag and latest full release when available, and CI/check information. Set `GitHubRequest.IncludeGoMod` to also parse every requirement in the default branch's `go.mod`; a missing file is normal, while fetch/decode problems are reported as warnings.

The latest tag is selected by commit date from up to the first ten tags and includes its SHA, date and whether it points at HEAD. The release field comes from GitHub's latest-release endpoint, which reports the latest published non-draft, non-prerelease release. CI combines Actions check runs with legacy commit statuses. `ci.state` is `PASSED`, `FAILED`, `RUNNING`, `NO CI` or `UNKNOWN`; the response also includes the check/status details, truncation flag and warnings when an endpoint could not be read.

Public repositories can be queried without authentication, subject to GitHub's unauthenticated rate limit. For higher limits or private repositories, set `GITHUB_TOKEN` or `GH_TOKEN`, or authenticate with `gh auth login`; the provider uses `gh auth token` as a fallback and never prints the token. The REST client uses GitHub's current API-version header (`2026-03-10`). See GitHub's docs for [repository metadata](https://docs.github.com/rest/repos/repos#get-a-repository), [tags](https://docs.github.com/rest/repos/repos#list-repository-tags), [latest release](https://docs.github.com/rest/releases/releases#get-the-latest-release), [check runs](https://docs.github.com/rest/checks/runs#list-check-runs-for-a-git-reference) and [combined commit status](https://docs.github.com/rest/commits/statuses#get-the-combined-status-for-a-specific-reference).

The concrete `*repo.Service` also provides `InspectGitHubTags` for a tags-only lookup and `LatestGitHubSemverTag` for the highest Go-module-valid semver among the first 100 GitHub tags. These methods share the provider's token lookup and HTTP client; they are not added to the `Provider` interface, so existing provider implementations remain source-compatible. `NormalizeGitHubRepository` validates common GitHub URL/name forms and returns a canonical `owner/name` reference without network access. `GitHubModuleVersionStatuses` compares parsed `go.mod` requirements with caller-supplied latest versions using the RepoPeek-compatible major/minor badge policy.

## Go API

Import `github.com/nexssp/repo` and use the same provider for local and GitHub operations:

```go
provider := repo.New()
summary, err := provider.Inspect(ctx, repo.InspectRequest{Root: "."})
files, err := provider.Files(ctx, repo.FilesRequest{Root: ".", Path: "src"})
untracked, err := provider.UntrackedPaths(ctx, repo.UntrackedPathsRequest{Root: "."})
diff, err := provider.Diff(ctx, repo.DiffRequest{Root: "."})
changes, err := provider.ChangedPaths(ctx, repo.ChangedPathsRequest{Root: ".", Modified: true, Staged: true})
pack, err := provider.Pack(ctx, repo.PackRequest{Root: ".", MaxFiles: 100, MaxBytes: 500_000})
sync, err := provider.Sync(ctx, repo.SyncRequest{Root: ".", Remote: "origin"})
github, err := provider.InspectGitHub(ctx, repo.GitHubRequest{Repository: "owner/name", IncludeGoMod: true})
tags, err := provider.InspectGitHubTags(ctx, repo.GitHubRequest{Repository: "owner/name"})
latest, err := provider.LatestGitHubSemverTag(ctx, repo.GitHubRequest{Repository: "nexssp/kernel"})
```

`UntrackedPaths` returns sorted, unique absolute paths for non-ignored untracked files. `ChangedPaths` returns sorted, unique absolute paths; `Modified` includes tracked changes against `HEAD` and non-ignored untracked files; `Since` compares the working tree with a base revision and takes precedence over `Modified`; `Staged` adds index-to-`HEAD` paths. The path lists use NUL-delimited Git output, so filenames containing spaces or newlines are preserved.

`SyncResult.Synced` is true when the local head matches the remote branch head and no local tag is missing or diverged on the remote; shallow or partial clones get `counts_known=false` and a warning instead of guessed ahead/behind numbers.

`Provider` is the small interface intended for tests and consumers. Git is executed with argument vectors (never through a shell), optional index refresh is disabled, GitHub requests are read-only, and response bodies and timeouts are bounded.

## Nexss Flow

Flow loads the module's `nexssflow` Go package for bundle ID `repo`; it embeds the definitions from `nexssflow/nflows/repo.nflow`. A published consumer can use:

```nflow
@require github.com/nexssp/repo v0.1.0 as repo
@include repo:nflows/repo.nflow AS repo

@{ repository: "owner/name" } -> repo.pipeline.github_inspect
```

Replace `v0.1.0` with the release version you use. The exported pipelines are:

| Pipeline | Action | Input fields |
| --- | --- | --- |
| `repo.pipeline.inspect` | `repo.inspect` | `root` |
| `repo.pipeline.github_inspect` | `repo.github_inspect` | `repository`, optional `include_go_mod` |
| `repo.pipeline.github_tags` | `repo.github_tags` | `repository` |
| `repo.pipeline.github_normalize` | `repo.github_normalize` | `repository` |
| `repo.pipeline.github_latest_semver` | `repo.github_latest_semver` | `repository` |
| `repo.pipeline.github_module_versions` | `repo.github_module_versions` | `requirements`, `latest_versions` |
| `repo.pipeline.files` | `repo.files` | `root`, optional `path` |
| `repo.pipeline.untracked_paths` | `repo.untracked_paths` | `root` |
| `repo.pipeline.diff` | `repo.diff` | `root`, optional `staged`, `max_bytes` |
| `repo.pipeline.pack` | `repo.pack` | `root`, optional `path`, `max_files`, `max_bytes` |
| `repo.pipeline.sync` | `repo.sync` | `root`, optional `remote` (defaults to `origin`) |

For immediate local use before publishing, [`examples/flow/`](examples/flow/) statically links the bundle and includes both local and GitHub examples.

## Consumer boundaries

`repopeek` and `srcpack` remain separate applications but have one shared Go capability contract. `cmd/repopeek` is a thin GitHub JSON frontend backed by `Provider.InspectGitHub`; `srcpack` can use `Provider.Files`, `Provider.ChangedPaths`, `Provider.Diff` and `Provider.Pack` while retaining its own selectors, language analysis, rendering and CLI policy. The available srcpack source tree was left untouched because it has no Git metadata; no consumer migration was silently claimed. The first pass intentionally excludes persistent API caching, curated package inventories, dependency-version dashboards, terminal themes, Bitbucket and Scion. No `nexss_tooler` checkout was available here.
