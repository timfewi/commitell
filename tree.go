package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type treeRepo struct {
	Path         string   `json:"path"`
	Branch       string   `json:"branch"`
	Changes      []string `json:"changes"`
	Dependencies []string `json:"dependencies,omitempty"`
	origin       string
}

// treeGit never refreshes refs or invokes fsmonitor hooks.
func treeGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	argv := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotePath=true", "-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", argv...).Output()
	if err != nil {
		return "", fmt.Errorf("Git query failed in %q: %w", dir, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Tree mode must come first, so flag values in the commit workflow are never
// mistaken for commands. A cluster such as -tGF expands to tree -G -F.
func treeArguments(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	if args[0] == "tree" {
		return args[1:], true
	}
	if !strings.HasPrefix(args[0], "-t") {
		return nil, false
	}
	var expanded []string
	for _, option := range args[0][2:] {
		expanded = append(expanded, "-"+string(option))
	}
	return append(expanded, args[1:]...), true
}

func treeCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("cmt tree", flag.ContinueOnError)
	flags.SetOutput(out)
	files := flags.Bool("files", false, "show changed paths")
	flags.BoolVar(files, "f", false, "show changed paths")
	flags.BoolVar(files, "F", false, "show changed paths")
	reposOnly := flags.Bool("repos", false, "show repository summaries")
	flags.BoolVar(reposOnly, "r", false, "show repository summaries")
	all := flags.Bool("all", false, "include clean repositories")
	flags.BoolVar(all, "A", false, "include clean repositories")
	graph := flags.Bool("graph", false, "show local repository dependencies from flake.lock")
	flags.BoolVar(graph, "G", false, "show local repository dependencies from flake.lock")
	asJSON := flags.Bool("json", false, "print structured repository status")
	depth := flags.Int("depth", 6, "maximum directory discovery depth")
	limit := flags.Int("max-files", 10, "maximum paths per repository (0: all)")
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: cmt -t[FGA] [--json] [BASE]\n       cmt tree [--files] [--all] [--graph] [--json] [BASE]\nShortcuts: -tF files, -tG graph, -tA all repositories; combine as -tGF.\nBASE defaults to CMT_ROOT, then RT_ROOT, then ~/code. Local refs only; no fetch.")
		flags.PrintDefaults()
	}
	for name, target := range map[string]*int{"RT_DEPTH": depth, "RT_MAX_FILES": limit} {
		if value := os.Getenv(name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid %s", name)
			}
			*target = n
		}
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 || *depth < 1 || *depth > 99 || *limit < 0 || *limit > 9999 {
		return errors.New("expected one base directory, depth 1..99 and max-files 0..9999")
	}
	base := flags.Arg(0)
	if base == "" {
		base = os.Getenv("CMT_ROOT")
	}
	if base == "" {
		base = os.Getenv("RT_ROOT")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		base = filepath.Join(home, "code")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	info, err := os.Stat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %q", base)
	}
	repos, err := scanTree(base, *depth, *graph)
	if err != nil {
		return err
	}
	selected := []treeRepo{}
	for _, repo := range repos {
		if *all || *graph || len(repo.Changes) > 0 || strings.Contains(repo.Branch, "[ahead ") || strings.Contains(repo.Branch, "[behind ") {
			selected = append(selected, repo)
		}
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(selected)
	}
	fmt.Fprintf(out, "%s\n", treeLabel(base))
	if *graph {
		renderDependencyGraph(out, selected, *files && !*reposOnly, *limit)
	} else {
		renderRepoTree(out, selected, *files && !*reposOnly, *limit)
	}
	fmt.Fprintf(out, "\nRepositories: %d · local refs only, no fetch\n", len(selected))
	return nil
}

func scanTree(base string, depth int, graph bool) ([]treeRepo, error) {
	repos := []treeRepo{}
	skip := map[string]bool{".git": true, "node_modules": true, ".venv": true, ".direnv": true, ".devenv": true, "target": true, ".cache": true}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		level := 0
		if rel != "." {
			level = len(strings.Split(rel, string(filepath.Separator)))
		}
		if path != base && (skip[entry.Name()] || level >= depth) {
			return filepath.SkipDir
		}
		if _, err := os.Lstat(filepath.Join(path, ".git")); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		status, err := treeGit(path, "status", "--porcelain=v1", "--branch", "--untracked-files=all")
		if err != nil {
			return err
		}
		lines := strings.Split(status, "\n")
		repo := treeRepo{Path: rel, Branch: strings.TrimPrefix(lines[0], "## "), Changes: []string{}}
		if len(lines) > 1 {
			repo.Changes = lines[1:]
		}
		if graph {
			repo.origin, _ = treeGit(path, "config", "--get", "remote.origin.url")
		}
		repos = append(repos, repo)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if graph {
		if err := resolveTreeDependencies(base, repos); err != nil {
			return nil, err
		}
	}
	return repos, nil
}

