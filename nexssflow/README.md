# NexssFlow repository bundle

This folder contains the `nexssflow` Go package that Flow loads for `@require github.com/nexssp/repo`. It adapts the typed root `Provider` to five actions and embeds the public pipeline definitions in `nflows/repo.nflow`.

A consumer imports the bundle package and its definition with:

```nflow
@require github.com/nexssp/repo v0.1.0 as repo
@include repo:nflows/repo.nflow AS repo

@{ repository: "owner/name" } -> repo.pipeline.github_inspect
```

The bundle exports `repo.pipeline.inspect`, `repo.pipeline.github_inspect`, `repo.pipeline.files`, `repo.pipeline.diff`, and `repo.pipeline.pack`. `github_inspect` returns GitHub metadata, default-branch head, latest tag/release, and check-run plus legacy commit-status results. Inputs and response fields are described in the root README. Local file packing is bounded by default to 200 files / 1 MiB. See [`../examples/flow/`](../examples/flow/) for local and GitHub examples using the unpublished module.
