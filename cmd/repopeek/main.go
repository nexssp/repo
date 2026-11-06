// Command repopeek is a thin GitHub repository overview frontend. GitHub API
// access and status interpretation live in github.com/nexssp/repo.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	repository "github.com/nexssp/repo"
)

const maxConcurrentRepositories = 8

type row struct {
	Input      string                       `json:"input"`
	Inspection *repository.GitHubInspection `json:"inspection,omitempty"`
	Error      string                       `json:"error,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "repopeek:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("repopeek", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	listPath := flags.String("f", "", "file containing GitHub repositories, one per line")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	repositories := append([]string(nil), flags.Args()...)
	if *listPath != "" {
		fromFile, err := readRepositories(*listPath)
		if err != nil {
			return err
		}
		repositories = append(fromFile, repositories...)
	}
	if len(repositories) == 0 {
		return fmt.Errorf("usage: repopeek [-f repositories.txt] owner/repo [owner/repo ...]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	provider := repository.New()
	rows := make([]row, len(repositories))
	sem := make(chan struct{}, maxConcurrentRepositories)
	var wg sync.WaitGroup
	for i, input := range repositories {
		wg.Add(1)
		go func(i int, input string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				rows[i] = row{Input: input, Error: ctx.Err().Error()}
				return
			}
			defer func() { <-sem }()
			result, err := provider.InspectGitHub(ctx, repository.GitHubRequest{Repository: input})
			if err != nil {
				rows[i] = row{Input: input, Error: err.Error()}
				return
			}
			rows[i] = row{Input: input, Inspection: &result}
		}(i, input)
	}
	wg.Wait()

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rows); err != nil {
		return err
	}
	for _, item := range rows {
		if item.Error != "" {
			return fmt.Errorf("one or more repositories could not be inspected")
		}
	}
	return nil
}

func readRepositories(path string) (repositories []string, retErr error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close repository list: %w", closeErr)
		}
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			repositories = append(repositories, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return repositories, nil
}
