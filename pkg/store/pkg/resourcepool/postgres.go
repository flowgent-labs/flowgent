package resourcepool

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct{ db *pgxpool.Pool }

const postgresPoolColumns = `id,name,replicas,slots_per_pod,resources,sandbox_replicas,
	sandbox_slots_per_pod,sandbox_resources,priority_class_name,node_selector,description,
	namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag`

func scanPostgresPool(row interface{ Scan(...any) error }) (*entities.ResourcePoolInfo, error) {
	var item entities.ResourcePoolInfo
	var resources, sandboxResources, nodeSelector []byte
	err := row.Scan(&item.ID, &item.Name, &item.Replicas, &item.SlotsPerPod, &resources,
		&item.SandboxReplicas, &item.SandboxSlotsPerPod, &sandboxResources,
		&item.PriorityClassName, &nodeSelector, &item.Description, &item.Namespace,
		&item.Status, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy, &item.DelFlag)
	if err != nil {
		return nil, normalizeError(err)
	}
	_ = json.Unmarshal(resources, &item.Resources)
	_ = json.Unmarshal(sandboxResources, &item.SandboxResources)
	_ = json.Unmarshal(nodeSelector, &item.NodeSelector)
	return &item, nil
}

func (s *postgresRepository) List(ctx context.Context, namespace string) ([]*entities.ResourcePoolInfo, error) {
	rows, err := s.db.Query(ctx, `SELECT `+postgresPoolColumns+` FROM orh_resource_pool
		WHERE namespace_id=$1 AND del_flag=false ORDER BY name`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.ResourcePoolInfo, 0)
	for rows.Next() {
		item, err := scanPostgresPool(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *postgresRepository) Get(ctx context.Context, namespace, name string) (*entities.ResourcePoolInfo, error) {
	return scanPostgresPool(s.db.QueryRow(ctx, `SELECT `+postgresPoolColumns+` FROM orh_resource_pool
		WHERE namespace_id=$1 AND LOWER(name)=LOWER($2) AND del_flag=false`, namespace, name))
}

func (s *postgresRepository) Create(ctx context.Context, item *entities.ResourcePoolInfo) error {
	entities.NormalizeResourcePool(item)
	item.ID = uuid.NewString()
	item.MarkCreated(item.CreatedBy)
	resources, _ := json.Marshal(item.Resources)
	sandboxResources, _ := json.Marshal(item.SandboxResources)
	nodeSelector, _ := json.Marshal(item.NodeSelector)
	_, err := s.db.Exec(ctx, `INSERT INTO orh_resource_pool (`+postgresPoolColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,false)`,
		item.ID, item.Name, item.Replicas, item.SlotsPerPod, resources, item.SandboxReplicas,
		item.SandboxSlotsPerPod, sandboxResources, item.PriorityClassName, nodeSelector,
		item.Description, item.Namespace, item.Status, item.CreatedAt, item.CreatedBy,
		item.UpdatedAt, item.UpdatedBy)
	if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
		return ErrAlreadyExists
	}
	return err
}

func (s *postgresRepository) Update(ctx context.Context, item *entities.ResourcePoolInfo) error {
	entities.NormalizeResourcePool(item)
	resources, _ := json.Marshal(item.Resources)
	sandboxResources, _ := json.Marshal(item.SandboxResources)
	nodeSelector, _ := json.Marshal(item.NodeSelector)
	item.UpdatedAt = time.Now().UTC()
	tag, err := s.db.Exec(ctx, `UPDATE orh_resource_pool SET replicas=$1,slots_per_pod=$2,resources=$3,
		sandbox_replicas=$4,sandbox_slots_per_pod=$5,sandbox_resources=$6,priority_class_name=$7,
		node_selector=$8,description=$9,updated_at=$10,updated_by=$11
		WHERE namespace_id=$12 AND LOWER(name)=LOWER($13) AND del_flag=false`,
		item.Replicas, item.SlotsPerPod, resources, item.SandboxReplicas, item.SandboxSlotsPerPod,
		sandboxResources, item.PriorityClassName, nodeSelector, item.Description, item.UpdatedAt,
		item.UpdatedBy, item.Namespace, item.Name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postgresRepository) Delete(ctx context.Context, namespace, name, actor string) error {
	tag, err := s.db.Exec(ctx, `UPDATE orh_resource_pool SET del_flag=true,status='DELETED',updated_at=NOW(),updated_by=$1
		WHERE namespace_id=$2 AND LOWER(name)=LOWER($3) AND del_flag=false`, actor, namespace, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
