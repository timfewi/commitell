package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunStagedCommitsOnlyCapturedIndex(t *testing.T) {
	server := commitMessageServer(t, "feat: commit staged content")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "staged\n")
	git(t, repo, "add", "tracked.txt")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "unstaged\n")

	err := run(context.Background(), testConfig(repo, server, options{staged: true}))
	if err != nil {
		t.Fatal(err)
	}
	if got := git(t, repo, "show", "HEAD:tracked.txt"); got != "staged" {
		t.Fatalf("committed content = %q, want staged", got)
	}
	content, err := os.ReadFile(filepath.Join(repo, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "unstaged\n" {
		t.Fatalf("working content = %q", content)
	}
	if cached := git(t, repo, "diff", "--cached", "--name-only"); cached != "" {
		t.Fatalf("staged content remained after commit: %s", cached)
	}
	if status := git(t, repo, "status", "--porcelain"); !strings.Contains(status, "tracked.txt") {
		t.Fatalf("unstaged content disappeared: %q", status)
	}
}

func TestRunStagedRenameUsesOneAtomicChange(t *testing.T) {
	server := commitMessageServer(t, "refactor: rename tracked file")
	defer server.Close()
	repo := newRepository(t)
	git(t, repo, "mv", "tracked.txt", "renamed.txt")

	if err := run(context.Background(), testConfig(repo, server, options{staged: true})); err != nil {
		t.Fatal(err)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-status", "HEAD"); !strings.Contains(files, "tracked.txt") || !strings.Contains(files, "renamed.txt") {
		t.Fatalf("rename was not committed atomically: %q", files)
	}
	if status := git(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("rename left dirty state: %q", status)
	}
}

func TestRunStagedDeletion(t *testing.T) {
	server := commitMessageServer(t, "refactor: remove tracked file")
	defer server.Close()
	repo := newRepository(t)
	git(t, repo, "rm", "-q", "tracked.txt")

	if err := run(context.Background(), testConfig(repo, server, options{staged: true})); err != nil {
		t.Fatal(err)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-status", "HEAD"); !strings.Contains(files, "tracked.txt") {
		t.Fatalf("deletion missing from commit: %q", files)
	}
	if status := git(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("deletion left dirty state: %q", status)
	}
}

func TestRunExcludeLeavesFileUntouched(t *testing.T) {
	server := commitMessageServer(t, "feat: update selected file")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "bad.txt"), "initial\n")
	git(t, repo, "add", "bad.txt")
	git(t, repo, "commit", "-q", "-m", "chore: add bad file")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "selected\n")
	writeFile(t, filepath.Join(repo, "bad.txt"), "excluded\n")

	err := run(context.Background(), testConfig(repo, server, options{excludes: []string{"bad.txt"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := git(t, repo, "show", "HEAD:tracked.txt"); got != "selected" {
		t.Fatalf("selected file was not committed: %q", got)
	}
	if got := git(t, repo, "show", "HEAD:bad.txt"); got != "initial" {
		t.Fatalf("excluded file was committed: %q", got)
	}
	if status := git(t, repo, "status", "--porcelain"); !strings.Contains(status, "bad.txt") {
		t.Fatalf("excluded file no longer dirty: %q", status)
	}
}

func TestRunStagedExcludePreservesExcludedIndex(t *testing.T) {
	server := commitMessageServer(t, "feat: commit selected staged file")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "bad.txt"), "initial\n")
	git(t, repo, "add", "bad.txt")
	git(t, repo, "commit", "-q", "-m", "chore: add bad file")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "selected\n")
	writeFile(t, filepath.Join(repo, "bad.txt"), "excluded\n")
	git(t, repo, "add", "tracked.txt", "bad.txt")

	opts := options{staged: true, excludes: []string{"bad.txt"}}
	if err := run(context.Background(), testConfig(repo, server, opts)); err != nil {
		t.Fatal(err)
	}
	if got := git(t, repo, "show", "HEAD:bad.txt"); got != "initial" {
		t.Fatalf("excluded staged file was committed: %q", got)
	}
	if cached := git(t, repo, "diff", "--cached", "--name-only"); cached != "bad.txt" {
		t.Fatalf("cached files = %q, want bad.txt", cached)
	}
}

func TestRunRejectsUnknownExcludeBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")

	err := run(context.Background(), testConfig(repo, server, options{excludes: []string{"missing.txt"}}))
	if err == nil || !strings.Contains(err.Error(), `excluded file "missing.txt" is not part`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("made %d API requests", requests.Load())
	}
}

func TestRunSuggestsExcludeForProblemFile(t *testing.T) {
	server := commitMessageServer(t, "chore: should not happen")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, ".env"), "SECRET=value\n")

	err := run(context.Background(), testConfig(repo, server, options{}))
	if err == nil || !strings.Contains(err.Error(), `Try again with --exclude ".env".`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSplitCreatesOneCommitPerLogicalGroup(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		prompt := request.Messages[len(request.Messages)-1].Content
		content := `{"subject":"feat: update b","body":""}`
		if strings.Contains(prompt, "Partition the changed files") {
			content = `{"groups":[{"paths":["a.txt"]},{"paths":["b.txt"]}]}`
		} else if strings.Contains(prompt, "a.txt") {
			content = `{"subject":"feat: update a","body":""}`
		}
		w.Header().Set("Content-Type", "application/json")
		writeChatContent(t, w, content)
	}))
	defer server.Close()

	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	err := run(context.Background(), testConfig(repo, server, options{split: true}))
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("got %d requests, want split plan plus two messages", requests.Load())
	}
	if subjects := git(t, repo, "log", "-2", "--pretty=format:%s"); subjects != "feat: update b\nfeat: update a" {
		t.Fatalf("unexpected subjects:\n%s", subjects)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD~1"); files != "a.txt" {
		t.Fatalf("first split commit files = %q", files)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD"); files != "b.txt" {
		t.Fatalf("second split commit files = %q", files)
	}
}

func TestValidateSplitPlanRequiresExactCoverage(t *testing.T) {
	valid := splitPlan{}
	valid.Groups = append(valid.Groups, struct {
		Paths []string `json:"paths"`
	}{Paths: []string{"a.txt", "b.txt"}})
	if err := validateSplitPlan(valid, []string{"a.txt", "b.txt"}); err != nil {
		t.Fatal(err)
	}

	cases := []splitPlan{
		{Groups: []struct {
			Paths []string `json:"paths"`
		}{{Paths: []string{"a.txt"}}}},
		{Groups: []struct {
			Paths []string `json:"paths"`
		}{{Paths: []string{"a.txt", "a.txt", "b.txt"}}}},
		{Groups: []struct {
			Paths []string `json:"paths"`
		}{{Paths: []string{"a.txt", "b.txt", "unknown.txt"}}}},
	}
	for i, plan := range cases {
		if err := validateSplitPlan(plan, []string{"a.txt", "b.txt"}); err == nil {
			t.Fatalf("case %d accepted invalid plan", i)
		}
	}
}

func TestRunDryRunDoesNotChangeGit(t *testing.T) {
	server := commitMessageServer(t, "feat: preview change")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	before := git(t, repo, "rev-parse", "HEAD")
	var out bytes.Buffer
	cfg := testConfig(repo, server, options{dryRun: true})
	cfg.out = &out
	if err := run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if after := git(t, repo, "rev-parse", "HEAD"); after != before {
		t.Fatalf("dry run changed HEAD: %s -> %s", before, after)
	}
	if !strings.Contains(out.String(), "commitell: dry run") {
		t.Fatalf("missing dry-run output: %s", out.String())
	}
}

func TestRunUsesExplicitSolverFallbackOrder(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		requested = append(requested, request.Model)
		if request.Model == "model/first" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeChatContent(t, w, `{"subject":"feat: use fallback","body":""}`)
	}))
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	cfg := testConfig(repo, server, options{})
	cfg.models = []string{"model/first", "model/second"}
	if err := run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(requested, ","); got != "model/first,model/second" {
		t.Fatalf("solver order = %q", got)
	}
}

