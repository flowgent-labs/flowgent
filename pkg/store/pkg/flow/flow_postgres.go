package flow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowPostgresStore is the PG-backed IFlowInfoStore implementation.
type FlowPostgresStore struct {
	inner *store.PostgresGenericStore[entities.FlowVersionInfo]
}

func NewFlowPostgresStore(pool *pgxpool.Pool) *FlowPostgresStore {
	return &FlowPostgresStore{
		inner: &store.PostgresGenericStore[entities.FlowVersionInfo]{
			Pool: pool, Table: "orh_agentflow", IDCol: "agentflow_id",
		},
	}
}

func (s *FlowPostgresStore) Get(ctx context.Context, id string) (*entities.FlowVersionInfo, error) {
	ver, err := s.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return ver, nil
}
func (s *FlowPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *FlowPostgresStore) Save(ctx context.Context, e *entities.FlowVersionInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s *FlowPostgresStore) GetVersion(ctx context.Context, id string, ver int64) (*entities.FlowVersionInfo, error) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT "+cols+" FROM orh_agentflow WHERE agentflow_id=$1 AND version=$2", id, ver)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("not found")
	}
	var v entities.FlowVersionInfo
	if err := utils.ScanStruct(rows, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *FlowPostgresStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	b, _ := json.Marshal(spec)
	var nextVer int64
	_ = s.inner.Pool.QueryRow(ctx,
		"SELECT COALESCE(MAX(version),0)+1 FROM orh_agentflow WHERE agentflow_id=$1", spec.ID).Scan(&nextVer)
	if nextVer == 0 {
		nextVer = 1
	}
	_, err := s.inner.Pool.Exec(ctx,
		`INSERT INTO orh_agentflow (id,agentflow_id,version,definition,created_by,comment,priority,namespace_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (agentflow_id,version) DO UPDATE SET definition=$4,comment=$6,priority=$7,updated_at=NOW()`,
		uuid.New().String(), spec.ID, nextVer, b, createdBy, comment, string(spec.Priority), spec.Namespace)
	return err
}
func (s *FlowPostgresStore) GetSpec(ctx context.Context, id string) (*entities.FlowInfo, error) {
	ver, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var spec entities.FlowInfo
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
