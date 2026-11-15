package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SGR parameters used by the overview. Only fixed escape sequences are
// emitted; repository-controlled text is passed through treeLabel first.
const (
	sgrBold    = "1"
	sgrDim     = "2"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrYellow  = "33"
	sgrMagenta = "35"
	sgrCyan    = "36"
	sgrDir     = "1;34"
	sgrPath    = "34"
	sgrAlert   = "1;35"
)

type treeStyle struct{ color bool }

func (s treeStyle) paint(sgr, text string) string {
	if !s.color || strings.TrimSpace(text) == "" {
		return text
	}
	return "\x1b[" + sgr + "m" + text + "\x1b[0m"
}

// treeColor resolves --color. In auto mode, color requires a terminal and
// follows the NO_COLOR convention and TERM=dumb.
func treeColor(mode string, out io.Writer) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		file, ok := out.(*os.File)
		return ok && isTerminal(file) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb", nil
	}
	return false, errors.New("--color must be auto, always or never")
}

type branchState struct {
	local, upstream        string
	ahead, behind          int
	gone, detached, unborn bool
}

// parseBranch reads the header of `git status --porcelain=v1 --branch`.
// Ref names cannot contain spaces or "..", so both separators are unambiguous.
func parseBranch(header string) branchState {
	var b branchState
	if rest, ok := strings.CutPrefix(header, "No commits yet on "); ok {
		b.unborn, header = true, rest
	}
	if strings.HasPrefix(header, "HEAD (no branch)") {
		b.detached = true
		return b
	}
	if i := strings.Index(header, " ["); i >= 0 && strings.HasSuffix(header, "]") {
		for _, part := range strings.Split(header[i+2:len(header)-1], ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				b.ahead, _ = strconv.Atoi(n)
			} else if n, ok := strings.CutPrefix(part, "behind "); ok {
				b.behind, _ = strconv.Atoi(n)
			} else if part == "gone" {
				b.gone = true
			}
		}
		header = header[:i]
	}
	b.local, b.upstream, _ = strings.Cut(header, "...")
	return b
}