func TestRunPushesAndCreatesDraftPullRequest(t *testing.T) {
	server := commitMessageServer(t, "feat: publish change")
	defer server.Close()
	repo := newRepository(t)
	git(t, repo, "branch", "-M", "main")
	git(t, repo, "checkout", "-q", "-b", "feature")
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-q")
	git(t, repo, "remote", "add", "origin", remote)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "published\n")

	fakeBin := t.TempDir()
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	ghPath := filepath.Join(fakeBin, "gh")
	writeFile(t, ghPath, testString(t, "github.fake_gh_script"))
	if err := os.Chmod(ghPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_LOG", ghLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	opts := options{push: true, pullRequest: true, remote: "origin", base: "main"}
	if err := run(context.Background(), testConfig(repo, server, opts)); err != nil {
		t.Fatal(err)
	}
	if remoteHead := git(t, remote, "rev-parse", "refs/heads/feature"); remoteHead != git(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("remote feature branch was not pushed")
	}
	log, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"auth status",
		"pr view feature --json url --jq .url",
		"pr create --draft --fill --base main --head feature",
	} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("gh log missing %q:\n%s", want, log)
		}
	}
}

func TestRunComposesStagedExcludeSplitSolverAndPullRequest(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requested = append(requested, request.Model)
		if request.Model == "model/first" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		prompt := request.Messages[len(request.Messages)-1].Content
		content := `{"subject":"feat: commit b","body":""}`
		if strings.Contains(prompt, "Partition the changed files") {
			content = `{"groups":[{"paths":["a.txt"]},{"paths":["b.txt"]}]}`
		} else if strings.Contains(prompt, "a.txt") {
			content = `{"subject":"feat: commit a","body":""}`
		}
		w.Header().Set("Content-Type", "application/json")
		writeChatContent(t, w, content)
	}))
	defer server.Close()

	opts, err := parseOptions([]string{
		"--staged",
		"--exclude", "skip.txt",
		"--split",
		"--solver", "model/first",
		"--solver", "model/second",
		"--pr",
		"--remote", "origin",
		"--base", "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := newRepository(t)
	git(t, repo, "branch", "-M", "main")
	git(t, repo, "checkout", "-q", "-b", "feature")
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-q")
	git(t, repo, "remote", "add", "origin", remote)
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	writeFile(t, filepath.Join(repo, "skip.txt"), "skip\n")
	git(t, repo, "add", "a.txt", "b.txt", "skip.txt")

	fakeBin := t.TempDir()
	ghLog := filepath.Join(t.TempDir(), "gh.log")
	ghPath := filepath.Join(fakeBin, "gh")
	writeFile(t, ghPath, testString(t, "github.fake_gh_script"))
	if err := os.Chmod(ghPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_LOG", ghLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := testConfig(repo, server, opts)
	cfg.models = opts.solvers
	if err := run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(requested, ","); got != "model/first,model/second,model/first,model/second,model/first,model/second" {
		t.Fatalf("solver requests = %q", got)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD~1"); files != "a.txt" {
		t.Fatalf("first split commit files = %q", files)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD"); files != "b.txt" {
		t.Fatalf("second split commit files = %q", files)
	}
	if cached := git(t, repo, "diff", "--cached", "--name-only"); cached != "skip.txt" {
		t.Fatalf("excluded staged file was not preserved: %q", cached)
	}
	if remoteHead := git(t, remote, "rev-parse", "refs/heads/feature"); remoteHead != git(t, repo, "rev-parse", "HEAD") {
		t.Fatal("combined workflow did not push the resulting feature branch")
	}
	log, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "pr create --draft --fill --base main --head feature") {
		t.Fatalf("combined workflow did not create the expected draft PR:\n%s", log)
	}
}

func commitMessageServer(t *testing.T, subject string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		content, err := json.Marshal(commitMessage{Subject: subject})
		if err != nil {
			t.Error(err)
			return
		}
		writeChatContent(t, w, string(content))
	}))
}

func writeChatContent(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	response := map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{"content": content},
		}},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Error(err)
	}
}

func testConfig(repo string, server *httptest.Server, opts options) config {
	return config{
		dir:      repo,
		apiKey:   "test-key",
		endpoint: server.URL,
		client:   &http.Client{Timeout: time.Second},
		out:      &bytes.Buffer{},
		errOut:   &bytes.Buffer{},
		options:  opts,
	}
}
