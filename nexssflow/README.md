# NexssFlow repository bundle

This folder contains the `nexssflow` Go package that Flow loads for `@require github.com/nexssp/repo`. It adapts the shared repository service to ten actions and embeds the public pipeline definitions in `nflows/repo.nflow`.

A consumer imports the bundle package and its definition with:

```nflow
@require github.com/nexssp/repo v0.1.0 as repo
@include repo:nflows/repo.nflow AS repo

@{ repository: "owner/name" } -> repo.pipeline.github_inspect
```

The bundle exports `repo.pipeline.inspect`, `repo.pipeline.github_inspect`, `repo.pipeline.github_tags`, `repo.pipeline.github_normalize`, `repo.pipeline.github_latest_semver`, `repo.pipeline.github_module_versions`, `repo.pipeline.files`, `repo.pipeline.diff`, `repo.pipeline.pack`, and `repo.pipeline.sync`. `github_inspect` accepts optional `include_go_mod: true` and returns parsed requirements in `go_module_requirements`; `github_tags` avoids the commit/CI inspection path; `github_normalize` validates and canonicalizes a GitHub name/URL without a network request; `github_latest_semver` returns the highest valid Go-module semantic-version tag among the first 100 tags; `github_module_versions` compares parsed requirements with caller-supplied latest versions. Inputs and response fields are described in the root README. Local file packing is bounded by default to 200 files / 1 MiB. See [`../examples/flow/`](../examples/flow/) for local and GitHub examples using the unpublished module.
