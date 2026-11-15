package nexssflow

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
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
	if len(bundle.Libraries) != 1 || len(bundle.Libraries[0].Actions) != 10 {
		t.Fatalf("unexpected action libraries: %#v", bundle.Libraries)
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
