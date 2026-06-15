package agentflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// LoadFromDB reads agentflow definitions from the database (Standard mode).
func LoadFromDB(ctx context.Context, s store.IStore) ([]entities.AgentFlowInfo, map[string]entities.AgentFlowInfo, error) {
	var flows []entities.AgentFlowInfo
	subFlows := make(map[string]entities.AgentFlowInfo)

	var afStore IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = NewAgentFlowPostgresStore(db)
	case *sql.DB:
		afStore = NewAgentFlowSQLiteStore(db)
	}

	page, err := afStore.Select(ctx, entities.PageRequest{Page: 1, Size: 1000})
	versions := page.Items
	if err != nil {
		return flows, subFlows, fmt.Errorf("list agentflow definitions: %w", err)
	}

	seen := make(map[string]bool)
	for _, v := range versions {
		if seen[v.AgentFlowID] {
			continue
		}
		seen[v.AgentFlowID] = true

		var spec entities.AgentFlowInfo
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			slog.Warn("Skipping invalid agentflow definition", "agentflow_id", v.AgentFlowID, "error", err)
			continue
		}
		if spec.ID == "" {
			slog.Warn("Skipping agentflow definition with empty ID", "agentflow_id", v.AgentFlowID)
			continue
		}
		if spec.Kind == "skill" {
			subFlows[spec.ID] = spec
		} else {
			flows = append(flows, spec)
		}
	}

	return flows, subFlows, nil
}
