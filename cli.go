package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type options struct {
	models      bool
	eu          bool
	staged      bool
	excludes    []string
	solvers     []string
	split       bool
	dryRun      bool
	push        bool
	pullRequest bool
	remote      string
	base        string
}

type listValue struct {
	values     *[]string
	splitComma bool
}

func (v listValue) String() string {
	if v.values == nil {
		return ""
	}
	return strings.Join(*v.values, ",")
}

func (v listValue) Set(value string) error {
	parts := []string{value}
	if v.splitComma {
		parts = strings.Split(value, ",")
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return errors.New("value must not be empty")
		}
		*v.values = append(*v.values, part)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	set := flag.NewFlagSet("commitell", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&opts.models, "models", false, "list compatible OpenRouter models")
	set.BoolVar(&opts.eu, "eu", false, "use EU in-region OpenRouter routing")
	set.BoolVar(&opts.staged, "staged", false, "commit only staged changes")
	set.Var(listValue{values: &opts.excludes, splitComma: true}, "exclude", "exclude repository-relative files")
	set.Var(listValue{values: &opts.solvers}, "solver", "OpenRouter model to try, in fallback order")
	set.BoolVar(&opts.split, "split", false, "split changes into logical commits")
	set.BoolVar(&opts.dryRun, "dry-run", false, "show the plan without changing Git or publishing")
	set.BoolVar(&opts.push, "push", false, "push the current branch after committing")
	set.BoolVar(&opts.pullRequest, "pr", false, "push and create a draft pull request")
	set.StringVar(&opts.remote, "remote", "origin", "Git remote used for publishing")
	set.StringVar(&opts.base, "base", "", "default and pull-request base branch")
	if err := set.Parse(args); err != nil {
		return options{}, err
	}
	if set.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", set.Arg(0))
	}
	if opts.pullRequest {
		opts.push = true
	}
	if strings.TrimSpace(opts.remote) == "" {
		return options{}, errors.New("--remote must not be empty")
	}
	for i, path := range opts.excludes {
		clean, err := normalizeExclude(path)
		if err != nil {
			return options{}, err
		}
		opts.excludes[i] = clean
	}
	for i, model := range opts.solvers {
		model = strings.TrimSpace(model)
		if model == "" {
			return options{}, errors.New("--solver must not be empty")
		}
		opts.solvers[i] = model
	}
	if opts.models && (opts.staged || len(opts.excludes) != 0 || len(opts.solvers) != 0 || opts.split || opts.dryRun || opts.push || opts.pullRequest || opts.base != "" || opts.remote != "origin") {
		return options{}, errors.New("--models can only be combined with --eu")
	}
	return opts, nil
}

func normalizeExclude(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("invalid excluded file %q", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("excluded file %q must be repository-relative", path)
	}
	return clean, nil
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: commitell [options]

Create one or more AI-written, DCO-signed commits.

Options:
  --staged              commit only staged changes
  --exclude FILES       exclude exact files; repeat or use comma-separated values
  --split               split files into logical commits
  --solver MODEL        model to try; repeat to define the fallback order
  --dry-run             print commits and publish actions without changing anything
  --eu                  use EU in-region OpenRouter routing
  --models              list account-, guardrail-, and ZDR-compatible models
  --push                push the current non-default branch after committing
  --pr                  push and create a draft pull request
  --remote NAME         publishing remote (default: origin)
  --base BRANCH         default and pull-request base branch
  --version             print the version
  -h, --help            show this help

Environment:
  OPENROUTER_API_KEY    required
`)
}
