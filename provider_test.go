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

func testEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestResolveProviderDefaultsToOpenRouter(t *testing.T) {
	selected, err := resolveProvider(testEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if selected.id != "openrouter" || selected.keyEnv != "OPENROUTER_API_KEY" || !selected.openRouter || !reflect.DeepEqual(selected.models, models) {
		t.Fatalf("default provider = %+v", selected)
	}
	if got := (config{}).service(); !got.openRouter || got.id != "openrouter" {
		t.Fatalf("zero-value config provider = %+v", got)
	}
}

func TestResolveProviderSelectsOpenCodeGo(t *testing.T) {
	selected, err := resolveProvider(testEnv(map[string]string{"COMMITELL_PROVIDER": "opencode-go"}))
	if err != nil {
		t.Fatal(err)
	}
	if selected.keyEnv != "OPENCODE_GO_API_KEY" || selected.openRouter || !selected.session || !reflect.DeepEqual(selected.models, []string{"glm-5.3-flash"}) {
		t.Fatalf("OpenCode Go provider = %+v", selected)
	}
}

func TestResolveProviderConfiguresOpenAICompatibleService(t *testing.T) {
	selected, err := resolveProvider(testEnv(map[string]string{
		"COMMITELL_PROVIDER": "openai-compatible",
		"COMMITELL_BASE_URL": "http://localhost:11434/v1/",
		"COMMITELL_MODELS":   " first, second,,",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if selected.baseURL != "http://localhost:11434/v1" || selected.name != "localhost:11434" || selected.keyEnv != "COMMITELL_API_KEY" || selected.openRouter || selected.session {
		t.Fatalf("OpenAI-compatible provider = %+v", selected)
	}
	if !reflect.DeepEqual(selected.models, []string{"first", "second"}) {
		t.Fatalf("models = %q", selected.models)
	}
}

func TestResolveProviderModelsReplaceDefaults(t *testing.T) {
	selected, err := resolveProvider(testEnv(map[string]string{"COMMITELL_MODELS": "openai/gpt-4o-mini"}))
	if err != nil {
		t.Fatal(err)
	}
	if !selected.openRouter || !reflect.DeepEqual(selected.models, []string{"openai/gpt-4o-mini"}) {
		t.Fatalf("provider = %+v", selected)
	}
	if len(models) != 3 {
		t.Fatalf("COMMITELL_MODELS changed the package defaults: %q", models)
	}
}

func TestResolveProviderRejectsInvalidConfiguration(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unknown provider":     {"COMMITELL_PROVIDER": "example"},
		"missing base URL":     {"COMMITELL_PROVIDER": "openai-compatible"},
		"unsupported scheme":   {"COMMITELL_PROVIDER": "openai-compatible", "COMMITELL_BASE_URL": "ftp://example.test/v1"},
		"URL without scheme":   {"COMMITELL_PROVIDER": "openai-compatible", "COMMITELL_BASE_URL": "localhost:11434"},
		"base URL on a preset": {"COMMITELL_BASE_URL": "https://example.test/v1"},
	} {
		if selected, err := resolveProvider(testEnv(env)); err == nil {
			t.Errorf("%s: accepted %+v", name, selected)
		}
	}
}

func TestCheckProviderOptionsRejectsOpenRouterOnlyOptions(t *testing.T) {
	for _, opts := range []options{{models: true}, {autoModel: true}, {eu: true}} {
		if err := checkProviderOptions(openCodeGoProvider, opts); err == nil || !strings.Contains(err.Error(), "OpenRouter") {
			t.Errorf("options %+v error = %v", opts, err)
		}
		if err := checkProviderOptions(openRouterProvider, opts); err != nil {
			t.Errorf("OpenRouter rejected %+v: %v", opts, err)
		}
	}
}

func TestOpenAICompatibleRequestOmitsPolicyAndSessionHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("x-opencode-session") != "" {
			t.Errorf("unexpected headers: %v", r.Header)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if _, ok := payload["provider"]; ok {
			t.Error("OpenRouter provider policy was sent to an OpenAI-compatible service")
		}
		var model string
		if err := json.Unmarshal(payload["model"], &model); err != nil || model != "local-model" {
			t.Errorf("model = %q, error = %v", model, err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"subject\":\"test: use a local model\",\"body\":\"\"}"}}]}`)
	}))
	defer server.Close()

	cfg := config{
		apiKey:   "test-key",
		endpoint: server.URL + "/chat/completions",
		provider: provider{id: openAICompatibleProviderID, name: "local", models: []string{"local-model"}},
		client:   server.Client(),
	}
	message, err := requestMessage(context.Background(), cfg, configuredModels(cfg)[0], "synthetic diff")
	if err != nil || message.Subject != "test: use a local model" {
		t.Fatalf("message = %+v, error = %v", message, err)
	}
}

func TestProviderErrorsNameTheProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config{
		apiKey:   "test-key",
		endpoint: server.URL,
		provider: provider{id: openAICompatibleProviderID, name: "models.example.test"},
		client:   server.Client(),
	}
	_, err := requestContent(context.Background(), cfg, "local-model", "synthetic diff", 800)
	if err == nil || !strings.HasPrefix(err.Error(), "models.example.test returned 503") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunRequiresKeyAndModelForOpenAICompatible(t *testing.T) {
	generic := provider{id: openAICompatibleProviderID, name: "local", keyEnv: "COMMITELL_API_KEY"}
	if err := run(context.Background(), config{provider: generic}); err == nil || !strings.Contains(err.Error(), "COMMITELL_API_KEY is not set") {
		t.Fatalf("missing key error = %v", err)
	}
	if err := run(context.Background(), config{apiKey: "test-key", provider: generic}); err == nil || !strings.Contains(err.Error(), "no model configured") {
		t.Fatalf("missing model error = %v", err)
	}
}
