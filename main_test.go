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
	"sync/atomic"
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

func TestOpenCodeGoRequestUsesItsModelAndOmitsOpenRouterPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("x-opencode-session") != "test-session" || !strings.HasPrefix(r.Header.Get("User-Agent"), "commitell/") {
			t.Errorf("unexpected OpenCode Go request: path=%q headers=%v", r.URL.Path, r.Header)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, ok := payload["provider"]; ok {
			t.Error("OpenRouter provider policy was sent to OpenCode Go")
		}
		var model string
		if err := json.Unmarshal(payload["model"], &model); err != nil || model != "glm-5.3-flash" {
			t.Errorf("model = %q, error = %v", model, err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"subject\":\"test: use OpenCode Go\",\"body\":\"\"}"}}]}`)
	}))
	defer server.Close()

	cfg := config{
		apiKey:    "test-key",
		endpoint:  server.URL + "/chat/completions",
		provider:  openCodeGoProvider,
		sessionID: "test-session",
		client:    server.Client(),
	}
	if got := configuredModels(cfg); len(got) != 1 || got[0] != "glm-5.3-flash" {
		t.Fatalf("OpenCode Go models = %v", got)
	}
	content, err := requestContent(context.Background(), cfg, configuredModels(cfg)[0], "synthetic diff", 800)
	if err != nil {
		t.Fatal(err)
	}
	if message, err := parseCommitMessage(content); err != nil || message.Subject != "test: use OpenCode Go" {
		t.Fatalf("message = %+v, error = %v", message, err)
	}
}

func TestOpenCodeGoRequiresItsOwnKey(t *testing.T) {
	err := run(context.Background(), config{provider: openCodeGoProvider})
	if err == nil || !strings.Contains(err.Error(), "OPENCODE_GO_API_KEY") {
		t.Fatalf("missing key error = %v", err)
	}
}

func TestSecretDetection(t *testing.T) {
	if findings := detectSecretPaths([]string{"src/main.go", ".env.example"}); len(findings) != 0 {
		t.Fatalf("safe paths produced findings: %+v", findings)
	}
	if findings := detectSecretPaths([]string{".env.production"}); len(findings) == 0 {
		t.Fatal("accepted secret path")
	}
	fakeKey := "sk-or-v1-" + "abcdefghijklmnopqrstuvwxyz"
	if findings := detectSecrets([]byte("+ OPENROUTER_API_KEY="+fakeKey+"\n"), "test.diff"); len(findings) == 0 {
		t.Fatal("accepted secret content")
	}
}

func TestRunConfirmsFlakeSecretFalsePositive(t *testing.T) {
	server := commitMessageServer(t, "test: add synthetic secret fixture")
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "flake.nix"), "secret = \"fixture-value-123456\"\n")

	cfg := testConfig(repo, server, options{})
	cfg.in = strings.NewReader("y\n")
	cfg.interactive = true
	var errOut bytes.Buffer
	cfg.errOut = &errOut
	if err := run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"flake.nix", "assigned secret", "Continue anyway? [y/N]", "continuing after secret warning confirmation"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("confirmation output missing %q:\n%s", want, errOut.String())
		}
	}
	if strings.Contains(errOut.String(), "fixture-value-123456") {
		t.Fatalf("confirmation output leaked the matched value:\n%s", errOut.String())
	}
	if files := git(t, repo, "show", "--pretty=", "--name-only", "HEAD"); files != "flake.nix" {
		t.Fatalf("confirmed commit files = %q", files)
	}
}

func TestRunDeclinesSecretBeforeRequestOrStaging(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "request must not be sent", http.StatusInternalServerError)
	}))
	defer server.Close()
	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "flake.nix"), "secret = \"fixture-value-123456\"\n")

	cfg := testConfig(repo, server, options{})
	cfg.in = strings.NewReader("n\n")
	cfg.interactive = true
	err := run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "cancelled by user") {
		t.Fatalf("confirmation error = %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("declined confirmation made %d model request(s)", got)
	}
	if staged := git(t, repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("declined confirmation staged %q", staged)
	}
}

func TestSecretConfirmationRequiresInteractiveTerminal(t *testing.T) {
	var errOut bytes.Buffer
	err := confirmSecretFindings(config{in: strings.NewReader("y\n"), errOut: &errOut}, []secretFinding{{
		Location: "flake.nix",
		Kind:     "assigned secret",
	}})
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("non-interactive confirmation error = %v", err)
	}
	if !strings.Contains(errOut.String(), "flake.nix") {
		t.Fatalf("non-interactive warning missing finding:\n%s", errOut.String())
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

func TestRunUsesPrivateFastFallbacksAndCommitsEverything(t *testing.T) {
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
		if request.Model != models[len(models)-1] {
			http.Error(w, "fast model unavailable", http.StatusServiceUnavailable)
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
	if len(requests) != len(models) {
		t.Fatalf("got %d model requests, want %d", len(requests), len(models))
	}
	for index, request := range requests {
		if request.Model != models[index] {
			t.Fatalf("fallback %d = %q, want %q", index, request.Model, models[index])
		}
		if !request.Provider.ZDR || request.Provider.DataCollection != "deny" || !request.Provider.RequireParams {
			t.Fatalf("privacy policy missing: %+v", request.Provider)
		}
		if strings.HasPrefix(request.Model, "openai/") {
			if request.MaxCompletionTokens == 0 || request.MaxTokens != 0 {
				t.Fatalf("OpenAI token limits are incompatible: %+v", request)
			}
		} else if request.MaxTokens == 0 || request.MaxCompletionTokens != 0 {
			t.Fatalf("model token limits are incompatible: %+v", request)
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
	git(t, repo, "config", "core.excludesFile", os.DevNull)
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
