package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"golang.org/x/time/rate"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const defaultTimeout = 120 * time.Second

// OpenAIProvider implements ILlmProvider for OpenAI-compatible APIs
// (OpenAI, DashScope/Bailian, DeepSeek, etc.).
type OpenAIProvider struct {
	client  *openai.Client
	limiter *rate.Limiter
}

func newOpenAIProvider(p *entities.LlmProviderInfo) *OpenAIProvider {
	p.NormalizeAliases()
	apiKey := p.ApiKey
	timeout := defaultTimeout
	if d, err := time.ParseDuration(p.Timeout); err == nil && d > 0 {
		timeout = d
	}
	rpm := p.RateLimit
	if rpm <= 0 {
		rpm = 60
	}

	opts := []openaiopt.RequestOption{
		openaiopt.WithAPIKey(apiKey),
		openaiopt.WithRequestTimeout(timeout),
	}
	if p.BaseURI != "" {
		opts = append(opts, openaiopt.WithBaseURL(p.BaseURI))
	}
	c := openai.NewClient(opts...)
	return &OpenAIProvider{
		client:  &c,
		limiter: rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm),
	}
}

func (p *OpenAIProvider) Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64, maxTokens int) (string, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return "", err
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	params := openai.ChatCompletionNewParams{
		Model: modelName,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		Temperature: openai.Float(temperature),
		MaxTokens:   openai.Int(int64(maxTokens)),
	}

	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices in response")
	}
	return completion.Choices[0].Message.Content, nil
}
