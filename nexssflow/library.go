// Package nexssflow exposes the repository capabilities as a registered
// Nexss Flow bundle and embeds the public .nflow pipeline definitions.
package nexssflow

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	repository "github.com/nexssp/repo"
)

const ID = "repo"

//go:embed nflows
var sources embed.FS

func init() { compiler.Register(ID, Bundle) }

// Bundle returns the reusable repository action library and its public .nflow
// definitions. The actions are read-only; no operation mutates the worktree.
func Bundle(_ map[string]string) compiler.Bundle {
	provider := repository.New()
	inspect := action.New("repo.inspect", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		result, err := provider.Inspect(ctx, repository.InspectRequest{Root: stringValue(in, "root")})
		return objectMap(result, err)
	}).Description("Inspect Git repository identity and working-tree status").Build()
	files := action.New("repo.files", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		result, err := provider.Files(ctx, repository.FilesRequest{Root: stringValue(in, "root"), Path: stringValue(in, "path")})
		return objectMap(result, err)
	}).Description("List tracked and non-ignored untracked repository files").Build()
	diff := action.New("repo.diff", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		result, err := provider.Diff(ctx, repository.DiffRequest{
			Root: stringValue(in, "root"), Staged: boolValue(in, "staged"),
			MaxBytes: limitValue(in["max_bytes"], repository.DefaultDiffBytes),
		})
		return objectMap(result, err)
	}).Description("Read the Git diff for tracked files").Build()
	pack := action.New("repo.pack", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		result, err := provider.Pack(ctx, repository.PackRequest{
			Root: stringValue(in, "root"), Path: stringValue(in, "path"),
			MaxFiles: limitValue(in["max_files"], repository.DefaultMaxFiles),
			MaxBytes: limitValue(in["max_bytes"], repository.DefaultMaxBytes),
		})
		return objectMap(result, err)
	}).Description("Collect bounded text-file contents from a Git repository").Build()
	githubInspect := action.New("repo.github_inspect", func(ctx context.Context, in map[string]any) (map[string]any, error) {
		result, err := provider.InspectGitHub(ctx, repository.GitHubRequest{Repository: stringValue(in, "repository")})
		return objectMap(result, err)
	}).Description("Inspect GitHub repository metadata, head, tags/releases, and CI status").Build()

	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name:    ID,
			Actions: []action.AnyAction{inspect, files, diff, pack, githubInspect},
		}},
		SourceModules:   sources,
		ExportedModules: []string{"nflows/repo.nflow"},
	}
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func boolValue(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}

func limitValue(value any, fallback int) int {
	switch n := value.(type) {
	case int:
		if n > 0 {
			return n
		}
	case int64:
		if n > 0 {
			return int(n)
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func objectMap(value any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode repository result: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("decode repository result: %w", err)
	}
	return out, nil
}
