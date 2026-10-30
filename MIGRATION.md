# Consumer migration map

`github.com/nexssp/repo` is the shared, read-only repository layer. It provides local Git operations and GitHub inspection behind a typed Go `Provider`. The CLI and Flow bundle are thin adapters over that provider.

## repopeek

| Existing behavior | Shared implementation | First-pass decision |
|---|---|---|
| Parse an owner/repo or GitHub URL | `repo.InspectGitHub` accepts either form | Reuse |
| Fetch repo description, default branch, head commit | `GitHubInspection.Repository` and `.Head` | Reuse |
| Find latest tag and release | `.LatestTag` and `.LatestRelease` | Reuse; latest tag is selected by commit date among the first ten returned tags |
| Summarize Actions check runs and legacy commit statuses | `.CI` with `PASSED`, `FAILED`, `RUNNING`, `NO_CI`, or `UNKNOWN`, details, warnings, and truncation status | Reuse; incomplete check lists must not be reported as a clean pass |
| Curated package inventories and dependency-version badges | No matching shared capability yet | Defer; keep these as optional repopeek policy/data if needed later |
| Styled terminal tables, multiple display views, persistent cache | Not part of this first pass | Defer; `repopeek` currently emits JSON and the client may add presentation/cache without changing the provider |

The included `cmd/repopeek` is a thin GitHub JSON consumer. It is a starter, not a complete drop-in replacement for the pasted repopeek dashboard.

## srcpack

| Existing behavior | Shared implementation | Keep in srcpack |
|---|---|---|
| Discover tracked and non-ignored untracked files | `Provider.Files` | Selector grammar, target/profile resolution, ignore policy, and source-language parsing |
| Get changed paths and a bounded tracked diff | `Provider.Diff` | `--modified`, `--staged`, `--since`, and `--diff-only` request semantics and presentation |
| Read bounded text files safely | `Provider.Pack` | Token/fidelity profiles, per-language transformations, prompt composition, tree generation, rendering, clipboard/output policy, and incremental cache |

Migration should happen behind srcpack's existing Go actions: replace duplicated filesystem/Git traversal with calls to `Provider.Files`, `Provider.Diff`, and `Provider.Pack`, then compare outputs on the same fixture repositories. Do not move srcpack's selectors or document renderer into the shared repo package.

No srcpack source was migrated in this handoff. The available copy at `/workspace/srcpack-work/srcpack` has no Git metadata, so it was left unchanged. The shared module is not published yet; for local development, add a temporary `replace github.com/nexssp/repo => ../repo` in the consumer's `go.mod`, or use the module path after publishing a version.

## Nexss Flow

The `nexssflow` package in this module exposes `repo.inspect`, `repo.github_inspect`, `repo.files`, `repo.diff`, and `repo.pack`, plus the corresponding `repo.pipeline.*` definitions in `nexssflow/nflows/repo.nflow`. The checked-in `examples/flow` host links the local bundle so the examples can run before publishing. Published flows can use `@require github.com/nexssp/repo <version> as repo` and include `repo:nflows/repo.nflow`.

## Deliberately deferred

Bitbucket, GitLab, Scion, a generic provider plugin framework, hosted source-file packing, persistent GitHub API caching, package/version inventories, and a full migration of srcpack are out of scope for this first usable slice. The shared contract is small enough to add a provider when a real consumer requires it.
