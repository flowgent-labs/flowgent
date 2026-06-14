package llm

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/flowgent-labs/flowgent/model/pkg"
)

// GeminiProvider implements ILlmProvider for Google Gemini API (stub).
// TODO: use google.golang.org/genai when available.
type GeminiProvider struct {
	apiKey   string
	endpoint string
	limiter  *rate.Limiter
}

func newGeminiProvider(p *model.LlmProvider) *GeminiProvider {
	apiKey := p.ApiKey
	timeout := defaultTimeout
	if d, err := time.ParseDuration(p.Timeout); err == nil && d > 0 {
		timeout = d
	}
	rpm := p.RateLimit
	if rpm <= 0 {
		rpm = 60
	}
	_ = timeout

	return &GeminiProvider{
		apiKey:   apiKey,
		endpoint: p.Endpoint,
		limiter:  rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm),
	}
}

func (p *GeminiProvider) Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return "", err
	}
	// TODO: implement Gemini HTTP API using google.golang.org/genai
	return "", fmt.Errorf("gemini: not yet implemented")
}
