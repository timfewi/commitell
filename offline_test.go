package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestRunOfflineDoesNotRequireAPIKeyOrContactModel(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "offline mode must not make a request", http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := newRepository(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	var out, errOut bytes.Buffer
	err := run(context.Background(), config{
		dir:      repo,
		endpoint: server.URL,
		client:   server.Client(),
		out:      &out,
		errOut:   &errOut,
		options:  options{offline: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("offline mode made %d HTTP request(s)", got)
	}
	message := git(t, repo, "log", "-1", "--pretty=format:%B")
	for _, want := range []string{
		"chore: update tracked.txt",
		"Signed-off-by: Test User <test@example.com>",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("offline commit message missing %q:\n%s", want, message)
		}
	}
	if !strings.Contains(out.String(), "committed with offline") {
		t.Fatalf("offline result not identified:\n%s", out.String())
	}
}

func TestOfflineSubjectUsesConservativeConventionalCommitTypes(t *testing.T) {
	tests := []struct {
		name string
		snap snapshot
		want string
	}{
		{
			name: "documentation",
			snap: snapshot{changes: []changeSnapshot{{
				change: change{Path: "README.md", Code: " M"},
			}}},
			want: "docs: update README.md",
		},
		{
			name: "new source",
			snap: snapshot{changes: []changeSnapshot{{
				change: change{Path: "internal/check.go", Code: "??", Untracked: true},
			}}},
			want: "feat: add check.go",
		},
		{
			name: "rename",
			snap: snapshot{changes: []changeSnapshot{{
				change: change{Path: "new.go", OldPath: "old.go", Code: "R "},
			}}},
			want: "refactor: rename old.go to new.go",
		},
		{
			name: "multiple tests",
			snap: snapshot{changes: []changeSnapshot{
				{change: change{Path: "main_test.go", Code: " M"}},
				{change: change{Path: "cli_test.go", Code: " M"}},
			}},
			want: "test: update tests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateOfflineMessage(tt.snap).Subject; got != tt.want {
				t.Fatalf("subject = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOfflineSubjectIsBoundedTo72Runes(t *testing.T) {
	subject := generateOfflineMessage(snapshot{changes: []changeSnapshot{{
		change: change{
			Path:      strings.Repeat("界", 100) + ".go",
			Code:      "??",
			Untracked: true,
		},
	}}}).Subject
	if got := utf8.RuneCountInString(subject); got > 72 {
		t.Fatalf("subject has %d runes: %q", got, subject)
	}
}
