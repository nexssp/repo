package main

import (
	"os"

	"github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/repo/nexssflow"
)

func main() {
	bundles := []compiler.Bundle{nexssflow.Bundle(nil)}
	os.Exit(cli.RunWithBundles(os.Args[1:], bundles))
}
