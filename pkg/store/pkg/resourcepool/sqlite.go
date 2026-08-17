package resourcepool

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
)

type sqliteRepository struct{ db *sql.DB }

const sqlitePoolColumns = `id,name,replicas,slots_per_pod,resources,sandbox_replicas,
	sandbox_slots_per_pod,sandbox_resources,priority_class_name,node_selector,description,
	namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag`

func scanSQLitePool(row interface{ Scan(...any) error }) (*entities.ResourcePoolInfo, error) {
	var item entities.ResourcePoolInfo
	var resources, sandboxResources, nodeSelector, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.Name, &item.Replicas, &item.SlotsPerPod, &resources,
		&item.SandboxReplicas, &item.SandboxSlotsPerPod, &sandboxResources,
		&item.PriorityClassName, &nodeSelector, &item.Description, &item.Namespace,
		&item.Status, &createdAt, &item.CreatedBy, &updatedAt, &item.UpdatedBy, &item.DelFlag)
	if err != nil {
		return nil, normalizeError(err)
	}
	_ = json.Unmarshal([]byte(resources), &item.Resources)
	_ = json.Unmarshal([]byte(sandboxResources), &item.SandboxResources)
	_ = json.Unmarshal([]byte(nodeSelector), &item.NodeSelector)
	item.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	item.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return &item, nil
}

func (s *sqliteRepository) List(ctx context.Context, namespace string) ([]*entities.ResourcePoolInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sqlitePoolColumns+` FROM orh_resource_pool
		WHERE namespace_id=? AND del_flag=0 ORDER BY name`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.ResourcePoolInfo, 0)
	for rows.Next() {
		item, err := scanSQLitePool(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqliteRepository) Get(ctx context.Context, namespace, name string) (*entities.ResourcePoolInfo, error) {
	return scanSQLitePool(s.db.QueryRowContext(ctx, `SELECT `+sqlitePoolColumns+` FROM orh_resource_pool
		WHERE namespace_id=? AND LOWER(name)=LOWER(?) AND del_flag=0`, namespace, name))
}

func (s *sqliteRepository) Create(ctx context.Context, item *entities.ResourcePoolInfo) error {
	entities.NormalizeResourcePool(item)
	item.ID = uuid.NewString()
	item.MarkCreated(item.CreatedBy)
	resources, _ := json.Marshal(item.Resources)
	sandboxResources, _ := json.Marshal(item.SandboxResources)
	nodeSelector, _ := json.Marshal(item.NodeSelector)
	_, err := s.db.ExecContext(ctx, `INSERT INTO orh_resource_pool (`+sqlitePoolColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`, item.ID, item.Name, item.Replicas,
		item.SlotsPerPod, string(resources), item.SandboxReplicas, item.SandboxSlotsPerPod,
		string(sandboxResources), item.PriorityClassName, string(nodeSelector), item.Description,
		item.Namespace, item.Status, formatPoolTime(item.CreatedAt), item.CreatedBy,
		formatPoolTime(item.UpdatedAt), item.UpdatedBy)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrAlreadyExists
	}
	return err
}

func (s *sqliteRepository) Update(ctx context.Context, item *entities.ResourcePoolInfo) error {
	entities.NormalizeResourcePool(item)
	resources, _ := json.Marshal(item.Resources)
	sandboxResources, _ := json.Marshal(item.SandboxResources)
	nodeSelector, _ := json.Marshal(item.NodeSelector)
	item.UpdatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE orh_resource_pool SET replicas=?,slots_per_pod=?,resources=?,
		sandbox_replicas=?,sandbox_slots_per_pod=?,sandbox_resources=?,priority_class_name=?,node_selector=?,
		description=?,updated_at=?,updated_by=? WHERE namespace_id=? AND LOWER(name)=LOWER(?) AND del_flag=0`,
		item.Replicas, item.SlotsPerPod, string(resources), item.SandboxReplicas,
		item.SandboxSlotsPerPod, string(sandboxResources), item.PriorityClassName, string(nodeSelector),
		item.Description, formatPoolTime(item.UpdatedAt), item.UpdatedBy, item.Namespace, item.Name)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteRepository) Delete(ctx context.Context, namespace, name, actor string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE orh_resource_pool SET del_flag=1,status='DELETED',updated_at=datetime('now'),updated_by=?
		WHERE namespace_id=? AND LOWER(name)=LOWER(?) AND del_flag=0`, actor, namespace, name)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func formatPoolTime(value time.Time) string { return value.UTC().Format("2006-01-02 15:04:05") }