// Display control characters as escapes, including in directory names and refs.
func treeLabel(s string) string {
	for _, r := range s {
		if r < 32 || r == 127 {
			return strconv.Quote(s)
		}
	}
	return s
}
func repoSummary(r treeRepo) string {
	return fmt.Sprintf("[%s · %d changed paths]", treeLabel(r.Branch), len(r.Changes))
}
func renderFiles(out io.Writer, r treeRepo, prefix string, limit int) {
	count := len(r.Changes)
	if limit > 0 && count > limit {
		count = limit
	}
	for _, line := range r.Changes[:count] {
		fmt.Fprintf(out, "%s  %s\n", prefix, treeLabel(line))
	}
	if count < len(r.Changes) {
		fmt.Fprintf(out, "%s  … %d more files\n", prefix, len(r.Changes)-count)
	}
}

type repoTreeNode struct {
	repo     *treeRepo
	children map[string]*repoTreeNode
}

func renderRepoTree(out io.Writer, repos []treeRepo, files bool, limit int) {
	root := &repoTreeNode{children: map[string]*repoTreeNode{}}
	for i := range repos {
		node := root
		if repos[i].Path != "." {
			for _, part := range strings.Split(repos[i].Path, string(filepath.Separator)) {
				if node.children[part] == nil {
					node.children[part] = &repoTreeNode{children: map[string]*repoTreeNode{}}
				}
				node = node.children[part]
			}
		}
		node.repo = &repos[i]
	}
	var render func(*repoTreeNode, string)
	render = func(node *repoTreeNode, prefix string) {
		if node.repo != nil {
			fmt.Fprintf(out, "%s%s\n", prefix, repoSummary(*node.repo))
			if files {
				renderFiles(out, *node.repo, prefix, limit)
			}
		}
		names := []string{}
		for name := range node.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			connector, next := "├── ", "│   "
			if i == len(names)-1 {
				connector, next = "└── ", "    "
			}
			child := node.children[name]
			fmt.Fprintf(out, "%s%s%s\n", prefix, connector, treeLabel(name))
			render(child, prefix+next)
		}
	}
	render(root, "")
}

// Remote identity is used only for matching; credentials and URLs are never displayed.
func repoIdentity(raw string) string {
	raw = strings.TrimPrefix(raw, "git+")
	if strings.HasPrefix(raw, "git@") {
		raw = "ssh://" + strings.Replace(raw, ":", "/", 1)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname()) + strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git")
}
func resolveTreeDependencies(base string, repos []treeRepo) error {
	identities := map[string][]string{}
	for _, r := range repos {
		if id := repoIdentity(r.origin); id != "" {
			identities[id] = append(identities[id], r.Path)
		}
	}
	for i := range repos {
		path := filepath.Join(base, repos[i].Path, "flake.lock")
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var lock struct {
			Root  string `json:"root"`
			Nodes map[string]struct {
				Inputs map[string]json.RawMessage              `json:"inputs"`
				Locked struct{ Type, Owner, Repo, URL string } `json:"locked"`
			} `json:"nodes"`
		}
		err = json.NewDecoder(io.LimitReader(f, 4*1024*1024)).Decode(&lock)
		f.Close()
		if err != nil {
			return fmt.Errorf("read %q: %w", path, err)
		}
		root, ok := lock.Nodes[lock.Root]
		if !ok {
			return fmt.Errorf("missing root node in %q", path)
		}
		deps := map[string]bool{}
		for _, value := range root.Inputs {
			var nodeID string
			if json.Unmarshal(value, &nodeID) != nil {
				continue
			} // follows aliases do not add dependencies
			node, ok := lock.Nodes[nodeID]
			if !ok {
				return fmt.Errorf("missing input node in %q", path)
			}
			id := repoIdentity(node.Locked.URL)
			if node.Locked.Type == "github" {
				id = "github.com/" + node.Locked.Owner + "/" + node.Locked.Repo
			}
			for _, local := range identities[id] {
				deps[local] = true
			}
		}
		for dep := range deps {
			repos[i].Dependencies = append(repos[i].Dependencies, dep)
		}
		sort.Strings(repos[i].Dependencies)
	}
	return nil
}
func renderDependencyGraph(out io.Writer, repos []treeRepo, files bool, limit int) {
	fmt.Fprintln(out, "Dependencies: consumer → input (local matches from flake.lock)")
	byPath := map[string]treeRepo{}
	for _, r := range repos {
		byPath[r.Path] = r
	}
	var render func(string, string, map[string]bool)
	expanded := map[string]bool{}
	render = func(path, prefix string, ancestors map[string]bool) {
		r := byPath[path]
		fmt.Fprintf(out, "%s%s %s", prefix, treeLabel(path), repoSummary(r))
		if ancestors[path] {
			fmt.Fprintln(out, " ↻ cycle")
			return
		}
		if expanded[path] {
			fmt.Fprintln(out, " (shown above)")
			return
		}
		expanded[path] = true
		fmt.Fprintln(out)
		if files {
			renderFiles(out, r, prefix, limit)
		}
		ancestors[path] = true
		for _, dep := range r.Dependencies {
			render(dep, prefix+"  → ", ancestors)
		}
		delete(ancestors, path)
	}
	for _, r := range repos {
		render(r.Path, "", map[string]bool{})
	}
}
