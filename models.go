package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

const (
	openRouterBaseURL   = "https://openrouter.ai/api/v1"
	openRouterEUBaseURL = "https://eu.openrouter.ai/api/v1"
)

var requiredModelParameters = []string{"temperature", "max_tokens", "response_format"}

type modelArchitecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type modelPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type userModel struct {
	ID                  string            `json:"id"`
	ContextLength       int               `json:"context_length"`
	Architecture        modelArchitecture `json:"architecture"`
	Pricing             modelPricing      `json:"pricing"`
	SupportedParameters []string          `json:"supported_parameters"`
}

type zdrEndpoint struct {
	ModelID             string       `json:"model_id"`
	ContextLength       int          `json:"context_length"`
	Pricing             modelPricing `json:"pricing"`
	SupportedParameters []string     `json:"supported_parameters"`
}

type modelListResponse struct {
	Data []userModel `json:"data"`
}

type endpointListResponse struct {
	Data []zdrEndpoint `json:"data"`
}

type compatibleModel struct {
	ID            string
	ContextLength int
	PromptPrice   float64
	OutputPrice   float64
	Default       bool
}

func listModels(ctx context.Context, cfg config, eu bool) error {
	if strings.TrimSpace(cfg.apiKey) == "" {
		return errors.New("OPENROUTER_API_KEY is not set")
	}
	base := cfg.apiBase
	if base == "" {
		base = openRouterBaseURL
		if eu {
			base = openRouterEUBaseURL
		}
	}
	userModels, err := fetchUserModels(ctx, cfg, base)
	if err != nil {
		return err
	}
	endpoints, err := fetchZDREndpoints(ctx, cfg, base)
	if err != nil {
		return err
	}
	compatible := intersectModels(userModels, endpoints)
	if len(compatible) == 0 {
		return errors.New("OpenRouter returned no models compatible with commitell, account guardrails, and ZDR")
	}

	fmt.Fprintln(cfg.out, "Models available for commitell")
	fmt.Fprintln(cfg.out, "Account privacy settings and guardrails applied.")
	if eu {
		fmt.Fprintln(cfg.out, "Routing: EU in-region + ZDR")
	} else {
		fmt.Fprintln(cfg.out, "Routing: global + ZDR")
	}
	fmt.Fprintln(cfg.out)
	tw := tabwriter.NewWriter(cfg.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tCONTEXT\tINPUT/1M\tOUTPUT/1M\tDEFAULT")
	for _, model := range compatible {
		isDefault := ""
		if model.Default {
			isDefault = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", model.ID, formatContext(model.ContextLength), formatPrice(model.PromptPrice), formatPrice(model.OutputPrice), isDefault)
	}
	return tw.Flush()
}

func fetchUserModels(ctx context.Context, cfg config, base string) ([]userModel, error) {
	const limit = 1000
	var all []userModel
	for offset := 0; ; offset += limit {
		endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/models/user")
		if err != nil {
			return nil, err
		}
		query := endpoint.Query()
		query.Set("limit", strconv.Itoa(limit))
		query.Set("offset", strconv.Itoa(offset))
		endpoint.RawQuery = query.Encode()
		var page modelListResponse
		if err := getJSON(ctx, cfg, endpoint.String(), &page); err != nil {
			return nil, fmt.Errorf("list OpenRouter user models: %w", err)
		}
		all = append(all, page.Data...)
		if len(page.Data) < limit {
			break
		}
	}
	return all, nil
}

func fetchZDREndpoints(ctx context.Context, cfg config, base string) ([]zdrEndpoint, error) {
	var response endpointListResponse
	if err := getJSON(ctx, cfg, strings.TrimRight(base, "/")+"/endpoints/zdr", &response); err != nil {
		return nil, fmt.Errorf("list OpenRouter ZDR endpoints: %w", err)
	}
	return response.Data, nil
}

func getJSON(ctx context.Context, cfg config, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Title", "commitell")
	response, err := cfg.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("OpenRouter returned %s: %s", response.Status, strings.TrimSpace(string(detail)))
	}
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode OpenRouter response: %w", err)
	}
	return nil
}

func intersectModels(userModels []userModel, endpoints []zdrEndpoint) []compatibleModel {
	byModel := make(map[string][]zdrEndpoint)
	for _, endpoint := range endpoints {
		if endpoint.ModelID != "" && supportsAll(endpoint.SupportedParameters, requiredModelParameters) {
			byModel[endpoint.ModelID] = append(byModel[endpoint.ModelID], endpoint)
		}
	}
	defaults := make(map[string]bool, len(models))
	for _, model := range models {
		defaults[model] = true
	}
	var result []compatibleModel
	for _, model := range userModels {
		available := byModel[model.ID]
		if len(available) == 0 || !supportsAll(model.SupportedParameters, requiredModelParameters) || !contains(model.Architecture.InputModalities, "text") || !contains(model.Architecture.OutputModalities, "text") {
			continue
		}
		contextLength := model.ContextLength
		prompt, output := parsePrice(model.Pricing.Prompt), parsePrice(model.Pricing.Completion)
		for _, endpoint := range available {
			if endpoint.ContextLength > contextLength {
				contextLength = endpoint.ContextLength
			}
			if price := parsePrice(endpoint.Pricing.Prompt); price >= 0 && (prompt < 0 || price < prompt) {
				prompt = price
			}
			if price := parsePrice(endpoint.Pricing.Completion); price >= 0 && (output < 0 || price < output) {
				output = price
			}
		}
		result = append(result, compatibleModel{ID: model.ID, ContextLength: contextLength, PromptPrice: prompt, OutputPrice: output, Default: defaults[model.ID]})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Default != result[j].Default {
			return result[i].Default
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func supportsAll(have, required []string) bool {
	set := make(map[string]bool, len(have))
	for _, item := range have {
		set[item] = true
	}
	for _, item := range required {
		if !set[item] {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func parsePrice(raw string) float64 {
	if raw == "" {
		return -1
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return -1
	}
	return value
}

func formatPrice(perToken float64) string {
	if perToken < 0 {
		return "n/a"
	}
	return fmt.Sprintf("$%.2f", perToken*1_000_000)
}

func formatContext(tokens int) string {
	if tokens <= 0 {
		return "n/a"
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return strconv.Itoa(tokens)
}
