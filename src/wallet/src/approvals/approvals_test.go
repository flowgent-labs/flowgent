package approvals

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/wallet/src"
)

type mockStore struct {
	mu        sync.Mutex
	approvals map[string]*model.HumanApproval
}

func newMockStore() *mockStore {
	return &mockStore{approvals: make(map[string]*model.HumanApproval)}
}

func (s *mockStore) CreateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.Token = "tok-" + a.TaskRunID
	s.approvals[a.TaskRunID] = a
	return nil
}

func (s *mockStore) GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.approvals {
		if a.Token == token {
			return a, nil
		}
	}
	return nil, nil
}

func (s *mockStore) UpdateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approvals[a.TaskRunID] = a
	return nil
}

func TestPaymentApprover_New(t *testing.T) {
	store := newMockStore()
	approver := New(store, 1*time.Hour)
	if approver == nil {
		t.Fatal("approver should not be nil")
	}
}

func TestPaymentApprover_DefaultTimeout(t *testing.T) {
	store := newMockStore()
	approver := New(store, 0)
	if approver.timeout != 24*time.Hour {
		t.Errorf("default timeout should be 24h, got %s", approver.timeout)
	}
}

func TestPaymentApprover_RequestApproval(t *testing.T) {
	store := newMockStore()
	approver := New(store, 200*time.Millisecond)
	ctx := context.Background()

	intent := &payments.PaymentIntent{
		ID: "int-1", Asset: "USDC",
		Amount: decimal.NewFromFloat(10.0),
		Chain:  "base", Recipient: "0x1234",
	}

	// Start approval request in goroutine (it will time out since nobody resolves it)
	errCh := make(chan error, 1)
	go func() {
		_, err := approver.RequestApproval(ctx, intent)
		errCh <- err
	}()

	// Wait for timeout
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if perr, ok := err.(*payments.PaymentError); !ok || perr.Code != "APPROVAL_TIMEOUT" {
			t.Errorf("expected APPROVAL_TIMEOUT, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("test timed out waiting for approval timeout")
	}
}
