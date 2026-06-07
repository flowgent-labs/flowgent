package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"golang.org/x/time/rate"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// AnthropicProvider implements ILlmProvider for the Anthropic Messages API.
type AnthropicProvider struct {
	client  *anthropic.Client
	limiter *rate.Limiter
}

func newAnthropicProvider(p config.LLMProviderDef) *AnthropicProvider {
	apiKey := ""
	if v, ok := p.Credentials["apikey"]; ok {
		apiKey = v
	}
	timeout := defaultTimeout
	if d, err := time.ParseDuration(p.Timeout); err == nil && d > 0 {
		timeout = d
	}
	rpm := p.RateLimit
	if rpm <= 0 {
		rpm = 60
	}

	opts := []anthropicopt.RequestOption{
		anthropicopt.WithAPIKey(apiKey),
		anthropicopt.WithRequestTimeout(timeout),
	}
	if p.Endpoint != "" {
		opts = append(opts, anthropicopt.WithBaseURL(p.Endpoint))
	}
	c := anthropic.NewClient(opts...)
	return &AnthropicProvider{
		client:  &c,
		limiter: rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm),
	}
}

func (p *AnthropicProvider) Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return "", err
	}

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

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("anthropic: empty response")
	}
	return msg.Content[0].Text, nil
}
