package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := treeCommand([]string{"--files", base}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nested", "dirty repo", "?? changed", "Repositories: 1"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, &out)
		}
	}
	out.Reset()
	if err := treeCommand([]string{"--json", "--all", base}, &out); err != nil {
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
	if err := treeCommand([]string{"--depth", "2", base}, &out); err != nil {
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
	renderDependencyGraph(&out, repos, false, 10)
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
		if treeCommand(args, &out) == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if strings.Contains(treeLabel("bad\x1b[0m"), "\x1b") {
		t.Fatal("terminal escape leaked")
	}
}
