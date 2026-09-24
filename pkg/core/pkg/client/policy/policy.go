// Package policy implements the spending policy engine for x402 payments.
// Policies are evaluated before any payment authorization to enforce
// budget limits, domain allowlists, and approval thresholds.
package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// Engine evaluates payment intents against configured spending policies.
type Engine struct {
	cfg   *config.PoliciesConfig
	store SpendingStore
}

// SpendingStore tracks daily spending for budget enforcement. ReserveSpend
// MUST compare and add atomically so concurrent clients cannot exceed maxTotal.
type SpendingStore interface {
	GetDailySpent(ctx context.Context, wallet string, date string) (decimal.Decimal, error)
	ReserveSpend(ctx context.Context, wallet string, date string, amount, maxTotal decimal.Decimal) error
}

// memorySpendingStore is an in-memory implementation for single-process mode.
type memorySpendingStore struct {
	mu     sync.Mutex
	ledger map[string]decimal.Decimal // "wallet:date" -> total
}

func (s *memorySpendingStore) GetDailySpent(_ context.Context, wallet, date string) (decimal.Decimal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := wallet + ":" + date
	if v, ok := s.ledger[key]; ok {
		return v, nil
	}
	return decimal.Zero, nil
}

func (s *memorySpendingStore) ReserveSpend(
	_ context.Context,
	wallet string,
	date string,
	amount decimal.Decimal,
	maxTotal decimal.Decimal,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := wallet + ":" + date
	current := s.ledger[key]
	next := current.Add(amount)
	if maxTotal.IsPositive() && next.GreaterThan(maxTotal) {
		return fmt.Errorf(
			"%w: daily budget %s would be exceeded (spent: %s, pending: %s)",
			model.ErrPaymentDenied,
			maxTotal,
			current,
			amount,
		)
	}
	s.ledger[key] = next
	return nil
}

// NewEngine creates a new policy engine.
func NewEngine(cfg *config.PoliciesConfig, store SpendingStore) *Engine {
	if store == nil {
		store = &memorySpendingStore{ledger: make(map[string]decimal.Decimal)}
	}
	return &Engine{
		cfg:   cfg,
		store: store,
	}
}

// Allow evaluates whether a payment intent is allowed by policy.
func (e *Engine) Allow(ctx context.Context, intent *model.PaymentIntent) error {
	if intent == nil {
		return fmt.Errorf("nil payment intent")
	}

	// Asset allowlist
	if len(e.cfg.AllowedAssets) > 0 && !containsFold(e.cfg.AllowedAssets, intent.Asset) {
		return fmt.Errorf("%w: asset %s not in allowed list", model.ErrPaymentDenied, intent.Asset)
	}

	// Chain allowlist
	if len(e.cfg.AllowedChains) > 0 && !containsFold(e.cfg.AllowedChains, intent.Chain) {
		return fmt.Errorf("%w: chain %s not in allowed list", model.ErrPaymentDenied, intent.Chain)
	}

	// Max single payment
	if e.cfg.MaxSinglePaymentUSD > 0 {
		limit := decimal.NewFromFloat(e.cfg.MaxSinglePaymentUSD)
		if intent.Amount.GreaterThan(limit) {
			return fmt.Errorf("%w: amount %s exceeds max single payment %s", model.ErrPaymentDenied, intent.Amount, limit)
		}
	}

	// Domain allowlist/blocklist
	if intent.URL != "" {
		domain := extractDomain(intent.URL)
		if err := e.checkDomain(domain); err != nil {
			return err
		}
	}

	// Daily budget
	if e.cfg.MaxDailyBudgetUSD > 0 {
		today := time.Now().Format("2006-01-02")
		if intent.Payer == "" {
			return fmt.Errorf("%w: payer address is required for daily budget enforcement", model.ErrPaymentDenied)
		}
		spent, err := e.store.GetDailySpent(ctx, intent.Payer, today)
		if err != nil {
			return fmt.Errorf("check daily budget: %w", err)
		}
		budget := decimal.NewFromFloat(e.cfg.MaxDailyBudgetUSD)
		if spent.Add(intent.Amount).GreaterThan(budget) {
			return fmt.Errorf("%w: daily budget %s would be exceeded (spent: %s, pending: %s)", model.ErrPaymentDenied, budget, spent, intent.Amount)
		}
	}

	return nil
}

// RequiresHumanApproval checks if the payment amount exceeds the approval threshold.
func (e *Engine) RequiresHumanApproval(intent *model.PaymentIntent) bool {
	if e.cfg.RequireHumanApprovalAboveUSD <= 0 {
		return false
	}
	threshold := decimal.NewFromFloat(e.cfg.RequireHumanApprovalAboveUSD)
	return intent.Amount.GreaterThanOrEqual(threshold)
}

// ReserveSpend atomically reserves a payment before network dispatch.
func (e *Engine) ReserveSpend(ctx context.Context, wallet string, amount decimal.Decimal) error {
	if wallet == "" {
		return fmt.Errorf("%w: payer address is required for daily budget enforcement", model.ErrPaymentDenied)
	}
	if !amount.IsPositive() {
		return fmt.Errorf("%w: payment amount must be positive", model.ErrPaymentDenied)
	}
	today := time.Now().Format("2006-01-02")
	limit := decimal.Zero
	if e.cfg.MaxDailyBudgetUSD > 0 {
		limit = decimal.NewFromFloat(e.cfg.MaxDailyBudgetUSD)
	}
	if err := e.store.ReserveSpend(ctx, wallet, today, amount, limit); err != nil {
		return fmt.Errorf("reserve daily budget: %w", err)
	}
	return nil
}

func (e *Engine) checkDomain(domain string) error {
	// Blocked domains take precedence
	for _, pattern := range e.cfg.BlockedDomains {
		if matchDomain(pattern, domain) {
			return fmt.Errorf("%w: domain %s is blocked (matches %s)", model.ErrPaymentDenied, domain, pattern)
		}
	}

	// If allowed domains are specified, domain must match one
	if len(e.cfg.AllowedDomains) > 0 {
		for _, pattern := range e.cfg.AllowedDomains {
			if matchDomain(pattern, domain) {
				return nil
			}
		}
		return fmt.Errorf("%w: domain %s not in allowed list", model.ErrPaymentDenied, domain)
	}

	return nil
}

func extractDomain(rawURL string) string {
	s := rawURL
	if after, found := strings.CutPrefix(s, "https://"); found {
		s = after
	} else if after, found := strings.CutPrefix(s, "http://"); found {
		s = after
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

func matchDomain(pattern, domain string) bool {
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(domain, suffix) || domain == pattern[2:]
	}
	return false
}

func containsFold(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}
