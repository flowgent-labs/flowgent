package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"golang.org/x/time/rate"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// AnthropicProvider implements ILlmProvider for the Anthropic Messages API.
type AnthropicProvider struct {
	client  *anthropic.Client
	limiter *rate.Limiter
}

func newAnthropicProvider(p *entities.LlmProviderInfo) *AnthropicProvider {
	apiKey := p.ApiKey
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

func (p *AnthropicProvider) Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64, maxTokens int) (string, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return "", err
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	params := anthropic.MessageNewParams{
		Model: modelName,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		MaxTokens:   int64(maxTokens),
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
