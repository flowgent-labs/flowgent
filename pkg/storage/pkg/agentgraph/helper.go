package agentgraph

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

// LoadFromDB reads graph definitions from the database.
func LoadFromDB(ctx context.Context, s storage.IStorage, namespace string) ([]entities.AgentGraph, map[string]entities.AgentGraph, error) {
	if namespace == "" {
		namespace = defaultNamespace
	}
	var graphs []entities.AgentGraph
	subAgentGraphs := make(map[string]entities.AgentGraph)

	var afStore IAgentGraphStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = NewAgentGraphPostgresStore(db)
	case *sql.DB:
		afStore = NewAgentGraphSQLiteStore(db)
	}

	page, err := afStore.Select(ctx, namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		return graphs, subAgentGraphs, fmt.Errorf("list graph definitions: %w", err)
	}
	versions := page.Items

	seen := make(map[string]bool)
	for _, v := range versions {
		if seen[v.AgentGraphID] {
			continue
		}
		seen[v.AgentGraphID] = true

		var spec entities.AgentGraph
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			slog.Warn("Skipping invalid graph definition", "agent_graph_id", v.AgentGraphID, "error", err)
			continue
		}
		spec.ID = v.AgentGraphID
		spec.Name = v.AgentGraphName
		if spec.ResourceName() == "" {
			slog.Warn("Skipping graph definition with empty ID", "agent_graph_id", v.AgentGraphID)
			continue
		}
		if spec.Kind == "skill" {
			subAgentGraphs[spec.ResourceName()] = spec
		} else {
			graphs = append(graphs, spec)
		}
	}

	return graphs, subAgentGraphs, nil
}

const defaultNamespace = "default"
