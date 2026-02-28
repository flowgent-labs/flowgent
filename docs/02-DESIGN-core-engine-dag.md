# AI Agents Orchestration Engine Design (Layer 1)

## 1. System Overview
The system is a universal **Orchestration Engine** for multi-agent collaboration. It decouples business logic from AI Agent execution, utilizing a **Direct Acyclic Graph (DAG)** topology for orchestration nodes. Crucially, it integrates a **Harness** environment for safety, state persistence, and self-policing.

## 2. Architecture & Directory Structure

```text
src/internal/adk/
  ├── orchestration/    # Core DAG engine, Loader, YAML parser
  ├── memory/           # Unified Memory & Vector Storage Interface (SQLite/Postgres)
  ├── harness/          # Sandboxes, Time-travel, Checkpoints
  ├── a2a/              # Agent-to-Agent Server for autonomy
  ├── tools/            # Standardized tool registry and interfaces
  ├── polling/          # OTEL/TraceID integration
  └── agents/           # Standard agent base classes
```

## 3. Core Components (The Engine)

### 3.1 DAG Orchestration Engine (`orchestration/engine.go`)
The engine interprets the workflow as a directed acyclic graph (DAG) composed of two main concepts:
- **Nodes (`nodes`)**: Definitions of atomic execution units (Agents, MCP tools, Condition gates).
- **Connections (`connections`)**: Directed edges defining the topology (Fan-in, Fan-out, Merging).
The engine resolves dependencies dynamically via Topological Sort, ensuring parallel execution of unconnected branches and merging results for downstream nodes.

### 3.2 Memory & State Persistence (`adk/memory`)
Agents must retain context across long-running tasks or system restarts.
- **Unified Interface**:
  ```go
  type MemoryStore interface {
      SaveState(ctx context.Context, orchestrationID string, state *OrchestrationState) error
      LoadState(ctx context.Context, orchestrationID string) (*OrchestrationState, error)
      VectorSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
  }
  ```
- **Implementations**:
  1. **Default (Embedded)**: **SQLite + `sqlite-vec`**. Zero dependencies, local file-based vector storage.
  2. **Production**: **PostgreSQL + `pgvector`**. Scalable, distributed vector search.
- **Compression Strategy**: Before persisting `HandoffData`, the engine passes it through a **Context Compressor Agent** to strip redundant tokens, keeping only actionable data to prevent context explosion and save DB space.

### 3.3 Harness Architecture (`adk/harness`)
- **Sandbox Harness**: Agents and MCP tools run in isolated OCI/Docker containers with restricted network egress/CPU.
- **Time Travel Harness**: Administrators can "rewind" to a specific checkpoint, modify state, and "replay" the graph.
- **Cost Governance**: Enforces strict limits on maximum LLM tokens and API costs per orchestration run.

## 4. Generic Orchestration Example: ETL (Extract, Transform, Load)

This demonstrates how a standard, non-AI ETL pipeline is represented in our Agent-based DAG. It shows Fan-out and Fan-in capabilities.

```yaml
# 1. NODE DEFINITIONS (Atomic Units)
nodes:
  # Extraction Layer
  extract_api_data:
    type: tool
    tool_ref: http_fetch
    params: { url: "https://api.erp.com/inventory" }
  
  # Transformation Layer (Parallel Processing)
  transform_clean:
    type: agent
    agent_ref: data_cleanser
    params: { instructions: "Remove duplicates, normalize timestamps" }
    
  transform_enrich:
    type: agent
    agent_ref: geo_enricher
    params: { tools: ["geo_ip_mcp"] }

  # Loading Layer
  load_warehouse:
    type: tool
    tool_ref: postgres_bulk_upsert

  load_archive:
    type: tool
    tool_ref: s3_object_append

# 2. CONNECTIONS (Flow Topology)
connections:
  # Extract -> Parallel Transform
  - from: extract_api_data
    to: [transform_clean, transform_enrich]
    
  # Parallel Transform -> Fan-in for Loading
  - from: transform_clean
    to: load_warehouse
  - from: transform_enrich
    to: load_warehouse
    to: load_archive # Enriched data goes to both
```

## 5. Configuration Root (A2A Autonomy)

CyberBot exposes an A2A (Agent-to-Agent) server allowing other AI systems to invoke these orchestrations remotely.

```yaml
autonomy:
  enabled: true
  server_port: 8080
  allowed_callers: ["project-manager-agent", "dev-bot"] 
```