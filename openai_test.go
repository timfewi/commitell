package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelRequestParameters(t *testing.T) {
	for _, test := range []struct {
		name             string
		model            string
		openRouter       bool
		completionTokens bool
		reasoningEffort  string
		routerReasoning  bool
	}{
		{name: "direct Luna", model: "gpt-6-luna", completionTokens: true, reasoningEffort: "none"},
		{name: "OpenRouter Luna", model: "openai/gpt-6-luna", openRouter: true, completionTokens: true, routerReasoning: true},
		{name: "local model", model: "local-model"},
		{name: "OpenRouter legacy", model: "openai/gpt-4o-mini", openRouter: true, completionTokens: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model               string            `json:"model"`
					MaxTokens           *int              `json:"max_tokens"`
					MaxCompletionTokens *int              `json:"max_completion_tokens"`
					Temperature         float64           `json:"temperature"`
					ResponseFormat      map[string]string `json:"response_format"`
					ReasoningEffort     string            `json:"reasoning_effort"`
					Reasoning           map[string]string `json:"reasoning"`
					Provider            *providerPolicy   `json:"provider"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				valid := r.URL.Path == "/v1/chat/completions" && r.Header.Get("Authorization") == "Bearer test-key"
				valid = valid && payload.Model == test.model && payload.Temperature == 0.2 && payload.ResponseFormat["type"] == "json_object"
				if test.completionTokens {
					valid = valid && payload.MaxTokens == nil && payload.MaxCompletionTokens != nil && *payload.MaxCompletionTokens == 800
				} else {
					valid = valid && payload.MaxCompletionTokens == nil && payload.MaxTokens != nil && *payload.MaxTokens == 800
				}
				valid = valid && payload.ReasoningEffort == test.reasoningEffort
				if test.routerReasoning {
					valid = valid && len(payload.Reasoning) == 1 && payload.Reasoning["effort"] == "none"
				} else {
					valid = valid && payload.Reasoning == nil
				}
				if test.openRouter {
					valid = valid && payload.Provider != nil && payload.Provider.ZDR && payload.Provider.DataCollection == "deny" && payload.Provider.RequireParams && payload.Provider.AllowFallbacks
				} else {
					valid = valid && payload.Provider == nil
				}
				if !valid {
					t.Errorf("incompatible request parameters: %+v", payload)
					http.Error(w, "incompatible request parameters", http.StatusBadRequest)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"subject\":\"test: verify model parameters\",\"body\":\"\"}"}}]}`)
			}))
			defer server.Close()

			cfg := config{
				apiKey:   "test-key",
				endpoint: server.URL + "/v1/chat/completions",
				provider: provider{id: openAICompatibleProviderID, name: "test provider", openRouter: test.openRouter},
				client:   server.Client(),
			}
			message, err := requestMessage(context.Background(), cfg, test.model, "synthetic diff")
			if err != nil || message.Subject != "test: verify model parameters" {
				t.Fatalf("message = %+v, error = %v", message, err)
			}
		})
	}
}

func TestIntersectModelsRequiresLunaReasoning(t *testing.T) {
	for _, supported := range []bool{false, true} {
		parameters := []string{"temperature", "max_completion_tokens", "response_format"}
		if supported {
			parameters = append(parameters, "reasoning")
		}
		compatible := intersectModels(
			[]userModel{{
				ID: "openai/gpt-6-luna", ContextLength: 128000,
				Architecture:        modelArchitecture{InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
				SupportedParameters: []string{"temperature", "max_completion_tokens", "response_format", "reasoning"},
			}},
			[]zdrEndpoint{{ModelID: "openai/gpt-6-luna", ContextLength: 128000, SupportedParameters: parameters}},
		)
		if got := len(compatible) == 1; got != supported {
			t.Errorf("reasoning supported = %v, compatible models = %+v", supported, compatible)
		}
	}
}
