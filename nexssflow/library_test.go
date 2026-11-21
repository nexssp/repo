package nexssflow

import (
	"context"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
	repository "github.com/nexssp/repo"
)

func TestBundleExportsRunnableModule(t *testing.T) {
	bundle := Bundle(nil)
	if err := compiler.ValidateBundle(bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.ExportedModules) != 1 || bundle.ExportedModules[0] != "nflows/repo.nflow" {
		t.Fatalf("unexpected exported modules: %#v", bundle.ExportedModules)
	}
	if _, err := fs.Stat(bundle.SourceModules, bundle.ExportedModules[0]); err != nil {
		t.Fatalf("exported module missing: %v", err)
	}
	if len(bundle.Libraries) != 1 || len(bundle.Libraries[0].Actions) != 11 {
		t.Fatalf("unexpected action libraries: %#v", bundle.Libraries)
	}
	hasChangedPaths := false
	for _, bundleAction := range bundle.Libraries[0].Actions {
		metadata := bundleAction.Describe()
		if metadata == nil || metadata.Name != "repo.changed_paths" {
			continue
		}
		if _, ok := bundleAction.(interface {
			Do(context.Context, repository.ChangedPathsRequest) (repository.ChangedPathsResult, error)
		}); !ok {
			t.Fatal("repo.changed_paths is not a typed request/response action")
		}
		hasChangedPaths = true
	}
	if !hasChangedPaths {
		t.Fatal("bundle is missing repo.changed_paths")
	}
	source, err := fs.ReadFile(bundle.SourceModules, bundle.ExportedModules[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "@pipeline github_inspect") {
		t.Fatal("exported Flow source is missing github_inspect pipeline")
	}
	if !strings.Contains(string(source), "@pipeline github_tags") {
		t.Fatal("exported Flow source is missing github_tags pipeline")
	}
	if !strings.Contains(string(source), "@pipeline github_normalize") {
		t.Fatal("exported Flow source is missing github_normalize pipeline")
	}
	if !strings.Contains(string(source), "@pipeline github_latest_semver") {
		t.Fatal("exported Flow source is missing github_latest_semver pipeline")
	}
	if !strings.Contains(string(source), "@pipeline github_module_versions") {
		t.Fatal("exported Flow source is missing github_module_versions pipeline")
	}
	if !strings.Contains(string(source), "@pipeline sync") {
		t.Fatal("exported Flow source is missing sync pipeline")
	}
}

func TestChangedPathsActionDelegatesToProvider(t *testing.T) {
	ctx := context.Background()
	request := repository.ChangedPathsRequest{Root: "/checkout", Since: "origin/main", Modified: true, Staged: true}
	wantResult := repository.ChangedPathsResult{Root: "/checkout", Paths: []string{"/checkout/a.go", "/checkout/b.go"}}
	provider := &recordingChangedPathsProvider{result: wantResult}

	gotResult, err := newChangedPathsAction(provider).Do(ctx, request)
	if err != nil {
		t.Fatalf("changed paths action error = %v", err)
	}
	if provider.callContext != ctx {
		t.Fatal("provider did not receive the action context")
	}
	if provider.request != request {
		t.Fatalf("provider request = %+v, want %+v", provider.request, request)
	}
	if !reflect.DeepEqual(gotResult, wantResult) {
		t.Fatalf("changed paths result = %+v, want %+v", gotResult, wantResult)
	}
}

type recordingChangedPathsProvider struct {
	repository.Provider
	callContext context.Context
	request     repository.ChangedPathsRequest
	result      repository.ChangedPathsResult
}

func (p *recordingChangedPathsProvider) ChangedPaths(ctx context.Context, request repository.ChangedPathsRequest) (repository.ChangedPathsResult, error) {
	p.callContext = ctx
	p.request = request
	return p.result, nil
}
