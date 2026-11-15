package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAIConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		env     map[string]string
		opts    options
		want    []string
		invalid bool
	}{
		{name: "default", want: []string{"gpt-6-luna"}},
		{name: "environment", env: map[string]string{"COMMITELL_MODELS": " gpt-4o-mini, gpt-6-luna "}, want: []string{"gpt-4o-mini", "gpt-6-luna"}},
		{name: "explicit priority", env: map[string]string{"COMMITELL_MODELS": "bad,,list"}, opts: options{solvers: []string{"gpt-4o-mini"}}, want: []string{"gpt-4o-mini"}},
		{name: "empty entry", env: map[string]string{"COMMITELL_MODELS": "gpt-6-luna,,"}, invalid: true},
		{name: "offline ignores models", env: map[string]string{"COMMITELL_MODELS": "bad,,list"}, opts: options{offline: true}, want: []string{"gpt-6-luna"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := modelConfig(func(key string) string { return test.env[key] }, test.opts)
			if (err != nil) != test.invalid {
				t.Fatalf("configuration error = %v", err)
			}
			if err != nil {
				return
			}
			if cfg.endpoint != "https://api.openai.com/v1/chat/completions" || !reflect.DeepEqual(configuredModels(cfg), test.want) {
				t.Fatalf("endpoint = %s, models = %v", cfg.endpoint, configuredModels(cfg))
			}
			if test.opts.offline && len(cfg.models) != 0 {
				t.Fatal("offline config has model overrides")
			}
		})
	}
}

func TestLegacySettingsCannotChangeOpenAIDestinationOrKey(t *testing.T) {
	env := map[string]string{
		"COMMITELL_PROVIDER": "opencode-go", "COMMITELL_BASE_URL": "https://example.invalid/v1",
		"OPENROUTER_API_KEY": "synthetic-router-key", "OPENCODE_GO_API_KEY": "synthetic-other-key",
	}
	getenv := func(key string) string { return env[key] }
	cfg, err := modelConfig(getenv, options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.endpoint != "https://api.openai.com/v1/chat/completions" || cfg.apiKey != "" {
		t.Fatal("legacy settings changed OpenAI configuration")
	}
	if err := run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "COMMITELL_API_KEY is not set") {
		t.Fatalf("missing key error = %v", err)
	}
	env["COMMITELL_API_KEY"] = "synthetic-openai-key"
	cfg, err = modelConfig(getenv, options{})
	if err != nil || cfg.apiKey != "synthetic-openai-key" {
		t.Fatal("OpenAI key was not selected")
	}
}

func TestModelRequestParameters(t *testing.T) {
	for _, model := range []string{"gpt-6-luna", "gpt-4o-mini"} {
		t.Run(model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model               string            `json:"model"`
					MaxTokens           *int              `json:"max_tokens"`
					MaxCompletionTokens int               `json:"max_completion_tokens"`
					Temperature         float64           `json:"temperature"`
					ResponseFormat      map[string]string `json:"response_format"`
					ReasoningEffort     string            `json:"reasoning_effort"`
					Reasoning           json.RawMessage   `json:"reasoning"`
					Provider            json.RawMessage   `json:"provider"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					http.Error(w, "invalid", 400)
					return
				}
				effort := ""
				if model == "gpt-6-luna" {
					effort = "none"
				}
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" ||
					r.Header.Get("x-opencode-session") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "commitell/") ||
					payload.Model != model || payload.MaxTokens != nil || payload.MaxCompletionTokens != 800 ||
					payload.Temperature != 0.2 || payload.ResponseFormat["type"] != "json_object" ||
					payload.ReasoningEffort != effort || payload.Reasoning != nil || payload.Provider != nil {
					t.Errorf("incompatible request parameters: %+v", payload)
					http.Error(w, "incompatible", 400)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"subject\":\"test: verify model parameters\",\"body\":\"\"}"}}]}`)
			}))
			defer server.Close()
			cfg := config{apiKey: "test-key", endpoint: server.URL + "/v1/chat/completions", client: server.Client()}
			message, err := requestMessage(context.Background(), cfg, model, "synthetic diff")
			if err != nil || message.Subject != "test: verify model parameters" {
				t.Fatalf("message = %+v, error = %v", message, err)
			}
		})
	}
}

func TestOpenAIResponseFailures(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{name: "HTTP failure", status: 503, body: "unavailable", want: "OpenAI returned 503"},
		{name: "invalid JSON", status: 200, body: "not JSON", want: "decode OpenAI response"},
		{name: "empty choices", status: 200, body: `{"choices":[]}`, want: "OpenAI returned no choices"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status); fmt.Fprint(w, test.body) }))
			defer server.Close()
			_, err := requestContent(context.Background(), config{apiKey: "test-key", endpoint: server.URL, client: server.Client()}, defaultModel, "synthetic diff", 800)
			if err == nil || !strings.HasPrefix(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
