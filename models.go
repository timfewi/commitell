package main

import (
	"errors"
	"strings"
)

const defaultModel = "gpt-6-luna"
const openAIEndpoint = "https://api.openai.com/v1/chat/completions"

// modelConfig deliberately reads no provider or base URL overrides: credentials
// and selected changes always go directly to OpenAI.
func modelConfig(getenv func(string) string, opts options) (config, error) {
	cfg := config{apiKey: getenv("COMMITELL_API_KEY"), endpoint: openAIEndpoint, options: opts}
	if opts.offline {
		return cfg, nil
	}
	cfg.models = opts.solvers
	if len(cfg.models) == 0 {
		if raw := strings.TrimSpace(getenv("COMMITELL_MODELS")); raw != "" {
			for _, model := range strings.Split(raw, ",") {
				model = strings.TrimSpace(model)
				if model == "" {
					return config{}, errors.New("COMMITELL_MODELS contains an empty model")
				}
				cfg.models = append(cfg.models, model)
			}
		}
	}
	return cfg, nil
}
