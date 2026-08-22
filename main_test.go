package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseCommitMessage(t *testing.T) {
	message, err := parseCommitMessage(testString(t, "commit_message.valid"))
	if err != nil {
		t.Fatal(err)
	}
	if message.Subject != "feat(cli): commit all changes" {
		t.Fatalf("unexpected subject: %q", message.Subject)
	}
	for _, invalid := range []string{
		`{"subject":"","body":""}`,
		`{"subject":"bad\nsubject","body":""}`,
		`{"subject":"feat: x","body":"Signed-off-by: Model <ai@example.com>"}`,
		"```json\n{\"subject\":\"feat: x\",\"body\":\"\"}\n```",
	} {
		if _, err := parseCommitMessage(invalid); err == nil {
			t.Fatalf("accepted invalid message: %q", invalid)
		}
	}
}

func TestCurrentVersionUsesReleaseOverride(t *testing.T) {
	previous := version
	version = "v1.2.3"
	t.Cleanup(func() { version = previous })
	if got := currentVersion(); got != "1.2.3" {
		t.Fatalf("currentVersion() = %q, want 1.2.3", got)
	}
}

func TestSecretDetection(t *testing.T) {
	if err := scanSecretPaths([]string{"src/main.go", ".env.example"}); err != nil {
		t.Fatal(err)
	}
	if err := scanSecretPaths([]string{".env.production"}); err == nil {
		t.Fatal("accepted secret path")
	}
	fakeKey := "sk-or-v1-" + "abcdefghijklmnopqrstuvwxyz"
	if err := scanSecrets([]byte("+ OPENROUTER_API_KEY=" + fakeKey + "\n")); err == nil {
		t.Fatal("accepted secret content")
	}
}

func TestRunForceBypassesLocalSecretChecks(t *testing.T) {
	server := commitMessageServer(t, "chore: add environment configuration")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, ".env.production"), "OPENROUTER_API_KEY=sk-or-v1-abcdefghijklmnopqrstuvwxyz\n")

	if err := run(context.Background(), testConfig(repo, server, options{force: true})); err != nil {
		t.Fatal(err)
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD"); files != ".env.production" {
		t.Fatalf("force commit files = %q", files)
	}
}

func TestRunUsesPrivateFallbackAndCommitsEverything(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []chatRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		if request.Model == models[0] {
			http.Error(w, "primary unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, testString(t, "openrouter.private_fallback_response"))
	}))
	defer server.Close()

	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	writeFile(t, filepath.Join(repo, "new.txt"), "new\n")

	var out, errOut bytes.Buffer
	err := run(context.Background(), config{
		dir:      repo,
		apiKey:   "test-key",
		endpoint: server.URL,
		client:   &http.Client{Timeout: time.Second},
		out:      &out,
		errOut:   &errOut,
	})
	if err != nil {
		t.Fatal(err)
	}

	message := git(t, repo, "log", "-1", "--pretty=format:%B")
	for _, want := range []string{
		"feat: update tracked and new files",
		"Capture the complete working tree",
		"Signed-off-by: Test User <test@example.com>",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("commit message missing %q:\n%s", want, message)
		}
	}
	if status := git(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("working tree not clean: %s", status)
	}
	if got := git(t, repo, "show", "--pretty=", "--name-only", "HEAD"); !strings.Contains(got, "tracked.txt") || !strings.Contains(got, "new.txt") {
		t.Fatalf("commit did not include every file:\n%s", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("got %d model requests, want 2", len(requests))
	}
	if requests[1].Model != models[1] {
		t.Fatalf("fallback model = %q, want %q", requests[1].Model, models[1])
	}
	for _, request := range requests {
		if !request.Provider.ZDR || request.Provider.DataCollection != "deny" {
			t.Fatalf("privacy policy missing: %+v", request.Provider)
		}
	}
}

func TestRunLeavesIndexUntouchedWhenModelsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	before := git(t, repo, "status", "--porcelain=v1")
	err := run(context.Background(), config{
		dir:      repo,
		apiKey:   "test-key",
		endpoint: server.URL,
		client:   server.Client(),
		out:      &bytes.Buffer{},
		errOut:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected model failure")
	}
	after := git(t, repo, "status", "--porcelain=v1")
	if before != after {
		t.Fatalf("repository state changed:\nbefore %q\nafter  %q", before, after)
	}
	if cached := git(t, repo, "diff", "--cached", "--name-only"); cached != "" {
		t.Fatalf("index was modified: %s", cached)
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.name", "Test User")
	git(t, repo, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "initial\n")
	git(t, repo, "add", "tracked.txt")
	git(t, repo, "commit", "-q", "-m", "chore: initial")
	return repo
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
