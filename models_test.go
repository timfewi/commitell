package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListModelsIntersectsAccountGuardrailsZDRAndCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models/user":
			fmt.Fprint(w, testString(t, "openrouter.user_models"))
		case "/endpoints/zdr":
			fmt.Fprint(w, testString(t, "openrouter.zdr_endpoints"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	err := listModels(context.Background(), config{
		apiKey:  "test-key",
		apiBase: server.URL,
		client:  &http.Client{Timeout: time.Second},
		out:     &out,
		errOut:  &bytes.Buffer{},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"Account privacy settings and guardrails applied.",
		"Routing: EU in-region + ZDR",
		"google/gemini-3.1-flash-lite",
		"1000k",
		"$0.10",
		"$0.40",
		"yes",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("model output missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"blocked/no-zdr", "image/only"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("model output contains %q:\n%s", unwanted, text)
		}
	}
}

func TestListModelsRequiresAPIKey(t *testing.T) {
	err := listModels(context.Background(), config{client: http.DefaultClient}, false)
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAutoSelectModelsRanksCompatibleLargeContextModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models/user":
			fmt.Fprintf(w, `{"data":[
				{"id":%q,"context_length":1048576,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000001","completion":"0.0000004"},"supported_parameters":["temperature","max_tokens","response_format"]},
				{"id":"large/fallback","context_length":262144,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000002","completion":"0.0000002"},"supported_parameters":["temperature","max_tokens","response_format"]},
				{"id":"small/skip","context_length":32768,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000001","completion":"0.0000001"},"supported_parameters":["temperature","max_tokens","response_format"]}
			]}`, models[0])
		case "/endpoints/zdr":
			fmt.Fprintf(w, `{"data":[
				{"model_id":%q,"context_length":1048576,"pricing":{"prompt":"0.0000001","completion":"0.0000004"},"supported_parameters":["temperature","max_tokens","response_format"]},
				{"model_id":"large/fallback","context_length":262144,"pricing":{"prompt":"0.0000002","completion":"0.0000002"},"supported_parameters":["temperature","max_tokens","response_format"]},
				{"model_id":"small/skip","context_length":32768,"pricing":{"prompt":"0.0000001","completion":"0.0000001"},"supported_parameters":["temperature","max_tokens","response_format"]}
			]}`, models[0])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	selected, err := autoSelectModels(context.Background(), config{
		apiKey:  "test-key",
		apiBase: server.URL,
		client:  server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(selected, ","); got != models[0]+",large/fallback" {
		t.Fatalf("automatic model order = %q", got)
	}
}
