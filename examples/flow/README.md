# Local Flow examples

The example host links the local `nexssflow` bundle. Its `go.mod` uses the sibling `/workspace/flow` checkout so lint and run exercise the current Flow source.

```sh
cd /workspace/nexssp/repo/examples/flow

go run . lint repo_inspect.nflow
go run . lint repo_github_inspect.nflow

go run . run repo_inspect.nflow '{"root":"/workspace/flow"}' --json
go run . run repo_github_inspect.nflow '{"repository":"nexssp/flow"}' --json
```

The GitHub example uses the public REST API and needs no token for a small public request. For private repositories or higher rate limits, set `GITHUB_TOKEN` or `GH_TOKEN`, or use an authenticated `gh` CLI session.

For a published consumer, require `github.com/nexssp/repo` at its release version, then include `repo:nflows/repo.nflow`.
