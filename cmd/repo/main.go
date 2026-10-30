// Command repo exposes the shared read-only repository capabilities as JSON.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	repository "github.com/nexssp/repo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "repo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage()
		return nil
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", ".", "repository path (default: current directory)")
	path := flags.String("path", "", "optional path inside the repository")
	staged := flags.Bool("staged", false, "for diff, show staged changes only")
	maxFiles := flags.Int("max-files", repository.DefaultMaxFiles, "pack file limit")
	maxBytes := flags.Int("max-bytes", 0, "byte limit (uses the operation-specific default when omitted)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	positional := flags.Args()
	if command == "github" {
		if len(positional) != 1 {
			return fmt.Errorf("usage: repo github owner/name")
		}
	} else if len(positional) != 0 {
		return fmt.Errorf("unexpected arguments: %v", positional)
	}

	service := repository.New()
	ctx := context.Background()
	var value any
	var err error
	switch command {
	case "inspect":
		value, err = service.Inspect(ctx, repository.InspectRequest{Root: *root})
	case "files":
		value, err = service.Files(ctx, repository.FilesRequest{Root: *root, Path: *path})
	case "diff":
		value, err = service.Diff(ctx, repository.DiffRequest{Root: *root, Staged: *staged, MaxBytes: *maxBytes})
	case "pack":
		value, err = service.Pack(ctx, repository.PackRequest{Root: *root, Path: *path, MaxFiles: *maxFiles, MaxBytes: *maxBytes})
	case "github":
		value, err = service.InspectGitHub(ctx, repository.GitHubRequest{Repository: positional[0]})
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: repo <inspect|files|diff|pack|github> [flags]

All commands emit JSON and are read-only.
  repo inspect [--root .]
  repo files   [--root .] [--path src]
	repo diff    [--root .] [--staged] [--max-bytes 2097152]
	repo pack    [--root .] [--path src] [--max-files 200] [--max-bytes 1048576]
	repo github  owner/name-or-github-url`)
}
