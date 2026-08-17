package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type approvalStoreStub struct {
	items   map[string]*entities.ApprovalInfo
	updated *entities.ApprovalInfo
}

func (s *approvalStoreStub) Get(_ context.Context, token string) (*entities.ApprovalInfo, error) {
	if item := s.items[token]; item != nil {
		return item, nil
	}
	return nil, sql.ErrNoRows
}
func (s *approvalStoreStub) Select(context.Context, entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error) {
	return nil, nil
}
func (s *approvalStoreStub) Save(context.Context, *entities.ApprovalInfo) error { return nil }
func (s *approvalStoreStub) Delete(context.Context, string) error               { return nil }
func (s *approvalStoreStub) CreateApproval(context.Context, *entities.ApprovalInfo) error {
	return nil
}
func (s *approvalStoreStub) UpdateApproval(_ context.Context, item *entities.ApprovalInfo) error {
	copy := *item
	s.updated = &copy
	return nil
}
func (s *approvalStoreStub) ListPending(context.Context) ([]*entities.ApprovalInfo, error) {
	result := make([]*entities.ApprovalInfo, 0, len(s.items))
	for _, item := range s.items {
		if item.Status == "PENDING" {
			result = append(result, item)
		}
	}
	return result, nil
}

func TestHumanRunApprovalsAreNamespaceAndRunScoped(t *testing.T) {
	t.Parallel()
	store := &approvalStoreStub{items: map[string]*entities.ApprovalInfo{
		"token-a": {AgentFlowRunID: "run-a", Token: "token-a", Status: "PENDING"},
		"token-b": {AgentFlowRunID: "run-b", Token: "token-b", Status: "PENDING"},
	}}
	handler := &HumanHandler{
		store: store,
		runs: &flowRunStoreStub{runs: map[string]*entities.FlowRunInfo{
			"run-a": {BaseEntity: entities.BaseEntity{ID: "run-a", Namespace: "tenant-a"}},
			"run-b": {BaseEntity: entities.BaseEntity{ID: "run-b", Namespace: "tenant-b"}},
		}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/approvals", handler.ListRunApprovals)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/approvals/{token}/approve", handler.ApproveRun)

	listResponse := httptest.NewRecorder()
	mux.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/runs/run-a/approvals", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	var listed []*entities.ApprovalInfo
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || listed[0].Token != "token-a" {
		t.Fatalf("listed approvals = %+v", listed)
	}

	wrongRun := httptest.NewRecorder()
	mux.ServeHTTP(wrongRun, httptest.NewRequest(http.MethodPost, "/api/v1/tenant-a/runs/run-a/approvals/token-b/approve", nil))
	if wrongRun.Code != http.StatusNotFound || store.updated != nil {
		t.Fatalf("cross-run approve status = %d updated = %+v", wrongRun.Code, store.updated)
	}

	approved := httptest.NewRecorder()
	mux.ServeHTTP(approved, httptest.NewRequest(http.MethodPost, "/api/v1/tenant-a/runs/run-a/approvals/token-a/approve", nil))
	if approved.Code != http.StatusOK || store.updated == nil || store.updated.Status != "APPROVED" {
		t.Fatalf("approve status = %d updated = %+v", approved.Code, store.updated)
	}
}