func conflictStatus(xy string) bool {
	switch xy {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

type changeCounts struct{ staged, unstaged, untracked, conflicts int }

func countChanges(lines []string) changeCounts {
	var c changeCounts
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		switch xy := line[:2]; {
		case xy == "??":
			c.untracked++
		case conflictStatus(xy):
			c.conflicts++
		default:
			if xy[0] != ' ' && xy[0] != '!' {
				c.staged++
			}
			if xy[1] != ' ' && xy[1] != '!' {
				c.unstaged++
			}
		}
	}
	return c
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

type treeRenderer struct {
	out   io.Writer
	style treeStyle
	files bool
	limit int
	all   bool
}

func (r treeRenderer) println(parts ...string) {
	fmt.Fprintln(r.out, strings.Join(parts, ""))
}

// name shows the parent directories of a repository path quieter than its
// final component, so the repository itself stands out in long paths.
func (r treeRenderer) name(path string, repo bool) string {
	label := treeLabel(path)
	if !repo {
		return r.style.paint(sgrDir, label)
	}
	dir, base := "", label
	if i := strings.LastIndex(label, "/"); i >= 0 && !strings.HasPrefix(label, `"`) {
		dir, base = label[:i+1], label[i+1:]
	}
	return r.style.paint(sgrPath, dir) + r.style.paint(sgrBold, base)
}

func (r treeRenderer) summary(repo treeRepo) string {
	s := r.style
	b := parseBranch(repo.Branch)
	var head string
	switch {
	case b.detached:
		head = s.paint(sgrYellow, "detached HEAD")
	default:
		head = s.paint(sgrCyan, treeLabel(b.local))
	}
	if b.unborn {
		head += s.paint(sgrDim, " (no commits)")
	}
	switch {
	case b.upstream != "" && b.upstream != "origin/"+b.local:
		head += s.paint(sgrDim, " → "+treeLabel(b.upstream))
	case b.upstream == "" && !b.detached:
		head += s.paint(sgrDim, " (no upstream)")
	}
	if b.ahead > 0 {
		head += " " + s.paint(sgrGreen, fmt.Sprintf("↑%d", b.ahead))
	}
	if b.behind > 0 {
		head += " " + s.paint(sgrYellow, fmt.Sprintf("↓%d", b.behind))
	}
	if b.gone {
		head += " " + s.paint(sgrRed, "upstream gone")
	}
	return head + s.paint(sgrDim, " · ") + r.changes(repo.Changes)
}

func (r treeRenderer) changes(lines []string) string {
	s := r.style
	if len(lines) == 0 {
		return s.paint(sgrGreen, "clean")
	}
	c := countChanges(lines)
	var parts []string
	for _, part := range []struct {
		n    int
		word string
		sgr  string
	}{
		{c.conflicts, "conflict", sgrAlert},
		{c.staged, "staged", sgrGreen},
		{c.unstaged, "unstaged", sgrRed},
		{c.untracked, "untracked", sgrRed},
	} {
		if part.n == 0 {
			continue
		}
		text := strconv.Itoa(part.n) + " " + part.word
		if part.word == "conflict" {
			text = plural(part.n, part.word, part.word+"s")
		}
		parts = append(parts, s.paint(part.sgr, text))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return s.paint(sgrYellow, fmt.Sprintf("%d changed", len(lines))) + s.paint(sgrDim, " (") +
		strings.Join(parts, s.paint(sgrDim, ", ")) + s.paint(sgrDim, ")")
}

// fileLines prints porcelain lines with Git's color convention: index column
// green, worktree column red, conflicts highlighted, directories dimmed.
func (r treeRenderer) fileLines(repo treeRepo, indent string) {
	if !r.files {
		return
	}
	s := r.style
	count := len(repo.Changes)
	if r.limit > 0 && count > r.limit {
		count = r.limit
	}
	for _, line := range repo.Changes[:count] {
		if len(line) < 4 {
			r.println(indent, treeLabel(line))
			continue
		}
		xy, path := line[:2], treeLabel(line[3:])
		var status string
		switch {
		case xy == "??":
			status = s.paint(sgrRed, xy)
		case xy == "!!":
			status = s.paint(sgrDim, xy)
		case conflictStatus(xy):
			status = s.paint(sgrAlert, xy)
		default:
			status = s.paint(sgrGreen, xy[:1]) + s.paint(sgrRed, xy[1:])
		}
		if from, to, ok := strings.Cut(path, " -> "); ok {
			path = s.paint(sgrDim, from+" → ") + to
		} else if i := strings.LastIndex(path, "/"); i >= 0 && !strings.HasPrefix(path, `"`) {
			path = s.paint(sgrDim, path[:i+1]) + path[i+1:]
		}
		r.println(indent, status, " ", path)
	}
	if hidden := len(repo.Changes) - count; hidden > 0 {
		r.println(indent, s.paint(sgrDim, "… "+plural(hidden, "more file", "more files")))
	}
}

// guide continues a vertical line through a file list when children follow.
func (r treeRenderer) guide(children bool) string {
	if children {
		return r.style.paint(sgrDim, "│ ")
	}
	return "  "
}

type repoTreeNode struct {
	repo     *treeRepo
	children map[string]*repoTreeNode
}

func (r treeRenderer) renderRepoTree(base string, repos []treeRepo) {
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
	header := r.style.paint(sgrBold, treeLabel(base))
	if root.repo != nil {
		header += "  " + r.summary(*root.repo)
	}
	r.println(header)
	if root.repo != nil {
		r.fileLines(*root.repo, r.guide(len(root.children) > 0))
	}
	var render func(*repoTreeNode, string)
	render = func(node *repoTreeNode, prefix string) {
		names := make([]string, 0, len(node.children))
		for name := range node.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			connector, next := "├── ", "│   "
			if i == len(names)-1 {
				connector, next = "└── ", "    "
			}
			child, label := node.children[name], name
			// Directories leading to a single entry collapse into one path.
			for child.repo == nil && len(child.children) == 1 {
				for only, grandchild := range child.children {
					label, child = label+"/"+only, grandchild
				}
			}
			line := prefix + r.style.paint(sgrDim, connector) + r.name(label, child.repo != nil)
			if child.repo != nil {
				line += "  " + r.summary(*child.repo)
			}
			r.println(line)
			childPrefix := prefix + r.style.paint(sgrDim, next)
			if child.repo != nil {
				r.fileLines(*child.repo, childPrefix+r.guide(len(child.children) > 0))
			}
			render(child, childPrefix)
		}
	}
	render(root, "")
}

// renderDependencyGraph draws each consumer once as a root and its local
// flake inputs below it. Repositories without local links are listed apart.
func (r treeRenderer) renderDependencyGraph(repos []treeRepo) {
	s := r.style
	byPath := map[string]treeRepo{}
	for _, repo := range repos {
		byPath[repo.Path] = repo
	}
	consumed := map[string]bool{}
	deps := map[string][]string{}
	for _, repo := range repos {
		for _, dep := range repo.Dependencies {
			if _, ok := byPath[dep]; ok {
				deps[repo.Path] = append(deps[repo.Path], dep)
				consumed[dep] = true
			}
		}
	}
	r.println(s.paint(sgrDim, "consumer ─▶ input · local flake.lock matches"))
	expanded := map[string]bool{}
	ancestors := map[string]bool{}
	var visit func(path, lead, cont string)
	visit = func(path, lead, cont string) {
		name := r.name(path, true)
		if ancestors[path] {
			r.println(lead, name, " ", s.paint(sgrRed, "↻ cycle"))
			return
		}
		if expanded[path] {
			r.println(lead, name, " ", s.paint(sgrDim, "(shown above)"))
			return
		}
		expanded[path] = true
		repo := byPath[path]
		r.println(lead, name, "  ", r.summary(repo))
		r.fileLines(repo, cont+r.guide(len(deps[path]) > 0))
		ancestors[path] = true
		for i, dep := range deps[path] {
			connector, next := "├─▶ ", "│   "
			if i == len(deps[path])-1 {
				connector, next = "└─▶ ", "    "
			}
			visit(dep, cont+s.paint(sgrDim, connector), cont+s.paint(sgrDim, next))
		}
		delete(ancestors, path)
	}
	linked := func(repo treeRepo) bool { return len(deps[repo.Path]) > 0 || consumed[repo.Path] }
	roots := 0
	root := func(path string) {
		if roots > 0 {
			r.println()
		}
		roots++
		visit(path, "", "")
	}
	for _, repo := range repos {
		if linked(repo) && !consumed[repo.Path] {
			root(repo.Path)
		}
	}
	// Members of a cycle have no unconsumed root; start them where they appear.
	for _, repo := range repos {
		if linked(repo) && !expanded[repo.Path] {
			root(repo.Path)
		}
	}
	var standalone []treeRepo
	hidden := 0
	for _, repo := range repos {
		if linked(repo) {
			continue
		}
		if !r.all && len(repo.Changes) == 0 && repoInSync(repo) {
			hidden++
			continue
		}
		standalone = append(standalone, repo)
	}
	if len(standalone) == 0 && hidden == 0 {
		return
	}
	r.println()
	r.println(s.paint(sgrDim, "without local flake links"))
	for _, repo := range standalone {
		r.println("  ", r.name(repo.Path, true), "  ", r.summary(repo))
		r.fileLines(repo, "    ")
	}
	if hidden > 0 {
		r.println(s.paint(sgrDim, fmt.Sprintf("  … %s hidden (use -A to list)", plural(hidden, "clean repository", "clean repositories"))))
	}
}

func repoInSync(repo treeRepo) bool {
	b := parseBranch(repo.Branch)
	return b.ahead == 0 && b.behind == 0
}

func (r treeRenderer) footer(repos []treeRepo) {
	s := r.style
	changed, ahead, behind := 0, 0, 0
	for _, repo := range repos {
		b := parseBranch(repo.Branch)
		if len(repo.Changes) > 0 {
			changed++
		}
		if b.ahead > 0 {
			ahead++
		}
		if b.behind > 0 {
			behind++
		}
	}
	parts := []string{s.paint(sgrBold, fmt.Sprintf("Repositories: %d", len(repos)))}
	for _, part := range []struct {
		n    int
		text string
		sgr  string
	}{{changed, "with changes", sgrYellow}, {ahead, "ahead", sgrGreen}, {behind, "behind", sgrYellow}} {
		if part.n > 0 {
			parts = append(parts, s.paint(part.sgr, fmt.Sprintf("%d %s", part.n, part.text)))
		}
	}
	parts = append(parts, s.paint(sgrDim, "local refs only, no fetch"))
	r.println()
	r.println(strings.Join(parts, s.paint(sgrDim, " · ")))
}
