package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTree(args []string, out io.Writer) error {
	opts, err := parseTreeOptions(args)
	if err != nil {
		return err
	}
	return treeCommand(opts, out)
}

func treeFixture(t *testing.T, base, name string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := treeGit(dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := treeGit(dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "initial"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTreeShortcutsMatchLongCommands(t *testing.T) {
	t.Setenv("RT_DEPTH", "")
	t.Setenv("RT_MAX_FILES", "")
	base := t.TempDir()
	dirty := treeFixture(t, base, "dirty")
	clean := treeFixture(t, base, "clean")
	if _, err := treeGit(clean, "remote", "add", "origin", "https://github.com/fixture/clean.git"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dirty, "changed.txt"), "fixture")
	writeFile(t, filepath.Join(dirty, "flake.lock"), `{"root":"root","nodes":{"root":{"inputs":{"dep":"dep"}},"dep":{"locked":{"type":"github","owner":"fixture","repo":"clean"}}}}`)
	for _, test := range []struct {
		short []string
		long  []string
	}{
		{[]string{"-t"}, nil},
		{[]string{"-tF"}, []string{"--files"}},
		{[]string{"-tG"}, []string{"--graph"}},
		{[]string{"-tA"}, []string{"--all"}},
		{[]string{"-tGF"}, []string{"--graph", "--files"}},
		{[]string{"-tFGA", "--json"}, []string{"--files", "--graph", "--all", "--json"}},
		{[]string{"-t", "-F", "-G"}, []string{"--files", "--graph"}},
		{[]string{"tree", "-A"}, []string{"--all"}},
	} {
		t.Run(strings.Join(test.short, " "), func(t *testing.T) {
			args, selected := treeArguments(append(test.short, base))
			if !selected {
				t.Fatal("tree mode was not selected")
			}
			var short, long bytes.Buffer
			if err := runTree(args, &short); err != nil {
				t.Fatal(err)
			}
			if err := runTree(append(test.long, base), &long); err != nil {
				t.Fatal(err)
			}
			if short.String() != long.String() {
				t.Fatalf("shortcut output differs:\n%s\nwant:\n%s", &short, &long)
			}
		})
	}
}

func TestTreeSelectorKeepsCommitArgumentsSeparate(t *testing.T) {
	for _, args := range [][]string{nil, {"-F"}, {"-f"}, {"--model", "-tF"}, {"--", "-tG"}} {
		if _, selected := treeArguments(args); selected {
			t.Fatalf("commit arguments selected tree mode: %v", args)
		}
	}
	for _, flag := range []string{"-tp", "-tX"} {
		args, selected := treeArguments([]string{flag})
		if !selected {
			t.Fatalf("invalid tree flag fell through to committing: %s", flag)
		}
		if err := runTree(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid tree flag %s", flag)
		}
	}
}

func TestTreeOverview(t *testing.T) {
	t.Setenv("RT_DEPTH", "")
	t.Setenv("RT_MAX_FILES", "")
	base := t.TempDir()
	dirty := treeFixture(t, base, "nested/dirty repo")
	treeFixture(t, base, "clean")
	treeFixture(t, base, "node_modules/hidden")
	if err := os.WriteFile(filepath.Join(dirty, "changed"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runTree([]string{"--files", base}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nested", "dirty repo", "?? changed", "Repositories: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, &out)
		}
	}
	out.Reset()
	if err := runTree([]string{"--json", "--all", base}, &out); err != nil {
		t.Fatal(err)
	}
	var repos []treeRepo
	if err := json.Unmarshal(out.Bytes(), &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("repos=%v", repos)
	}
	out.Reset()
	if err := runTree([]string{"--depth", "2", base}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "dirty repo") {
		t.Fatal("depth ignored")
	}
}
func TestTreeGraphAndCycles(t *testing.T) {
	base := t.TempDir()
	a := treeFixture(t, base, "a")
	b := treeFixture(t, base, "b")
	for _, dir := range []string{a, b} {
		if _, err := treeGit(dir, "remote", "add", "origin", "https://github.com/fixture/"+filepath.Base(dir)+".git"); err != nil {
			t.Fatal(err)
		}
	}
	for dir, dep := range map[string]string{a: "b", b: "a"} {
		data := `{"root":"root","nodes":{"root":{"inputs":{"dep":"dep"}},"dep":{"locked":{"type":"github","owner":"fixture","repo":"` + dep + `"}}}}`
		if err := os.WriteFile(filepath.Join(dir, "flake.lock"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	repos, err := scanTree(base, 6, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos[0].Dependencies) != 1 || repos[0].Dependencies[0] != "b" {
		t.Fatalf("deps=%v", repos)
	}
	var out bytes.Buffer
	treeRenderer{out: &out, limit: 10}.renderDependencyGraph(repos)
	if !strings.Contains(out.String(), "cycle") {
		t.Fatal("cycle not marked")
	}
	if err := os.WriteFile(filepath.Join(a, "flake.lock"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanTree(base, 6, true); err == nil {
		t.Fatal("invalid lock accepted")
	}
}
func TestTreeWorktreeAndErrors(t *testing.T) {
	base := t.TempDir()
	repo := treeFixture(t, base, "repo")
	linked := filepath.Join(base, "linked")
	if _, err := treeGit(repo, "worktree", "add", "-qb", "linked", linked); err != nil {
		t.Fatal(err)
	}
	repos, err := scanTree(base, 6, false)
	if err != nil || len(repos) != 2 {
		t.Fatalf("worktrees: %v %v", repos, err)
	}
	if err := os.MkdirAll(filepath.Join(base, "broken", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := scanTree(base, 6, false); err == nil {
		t.Fatal("broken repository accepted")
	}
	var out bytes.Buffer
	for _, args := range [][]string{{"--depth", "0", base}, {"--invalid"}, {filepath.Join(base, "missing")}} {
		if runTree(args, &out) == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if strings.Contains(treeLabel("bad\x1b[0m"), "\x1b") {
		t.Fatal("terminal escape leaked")
	}
}

func TestTreeBranchParsing(t *testing.T) {
	for header, want := range map[string]branchState{
		"main...origin/main":                          {local: "main", upstream: "origin/main"},
		"main...origin/main [ahead 2, behind 13]":     {local: "main", upstream: "origin/main", ahead: 2, behind: 13},
		"feature...upstream/feature [gone]":           {local: "feature", upstream: "upstream/feature", gone: true},
		"main":                                        {local: "main"},
		"HEAD (no branch)":                            {detached: true},
		"No commits yet on main":                      {local: "main", unborn: true},
		"No commits yet on main...origin/main [gone]": {local: "main", upstream: "origin/main", unborn: true, gone: true},
	} {
		if got := parseBranch(header); got != want {
			t.Errorf("parseBranch(%q) = %+v, want %+v", header, got, want)
		}
	}
}

func TestTreeColorModes(t *testing.T) {
	var out bytes.Buffer
	base := t.TempDir()
	writeFile(t, filepath.Join(treeFixture(t, base, "dirty\x1b[31m"), "changed"), "fixture")
	if err := runTree([]string{"--color", "always", "--files", base}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[31m??\x1b[0m changed") || strings.Contains(out.String(), "dirty\x1b") {
		t.Fatalf("unexpected colored output: %q", &out)
	}
	out.Reset()
	if err := runTree([]string{"--color", "never", "--files", base}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("color leaked with --color never: %q", &out)
	}
	out.Reset()
	if err := runTree([]string{"--color", "always", "--json", base}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || !json.Valid(out.Bytes()) {
		t.Fatalf("JSON must remain machine-readable with forced color: %q", &out)
	}
}

func TestTreeRenderLayout(t *testing.T) {
	repos := []treeRepo{
		{Path: "dotfiles", Branch: "main...origin/main [behind 1]", Changes: []string{" M a.nix", "A  checks/b.nix", "?? c"}},
		{Path: "projects/labs/deep/repo", Branch: "main", Changes: []string{"UU conflict.txt"}},
		{Path: "projects/x", Branch: "dev...fork/dev [ahead 2]", Changes: []string{}},
	}
	var out bytes.Buffer
	treeRenderer{out: &out, files: true, limit: 2}.renderRepoTree("/base", repos)
	want := `/base
├── dotfiles  main ↓1 · 3 changed (1 staged, 1 unstaged, 1 untracked)
│      M a.nix
│     A  checks/b.nix
│     … 1 more file
└── projects
    ├── labs/deep/repo  main (no upstream) · 1 conflict
    │     UU conflict.txt
    └── x  dev → fork/dev ↑2 · clean
`
	if out.String() != want {
		t.Fatalf("tree layout:\n%s\nwant:\n%s", &out, want)
	}
}

func TestTreeGraphLayout(t *testing.T) {
	repos := []treeRepo{
		{Path: "app", Branch: "main...origin/main", Changes: []string{}, Dependencies: []string{"lib", "tool"}},
		{Path: "host", Branch: "main...origin/main", Changes: []string{}, Dependencies: []string{"lib"}},
		{Path: "lib", Branch: "main...origin/main", Changes: []string{" M x.go"}, Dependencies: []string{"tool"}},
		{Path: "loose", Branch: "main...origin/main", Changes: []string{"?? y"}},
		{Path: "quiet", Branch: "main...origin/main", Changes: []string{}},
		{Path: "tool", Branch: "main...origin/main", Changes: []string{}},
	}
	var out bytes.Buffer
	treeRenderer{out: &out}.renderDependencyGraph(repos)
	want := `consumer ─▶ input · local flake.lock matches
app  main · clean
├─▶ lib  main · 1 unstaged
│   └─▶ tool  main · clean
└─▶ tool (shown above)

host  main · clean
└─▶ lib (shown above)

without local flake links
  loose  main · 1 untracked
  … 1 clean repository hidden (use -A to list)
`
	if out.String() != want {
		t.Fatalf("graph layout:\n%s\nwant:\n%s", &out, want)
	}
}
