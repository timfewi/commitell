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
	staged      bool
	excludes    []string
	solvers     []string
	offline     bool
	split       bool
	dryRun      bool
	push        bool
	force       bool
	pullRequest bool
	version     bool
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

var shortOptions = map[byte]bool{
	's': false,
	'x': true,
	'm': true,
	'o': false,
	'c': false,
	'd': false,
	'p': false,
	'f': false,
	'P': false,
	'r': true,
	'b': true,
	'v': false,
	'h': false,
}

var longOptions = map[string]bool{
	"staged": true, "exclude": true,
	"solver": true, "model": true, "offline": true,
	"split": true, "dry-run": true, "push": true, "force": true,
	"pr": true, "remote": true, "base": true, "version": true,
	"help": true,
}

func expandShortOptions(args []string) ([]string, error) {
	expanded := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			expanded = append(expanded, args[index:]...)
			break
		}
		if len(arg) <= 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			expanded = append(expanded, arg)
			continue
		}
		name := strings.SplitN(strings.TrimPrefix(arg, "-"), "=", 2)[0]
		if longOptions[name] || strings.Contains(arg, "=") {
			expanded = append(expanded, arg)
			continue
		}
		cluster := strings.TrimPrefix(arg, "-")
		for position := 0; position < len(cluster); position++ {
			short := cluster[position]
			takesValue, ok := shortOptions[short]
			if !ok {
				return nil, fmt.Errorf("unknown short option -%c in %q", short, arg)
			}
			if takesValue && position != len(cluster)-1 {
				return nil, fmt.Errorf("short option -%c takes a value and must be last in cluster %q", short, arg)
			}
			expanded = append(expanded, "-"+string(short))
		}
	}
	return expanded, nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	set := flag.NewFlagSet("commitell", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	boolFlag := func(target *bool, long, short, description string) {
		set.BoolVar(target, long, false, description)
		set.BoolVar(target, short, false, description)
	}
	boolFlag(&opts.staged, "staged", "s", "commit only staged changes")
	excludeValue := listValue{values: &opts.excludes, splitComma: true}
	set.Var(excludeValue, "exclude", "exclude repository-relative files")
	set.Var(excludeValue, "x", "exclude repository-relative files")
	modelValue := listValue{values: &opts.solvers}
	set.Var(modelValue, "solver", "model to try, in fallback order (alias: --model)")
	set.Var(modelValue, "model", "model to try, in fallback order")
	set.Var(modelValue, "m", "model to try, in fallback order")
	boolFlag(&opts.offline, "offline", "o", "generate commit messages locally without a model API request")
	boolFlag(&opts.split, "split", "c", "split changes into logical commits")
	boolFlag(&opts.dryRun, "dry-run", "d", "show the plan without changing Git or publishing")
	boolFlag(&opts.push, "push", "p", "push the current branch after committing")
	boolFlag(&opts.force, "force", "f", "bypass local secret checks and allow a force-push to the default branch")
	boolFlag(&opts.pullRequest, "pr", "P", "push and create a draft pull request")
	set.StringVar(&opts.remote, "remote", "origin", "Git remote used for publishing")
	set.StringVar(&opts.remote, "r", "origin", "Git remote used for publishing")
	set.StringVar(&opts.base, "base", "", "default and pull-request base branch")
	set.StringVar(&opts.base, "b", "", "default and pull-request base branch")
	boolFlag(&opts.version, "version", "v", "print the version")
	expanded, err := expandShortOptions(args)
	if err != nil {
		return options{}, err
	}
	if err := set.Parse(expanded); err != nil {
		return options{}, err
	}
	if set.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", set.Arg(0))
	}
	if opts.pullRequest {
		opts.push = true
	}
	if opts.offline && (len(opts.solvers) != 0 || opts.split) {
		return options{}, errors.New("--offline cannot be combined with --model, --solver, or --split")
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
			return options{}, errors.New("--model must not be empty")
		}
		opts.solvers[i] = model
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
	fmt.Fprint(w, `Usage:
  commitell [options]
  cmt -t[FGA] [tree options] [BASE]
  cmt tree [--files] [--all] [--graph] [--json] [BASE]

Create one or more DCO-signed Git commits. By default, commitell analyzes the
complete dirty working tree with OpenAI gpt-6-luna, then stages and commits
all changes only after a valid message is ready.

Repository overview (no API key needed):
  -t                    Show repositories with changes or ahead/behind status.
  -tF                   Include changed files.
  -tG                   Show repository dependencies and Git status.
  -tA                   Include clean repositories.
                         Combine as -tGF; use -t --help for all tree options.
                         Colored on a terminal; --color never or NO_COLOR
                         disables it.

Change selection:
  -s, --staged           Commit only changes already in the Git index.
  -x, --exclude FILES    Exclude exact repository-relative paths. Repeat the
                         option or pass comma-separated paths.
  -c, --split            Ask the model to split whole files into logical commits.

Models and privacy:
  -m, --model MODEL      Model to try. Repeat to define the complete fallback
                         order. --solver is a legacy alias and also uses -m.
  -o, --offline          Generate a conservative message locally. No API key
                         or model provider is involved.

Preview and publishing:
  -d, --dry-run          Print the commit and publish plan without changing Git.
  -p, --push             Push the current branch after every commit succeeds.
  -P, --pr               Push and create a draft pull request; implies --push.
  -r, --remote NAME      Git remote used for publishing (default: origin).
  -b, --base BRANCH      Protected default branch and pull-request base.
  -f, --force            Skip likely-secret confirmation. Also permits a
                         force-with-lease push from the declared default branch.

General:
  -v, --version          Print the version.
  -h, --help             Show this help.

Short option clusters:
  Boolean short options can be combined. For example, -scd is equivalent to
  --staged --split --dry-run. Options that take a value (-x, -m, -r, -b) must
  be written separately or placed last in a cluster:
    commitell -sdm gpt-6-luna

Common use cases:
  Safest local commit; no model provider receives the diff:
    commitell --offline
    commitell -o

  Use OpenAI with the default gpt-6-luna model:
    commitell

  Preview staged changes without committing:
    commitell --staged --dry-run
    commitell -sd

  Preview a model-generated split of staged files:
    commitell --staged --split --dry-run
    commitell -scd

  Exclude local or generated files:
    commitell -x .env.example,dist/output.json --dry-run

  Choose an explicit fallback order:
    commitell -m gpt-6-luna -m gpt-4o-mini

  Push a feature branch and open a draft pull request:
    commitell --split --pr --base main
    commitell -cP -b main

Privacy and safety:
  Selected changes are sent directly to OpenAI. Account data policies apply.
  Likely-secret matches are shown by location and type, then require a y/N
  confirmation before any model request or staging. Non-interactive runs fail
  closed; use --exclude or the explicit --force override after reviewing the
  content. Cloud availability cannot be guaranteed. Use --offline when the
  commit must work without a model provider. Normal Git hooks still run.

Environment:
  COMMITELL_API_KEY      OpenAI API key. Required except with --offline or tree.
  COMMITELL_MODELS       Comma-separated fallback order (default: gpt-6-luna).
                         Repeated --model flags replace this order.
`)
}
