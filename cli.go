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
	colorMode   string
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
	"help": true, "color": true,
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
	boolFlag(&opts.force, "force", "f", "skip secret confirmation and push with --force-with-lease")
	boolFlag(&opts.pullRequest, "pr", "P", "push and create a draft pull request")
	set.StringVar(&opts.remote, "remote", "origin", "Git remote used for publishing")
	set.StringVar(&opts.remote, "r", "origin", "Git remote used for publishing")
	set.StringVar(&opts.base, "base", "", "default and pull-request base branch")
	set.StringVar(&opts.base, "b", "", "default and pull-request base branch")
	boolFlag(&opts.version, "version", "v", "print the version")
	set.StringVar(&opts.colorMode, "color", "auto", "color output: auto, always or never")
	expanded, err := expandShortOptions(args)
	if err != nil {
		return opts, err
	}
	parseErr := set.Parse(expanded)
	if err := validateColorMode(opts.colorMode); err != nil {
		return opts, err
	}
	if parseErr != nil {
		return opts, parseErr
	}
	if set.NArg() != 0 {
		return opts, fmt.Errorf("unexpected argument %q", set.Arg(0))
	}
	if opts.pullRequest {
		opts.push = true
	}
	if opts.offline && (len(opts.solvers) != 0 || opts.split) {
		return opts, errors.New("--offline cannot be combined with --model, --solver, or --split")
	}
	if strings.TrimSpace(opts.remote) == "" {
		return opts, errors.New("--remote must not be empty")
	}
	for i, path := range opts.excludes {
		clean, err := normalizeExclude(path)
		if err != nil {
			return opts, err
		}
		opts.excludes[i] = clean
	}
	for i, model := range opts.solvers {
		model = strings.TrimSpace(model)
		if model == "" {
			return opts, errors.New("--model must not be empty")
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

func usage(w io.Writer, mode string) {
	s := styleFor(mode, w)
	fmt.Fprintln(w, s.paint(sgrBold, "commitell")+" — turn Git changes into DCO-signed commits")
	helpSection(w, s, "Usage",
		helpRow{"commitell [options]", "Generate a message, then stage and commit changes."},
		helpRow{"cmt -t[FGA] [options] [BASE]", "Inspect repositories; no API key needed."})
	fmt.Fprintf(w, "\n  With no options: analyze all changes using OpenAI %s.\n", defaultModel)
	helpSection(w, s, "Change selection",
		helpRow{"-s, --staged", "Commit only content already in the Git index."},
		helpRow{"-x, --exclude FILES", "Exclude exact relative paths; repeat or separate with commas."},
		helpRow{"-c, --split", "Group whole files into up to eight logical commits."})
	helpSection(w, s, "Messages",
		helpRow{"-o, --offline", "Generate locally without an API key; excludes --model/--split."},
		helpRow{"-m, --model MODEL", "Repeat for fallback order; replaces COMMITELL_MODELS."},
		helpRow{"    --solver MODEL", "Alias for --model."})
	helpSection(w, s, "Preview and publishing",
		helpRow{"-d, --dry-run", "Show the plan without changing Git or publishing."},
		helpRow{"-p, --push", "Push the current branch after all commits succeed."},
		helpRow{"-P, --pr", "Push and open a draft pull request using gh."},
		helpRow{"-r, --remote NAME", "Publishing remote (default: origin)."},
		helpRow{"-b, --base BRANCH", "Protected default branch and pull-request base."},
		helpRow{"-f, --force", "Skip secret confirmation; push with --force-with-lease."})
	fmt.Fprintln(w, "  Publishing the default branch requires --force; --pr needs a feature branch.")
	helpSection(w, s, "Output",
		helpRow{"    --color WHEN", "auto, always or never (default: auto)."},
		helpRow{"-v, --version", "Print the version."},
		helpRow{"-h, --help", "Show this help."})
	fmt.Fprintln(w, "  Auto colors terminals only, respecting NO_COLOR and TERM=dumb.")
	fmt.Fprintln(w, "  Explicit --color overrides the environment; use it before --help.")
	helpSection(w, s, "Examples",
		helpRow{"commitell -od", "Preview a local message without contacting OpenAI."},
		helpRow{"commitell -scd", "Preview a model-generated split of staged changes."},
		helpRow{"commitell -cP -b main", "Split, commit, push and open a draft pull request."},
		helpRow{"cmt -tGF", "Show dependency trees with changed files."},
		helpRow{"cmt -t --help", "Show all repository overview options."})
	fmt.Fprintln(w, "  Boolean short options can be clustered: -scd = --staged --split --dry-run.")
	fmt.Fprintln(w, "  Value options must be separate or last in a cluster: -sdm "+defaultModel+".")
	helpSection(w, s, "Environment",
		helpRow{"COMMITELL_API_KEY", "OpenAI key; not needed for --offline, help or tree."},
		helpRow{"COMMITELL_MODELS", "Comma-separated model fallback order (default: " + defaultModel + ")."},
		helpRow{"NO_COLOR", "Any non-empty value disables automatic color."})
	fmt.Fprintln(w, "\n"+s.paint(sgrBold, "Privacy and safety"))
	fmt.Fprintln(w, "  Selected changes are sent directly to OpenAI unless --offline is set.")
	fmt.Fprintln(w, "  Possible secrets require confirmation before sending or staging. Unattended")
	fmt.Fprintln(w, "  runs stop for review; use --exclude or --force after inspecting the content.")
	fmt.Fprintln(w, "  Git hooks remain active. Nothing is pushed without --push or --pr.")
}

func treeUsage(w io.Writer, mode string) {
	s := styleFor(mode, w)
	fmt.Fprintln(w, s.paint(sgrBold, "cmt tree")+" — inspect local repositories and their dependencies")
	helpSection(w, s, "Usage",
		helpRow{"cmt -t[FGA] [options] [BASE]", "Put -t first; combine shortcuts as -tGF."},
		helpRow{"cmt tree [options] [BASE]", "Long-form command; same overview."})
	fmt.Fprintln(w, "\n  Shows changes and locally known ahead/behind status. No API key, model")
	fmt.Fprintln(w, "  request or fetch. Does not execute repository code or evaluate Nix files.")
	helpSection(w, s, "View",
		helpRow{"-F, -f, --files", "List changed paths using Git's short status format."},
		helpRow{"-r, --repos", "Show summaries only; overrides --files."},
		helpRow{"-A, --all", "Include clean repositories."},
		helpRow{"-G, --graph", "Show consumer → input links from direct flake.lock inputs."},
		helpRow{"    --json", "Emit repository records as JSON; never colored."})
	helpSection(w, s, "Discovery and output",
		helpRow{"    --depth N", "Discovery depth, 1..99 (default: 6)."},
		helpRow{"    --max-files N", "Paths per repository, 0..9999; 0 = all (default: 10)."},
		helpRow{"    --color WHEN", "auto, always or never (default: auto)."},
		helpRow{"-h, --help", "Show this help."})
	fmt.Fprintln(w, "  Auto honors NO_COLOR and TERM=dumb. Explicit --color overrides them;")
	fmt.Fprintln(w, "  place it before --help. Flags must come before BASE.")
	helpSection(w, s, "Examples",
		helpRow{"cmt -tF ~/code", "Show repositories and their changed files."},
		helpRow{"cmt -tGF --all ~/code", "Show dependency graphs, files and clean repositories."},
		helpRow{"cmt -tG --json ~/code", "Export dependency and status records."})
	helpSection(w, s, "Environment",
		helpRow{"CMT_ROOT", "Default BASE (fallback: RT_ROOT, then ~/code)."},
		helpRow{"RT_DEPTH, RT_MAX_FILES", "Legacy defaults; explicit flags take precedence."},
		helpRow{"NO_COLOR", "Any non-empty value disables automatic color."})
}
