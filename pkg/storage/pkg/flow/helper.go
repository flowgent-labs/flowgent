package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

// LoadFromDB reads flow definitions from the database.
func LoadFromDB(ctx context.Context, s storage.IStorage, namespace string) ([]entities.FlowInfo, map[string]entities.FlowInfo, error) {
	if namespace == "" {
		namespace = defaultNamespace
	}
	var flows []entities.FlowInfo
	subFlows := make(map[string]entities.FlowInfo)

	var afStore IFlowInfoStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = NewFlowPostgresStore(db)
	case *sql.DB:
		afStore = NewFlowSQLiteStore(db)
	}

	page, err := afStore.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return flows, subFlows, fmt.Errorf("list flow definitions: %w", err)
	}
	versions := page.Items

	seen := make(map[string]bool)
	for _, v := range versions {
		if seen[v.FlowID] {
			continue
		}
		seen[v.FlowID] = true

		var spec entities.FlowInfo
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			slog.Warn("Skipping invalid flow definition", "flow_id", v.FlowID, "error", err)
			continue
		}
		spec.ID = v.FlowID
		spec.Name = v.FlowName
		if spec.ResourceName() == "" {
			slog.Warn("Skipping flow definition with empty ID", "flow_id", v.FlowID)
			continue
		}
		if spec.Kind == "skill" {
			subFlows[spec.ResourceName()] = spec
		} else {
			flows = append(flows, spec)
		}
	}

	return flows, subFlows, nil
}

const defaultNamespace = "default"
