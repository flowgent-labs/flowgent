package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"golang.org/x/time/rate"

	"github.com/flowgent-labs/flowgent/config/src"
)

const defaultTimeout = 120 * time.Second

// Adapter implements engine.LLMClient using OpenAI and Anthropic Go SDKs.
type Adapter struct {
	clients map[string]*llmProviderClient
	mu      sync.Mutex
}

type llmProviderClient struct {
	providerType string // "openai" | "anthropic"
	openai       *openai.Client
	anthropic    *anthropic.Client
	limiter      *rate.Limiter
	models       map[string]*config.ModelDef
}

func newOpenAIClient(apiKey, endpoint string, timeout time.Duration) *openai.Client {
	opts := []openaiopt.RequestOption{
		openaiopt.WithAPIKey(apiKey),
		openaiopt.WithRequestTimeout(timeout),
	}
	if endpoint != "" {
		opts = append(opts, openaiopt.WithBaseURL(endpoint))
	}
	c := openai.NewClient(opts...)
	return &c
}

func newAnthropicClient(apiKey, endpoint string, timeout time.Duration) *anthropic.Client {
	opts := []anthropicopt.RequestOption{
		anthropicopt.WithAPIKey(apiKey),
		anthropicopt.WithRequestTimeout(timeout),
	}
	if endpoint != "" {
		opts = append(opts, anthropicopt.WithBaseURL(endpoint))
	}
	c := anthropic.NewClient(opts...)
	return &c
}

// New creates an LLM adapter from config.
func New(cfg *config.LLMConfig) *Adapter {
	a := &Adapter{clients: make(map[string]*llmProviderClient)}
	if cfg == nil {
		return a
	}
	for _, p := range cfg.Providers.Static {
		if !p.Enabled || p.ID == "" {
			continue
		}
		timeout := defaultTimeout
		if d, err := time.ParseDuration(p.Timeout); err == nil && d > 0 {
			timeout = d
		}
		apiKey := ""
		if v, ok := p.Credentials["apikey"]; ok {
			apiKey = v
		}
		rpm := p.RateLimit
		if rpm <= 0 {
			rpm = 60
		}

		modelMap := make(map[string]*config.ModelDef)
		for i := range p.Models {
			m := &p.Models[i]
			modelMap[m.Name] = m
		}

		pc := &llmProviderClient{
			providerType: p.Type,
			limiter:      rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm),
			models:       modelMap,
		}

		switch p.Type {
		case "anthropic":
			pc.anthropic = newAnthropicClient(apiKey, p.Endpoint, timeout)
		default: // "openai", "dashscope", "deepseek", or empty → OpenAI-compatible
			pc.openai = newOpenAIClient(apiKey, p.Endpoint, timeout)
		}

		a.clients[p.ID] = pc
	}
	return a
}

// Generate sends a chat completion request and returns the response text.
func (a *Adapter) Generate(ctx context.Context, systemPrompt, userPrompt, providerModel string, temperature float64) (string, error) {
	providerID, modelName := resolveProvider(providerModel, a.clients)
	pc, ok := a.clients[providerID]
	if !ok {
		return "", fmt.Errorf("llm provider not found: %s", providerID)
	}

	if err := pc.limiter.Wait(ctx); err != nil {
		return "", err
	}

	if md, ok := pc.models[modelName]; ok && md.Temperature > 0 {
		temperature = md.Temperature
	}

	switch pc.providerType {
	case "anthropic":
		return pc.generateAnthropic(ctx, systemPrompt, userPrompt, modelName, temperature)
	default:
		return pc.generateOpenAI(ctx, systemPrompt, userPrompt, modelName, temperature)
	}
}

func (pc *llmProviderClient) generateOpenAI(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error) {
	params := openai.ChatCompletionNewParams{
		Model: modelName,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		Temperature: openai.Float(temperature),
		MaxTokens:   openai.Int(8192),
	}

	completion, err := pc.openai.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices in response")
	}
	return completion.Choices[0].Message.Content, nil
}

func (pc *llmProviderClient) generateAnthropic(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error) {
	params := anthropic.MessageNewParams{
		Model: modelName,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		MaxTokens:   8192,
		Temperature: anthropic.Float(temperature),
	}

	msg, err := pc.anthropic.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty response")
	}
	return msg.Content[0].Text, nil
}

func resolveProvider(providerModel string, clients map[string]*llmProviderClient) (providerID, modelName string) {
	providerID, modelName = "default", providerModel
	for id := range clients {
		if len(providerModel) > len(id) && providerModel[:len(id)] == id {
			providerID = id
			if len(providerModel) > len(id)+1 && providerModel[len(id)] == '/' {
				modelName = providerModel[len(id)+1:]
			}
			break
		}
	}
	return
}

