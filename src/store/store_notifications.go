package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Notification Channels ──────────────────────────────────

func (s *PostgresStore) SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error {
	if ch.ID == "" {
		ch.ID = uuid.New().String()
	}
	ch.UpdatedAt = time.Now()
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = ch.UpdatedAt
	}
	if ch.TenantID == "" {
		ch.TenantID = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_channels (id, name, channel_type, config, enabled, tenant_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (id) DO UPDATE SET name=$2, channel_type=$3, config=$4, enabled=$5, tenant_id=$6, updated_at=$8`,
		ch.ID, ch.Name, string(ch.Type), toJSON(ch.Config), ch.Enabled, ch.TenantID, ch.CreatedAt, ch.UpdatedAt)
	return err
}

func (s *PostgresStore) GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
		 FROM notification_channels WHERE id=$1`, id)
	return scanNotifierChannel(row)
}

func (s *PostgresStore) ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
			 FROM notification_channels ORDER BY name`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
			 FROM notification_channels WHERE tenant_id=$1 ORDER BY name`, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []model.NotifierChannel
	for rows.Next() {
		ch, err := scanNotifierChannelRow(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *ch)
	}
	return channels, rows.Err()
}

func (s *PostgresStore) DeleteNotifierChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=$1`, id)
	return err
}

// ─── Subscription Routes (clustered WS delivery) ────────────

func (s *PostgresStore) SaveSubscriptionRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	if route.ID == "" {
		route.ID = uuid.New().String()
	}
	route.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscription_routes (id, agentflow_id, ws_id, pod_id, created_at)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (id) DO UPDATE SET agentflow_id=$2, ws_id=$3, pod_id=$4`,
		route.ID, route.AgentFlowID, route.WSID, route.PodID, route.CreatedAt)
	return err
}

func (s *PostgresStore) GetSubscriptionRoutesByAgentFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agentflow_id, ws_id, pod_id, created_at
		 FROM subscription_routes WHERE agentflow_id=$1`, agentFlowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []model.SubscriptionRoute
	for rows.Next() {
		r, err := scanSubscriptionRouteRow(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, *r)
	}
	return routes, rows.Err()
}

func (s *PostgresStore) DeleteSubscriptionRoute(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscription_routes WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) DeleteSubscriptionRoutesByPod(ctx context.Context, podID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscription_routes WHERE pod_id=$1`, podID)
	return err
}

// CleanupOrphanedRoutes removes subscription routes older than maxAge that belong
// to pods that are no longer alive. Called periodically by the notification service.
func (s *PostgresStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM subscription_routes WHERE pod_id=$1 AND created_at < $2`, podID, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// ─── Scanner helpers ────────────────────────────────────────

func scanNotifierChannel(s scanner) (*model.NotifierChannel, error) {
	var ch model.NotifierChannel
	var cfgB []byte
	var chType string
	if err := s.Scan(&ch.ID, &ch.Name, &chType, &cfgB, &ch.Enabled, &ch.TenantID, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
		return nil, err
	}
	ch.Type = model.NotifierChannelType(chType)
	if cfgB != nil {
		json.Unmarshal(cfgB, &ch.Config)
	}
	return &ch, nil
}

func scanNotifierChannelRow(r rowsScanner) (*model.NotifierChannel, error) {
	return scanNotifierChannel(r)
}

func scanSubscriptionRoute(s scanner) (*model.SubscriptionRoute, error) {
	var r model.SubscriptionRoute
	if err := s.Scan(&r.ID, &r.AgentFlowID, &r.WSID, &r.PodID, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func scanSubscriptionRouteRow(r rowsScanner) (*model.SubscriptionRoute, error) {
	return scanSubscriptionRoute(r)
}

// ─── SQLite implementations ─────────────────────────────────

func (s *SQLiteStore) SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error {
	if ch.ID == "" {
		ch.ID = uuid.New().String()
	}
	ch.UpdatedAt = time.Now()
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = ch.UpdatedAt
	}
	if ch.TenantID == "" {
		ch.TenantID = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_channels (id, name, channel_type, config, enabled, tenant_id, created_at, updated_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8)
		 ON CONFLICT (id) DO UPDATE SET name=?2, channel_type=?3, config=?4, enabled=?5, tenant_id=?6, updated_at=?8`,
		ch.ID, ch.Name, string(ch.Type), toJSON(ch.Config), ch.Enabled, ch.TenantID, ch.CreatedAt, ch.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
		 FROM notification_channels WHERE id=?1`, id)
	return scanNotifierChannel(row)
}

func (s *SQLiteStore) ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
			 FROM notification_channels ORDER BY name`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, name, channel_type, config, enabled, tenant_id, created_at, updated_at
			 FROM notification_channels WHERE tenant_id=?1 ORDER BY name`, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []model.NotifierChannel
	for rows.Next() {
		ch, err := scanNotifierChannelRow(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *ch)
	}
	return channels, rows.Err()
}

func (s *SQLiteStore) DeleteNotifierChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?1`, id)
	return err
}

func (s *SQLiteStore) SaveSubscriptionRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	if route.ID == "" {
		route.ID = uuid.New().String()
	}
	route.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscription_routes (id, agentflow_id, ws_id, pod_id, created_at)
		 VALUES (?1,?2,?3,?4,?5)
		 ON CONFLICT (id) DO UPDATE SET agentflow_id=?2, ws_id=?3, pod_id=?4`,
		route.ID, route.AgentFlowID, route.WSID, route.PodID, route.CreatedAt)
	return err
}

func (s *SQLiteStore) GetSubscriptionRoutesByAgentFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agentflow_id, ws_id, pod_id, created_at
		 FROM subscription_routes WHERE agentflow_id=?1`, agentFlowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []model.SubscriptionRoute
	for rows.Next() {
		r, err := scanSubscriptionRouteRow(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, *r)
	}
	return routes, rows.Err()
}

func (s *SQLiteStore) DeleteSubscriptionRoute(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscription_routes WHERE id=?1`, id)
	return err
}

func (s *SQLiteStore) DeleteSubscriptionRoutesByPod(ctx context.Context, podID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscription_routes WHERE pod_id=?1`, podID)
	return err
}

func (s *SQLiteStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM subscription_routes WHERE pod_id=?1 AND created_at < ?2`, podID, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}
