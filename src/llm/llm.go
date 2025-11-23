package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"golang.org/x/time/rate"
)

// Adapter implements engine.LLMClient using OpenAI-compatible HTTP API.
type Adapter struct {
	clients map[string]*providerClient
	mu      sync.Mutex
	timeout time.Duration
}

type providerClient struct {
	endpoint   string
	apiKey     string
	proxy      *url.URL
	httpClient *http.Client
	limiter    *rate.Limiter
	models     map[string]*model.ModelDef
}

// New creates an LLM adapter from config.
func New(cfg *model.LLMConfig) *Adapter {
	a := &Adapter{
		clients: make(map[string]*providerClient),
		timeout: 120 * time.Second,
	}
	if cfg == nil {
		return a
	}
	if d, err := time.ParseDuration(cfg.RequestTimeout); err == nil {
		a.timeout = d
	}
	for name, p := range cfg.Providers {
		var proxyURL *url.URL
		if p.Proxy != "" {
			proxyURL, _ = url.Parse(p.Proxy)
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
		httpClient := &http.Client{
			Transport: transport,
			Timeout:   a.timeout,
		}
		apiKey := ""
		if v, ok := p.Credentials["api-key"]; ok {
			apiKey = v
		}
		rpm := 60
		if v, ok := cfg.RateLimit[name]; ok && v > 0 {
			rpm = v
		}
		limiter := rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm)

		modelMap := make(map[string]*model.ModelDef)
		for i := range p.Models {
			m := &p.Models[i]
			modelMap[m.Name] = m
		}

		a.clients[name] = &providerClient{
			endpoint:   p.Endpoint,
			apiKey:     apiKey,
			proxy:      proxyURL,
			httpClient: httpClient,
			limiter:    limiter,
			models:     modelMap,
		}
	}
	return a
}

// Generate sends a chat completion request and returns the response text.
func (a *Adapter) Generate(ctx context.Context, systemPrompt, userPrompt, providerModel string, temperature float64) (string, error) {
	providerName, modelName := "default", providerModel
	for name := range a.clients {
		if len(providerModel) > len(name) && providerModel[:len(name)] == name {
			providerName = name
			if len(providerModel) > len(name)+1 && providerModel[len(name)] == '/' {
				modelName = providerModel[len(name)+1:]
			}
			break
		}
	}

	pc, ok := a.clients[providerName]
	if !ok {
		return "", fmt.Errorf("provider not found: %s", providerName)
	}

	if err := pc.limiter.Wait(ctx); err != nil {
		return "", err
	}

	// Apply model-specific config (temperature, topk, modalities, thinking)
	if md, ok := pc.models[modelName]; ok {
		if md.Temperature > 0 {
			temperature = md.Temperature
		}
		if md.TopK > 0 {
			temperature = float64(md.TopK) // some APIs use top_k
		}
	}

	reqBody := map[string]any{
		"model": modelName,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": temperature,
	}

	if md, ok := pc.models[modelName]; ok {
		if md.TopK > 0 {
			reqBody["top_k"] = md.TopK
		}
		if md.Modalities != nil {
			reqBody["modalities"] = map[string]any{
				"input":  md.Modalities.Input,
				"output": md.Modalities.Output,
			}
		}
		if md.Thinking != nil {
			reqBody["thinking"] = map[string]any{
				"type":          md.Thinking.Type,
				"budget_tokens": md.Thinking.BudgetTokens,
			}
		}
	}

	jsonBody, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", pc.endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+pc.apiKey)

	resp, err := pc.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, string(body))
	}

	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &llmResp); err != nil {
		return "", fmt.Errorf("parse LLM response: %w", err)
	}
	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in LLM response")
	}
	return llmResp.Choices[0].Message.Content, nil
}
