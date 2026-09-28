package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const openAICompatibleProviderID = "openai-compatible"

// provider describes an OpenAI-compatible chat-completions service. Only
// OpenRouter supports the ZDR routing policy and model discovery.
type provider struct {
	id         string   // COMMITELL_PROVIDER value
	name       string   // name used in messages
	baseURL    string   // API base without the /chat/completions suffix
	keyEnv     string   // environment variable holding the API key
	models     []string // default fallback order
	openRouter bool     // send the ZDR policy; allow --models, --auto-model and --eu
	session    bool     // send a per-run x-opencode-session header
}

var (
	openRouterProvider = provider{
		id:         "openrouter",
		name:       "OpenRouter",
		baseURL:    openRouterBaseURL,
		keyEnv:     "OPENROUTER_API_KEY",
		models:     models,
		openRouter: true,
	}
	openCodeGoProvider = provider{
		id:      "opencode-go",
		name:    "OpenCode Go",
		baseURL: "https://opencode.ai/zen/go/v1",
		keyEnv:  "OPENCODE_GO_API_KEY",
		models:  []string{"glm-5.3-flash"},
		session: true,
	}
)

// resolveProvider selects the provider named by COMMITELL_PROVIDER. The
// openai-compatible provider reaches any service that implements the OpenAI
// chat-completions API at COMMITELL_BASE_URL. COMMITELL_MODELS replaces the
// default fallback order of every provider.
func resolveProvider(getenv func(string) string) (provider, error) {
	var selected provider
	baseURL := strings.TrimSpace(getenv("COMMITELL_BASE_URL"))
	switch id := strings.TrimSpace(getenv("COMMITELL_PROVIDER")); id {
	case "", openRouterProvider.id:
		selected = openRouterProvider
	case openCodeGoProvider.id:
		selected = openCodeGoProvider
	case openAICompatibleProviderID:
		if baseURL == "" {
			return provider{}, errors.New("COMMITELL_PROVIDER=openai-compatible requires COMMITELL_BASE_URL")
		}
		parsed, err := url.Parse(baseURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return provider{}, fmt.Errorf("COMMITELL_BASE_URL must be an http or https URL, got %q", baseURL)
		}
		selected = provider{
			id:      id,
			name:    parsed.Host,
			baseURL: strings.TrimRight(baseURL, "/"),
			keyEnv:  "COMMITELL_API_KEY",
		}
	default:
		return provider{}, fmt.Errorf("unsupported COMMITELL_PROVIDER %q; use openrouter, opencode-go, or openai-compatible", id)
	}
	if baseURL != "" && selected.id != openAICompatibleProviderID {
		return provider{}, errors.New("COMMITELL_BASE_URL requires COMMITELL_PROVIDER=openai-compatible")
	}
	if list := getenv("COMMITELL_MODELS"); strings.TrimSpace(list) != "" {
		selected.models = nil
		for _, model := range strings.Split(list, ",") {
			if model = strings.TrimSpace(model); model != "" {
				selected.models = append(selected.models, model)
			}
		}
	}
	return selected, nil
}

// checkProviderOptions rejects options that only OpenRouter implements.
func checkProviderOptions(selected provider, opts options) error {
	if !selected.openRouter && (opts.models || opts.autoModel || opts.eu) {
		return fmt.Errorf("--models, --auto-model, and --eu require the OpenRouter provider, not %s", selected.id)
	}
	return nil
}

// service returns the configured provider. The zero value selects OpenRouter,
// so its privacy policy is never dropped by omission.
func (cfg config) service() provider {
	if cfg.provider.id == "" {
		return openRouterProvider
	}
	return cfg.provider
}
