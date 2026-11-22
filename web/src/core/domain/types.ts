export interface BaseEntity {
  id: string
  description?: string
  namespace_id?: string
  status: string
  created_at: string
  created_by?: string
  updated_at: string
  updated_by?: string
  row_version?: number
  metadata?: Record<string, unknown>
}

export interface Page<T> {
  items: T[]
  total_count: number
  request?: { page: number; size: number }
  total_pages?: number
}

export interface RuntimeConfigLayer {
  environment: Record<string, string>
  secret_keys: string[]
}

export interface RuntimeConfigView {
  local: RuntimeConfigLayer
  inherited: RuntimeConfigLayer
  effective: RuntimeConfigLayer
}

export interface RuntimeSecretUpdate {
  secrets?: Record<string, string>
  clear_secret_keys?: string[]
}

export const nodeKinds = [
  'agent',
  'tool',
  'skill',
  'map',
  'subgraph',
  'condition',
  'committee',
  'human',
  'supervisor',
  'sandbox',
  'join',
  'noop',
] as const

export type NodeKind = (typeof nodeKinds)[number]

export interface GraphNode {
  id: string
  kind: NodeKind
  solution?: string
  agent?: string
  skill?: string
  tool?: string
  source?: string
  expression?: string
  instruction?: string
  strategy?: Record<string, unknown>
  args?: Record<string, unknown>
  retry?: { max: number; initial?: string; max_delay?: string; factor?: number }
  node?: GraphNode
  concurrency?: number
  approval?: { timeout?: string; on_approve?: string; on_reject?: string }
  supervisor_config?: {
    max_retries?: number
    max_nodes?: number
    max_injections?: number
    allowed_actions?: string[]
  }
  subgraph?: SubGraphConfig
  output_schema?: Record<string, unknown>
  runtime?: string
  script?: string
  timeout?: string
  resources?: Record<string, unknown>
  network_policy?: Record<string, unknown>
  workspace?: string
}

export interface GraphEdge {
  from: string
  to: string
  condition?: boolean
}

export type RuntimeMode = 'application' | 'session'

export interface RuntimeResources {
  jobmanager?: { cpu?: string; memory?: string }
  taskmanager?: { cpu?: string; memory?: string }
  sandbox?: { cpu?: string; memory?: string }
}

export interface AgentGraph extends BaseEntity {
  name: string
  revision: number
  summarize_enabled: boolean
  kind: string
  summary?: string
  input_schema?: Record<string, unknown>
  output_schema?: Record<string, unknown>
  vars?: Record<string, unknown>
  nodes: GraphNode[]
  edges: GraphEdge[]
  triggers?: Array<{ type: string; cron?: string; provider?: string; events?: string[] }>
  sandbox_policy?: Record<string, unknown>
  runtime_mode: RuntimeMode
  resources?: RuntimeResources
  k8s_namespace?: string
  labels?: Record<string, string>
}

export type GraphRunStatus = 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'PAUSED' | 'CANCELLED'
export type NodeRunStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'SUCCESS'
  | 'FAILED'
  | 'WAITING_HUMAN'
  | 'WAITING'
  | 'SKIPPED'
  | 'RETRYING'

export interface GraphRun extends BaseEntity {
  snapshot: AgentGraph
  node_runs?: NodeRun[]
  subgraphs?: Record<string, SubGraph>
  state_version?: number
  last_event_seq?: number
  agent_graph_id: string
  agent_graph_name: string
  agent_graph_revision_id: string
  agent_graph_revision: number
  status: GraphRunStatus
  input: Record<string, unknown>
  output: Record<string, unknown>
  error: string
  run_instruction?: string
  summarize_enabled: boolean
  context_snapshot: Record<string, unknown>
  trigger_type?: string
  trigger_source?: string
  trigger_payload?: Record<string, unknown>
  started_at?: string | null
  finished_at?: string | null
  namespace?: string
  labels?: Record<string, string>
  runtime_mode: RuntimeMode
}

export interface NodeRun extends BaseEntity {
  graph_node_id?: string
  subgraph_instance_id?: string
  graph_revision?: number
  dispatch_epoch?: number
  workspace_snapshot?: WorkspaceSnapshot
  graph_run_id: string
  node_instance_id: string
  attempt: number
  agent_revision_id?: string
  status: NodeRunStatus
  input: Record<string, unknown>
  output: Record<string, unknown>
  error: string
  max_retries: number
  execution_id: string
  parent_node_run_id?: string
  sequence: number
  execution_memory?: Record<string, unknown>
  checkpoint?: Record<string, unknown>
  workspace_version?: string
  lease_owner?: string
  lease_expires_at?: string | null
  fencing_token: number
  last_heartbeat_at?: string | null
  started_at?: string | null
  finished_at?: string | null
}

export interface HumanApproval extends BaseEntity {
  graph_run_id?: string
  node_run_id?: string
  type: 'human_gate' | 'tool_call' | 'payment' | 'knowledge_publish' | 'instruction_publish'
  subject_type: string
  subject_id: string
  request: Record<string, unknown>
  request_hash: string
  status: 'pending' | 'approved' | 'rejected' | 'expired' | 'cancelled'
  decision?: Record<string, unknown>
  decided_by?: string
  decided_at?: string | null
  expires_at?: string | null
  consumed_at?: string | null
  idempotency_key: string
}

export interface TraceEvent {
  timestamp: string
  name?: string
  attributes?: Record<string, unknown>
}

export interface TraceSpan {
  trace_id: string
  span_id: string
  parent_span_id?: string
  operation_name: string
  service_name: string
  start_time: string
  duration_micros: number
  status: 'OK' | 'ERROR' | 'UNSET' | string
  kind?: string
  attributes: Record<string, unknown>
  events?: TraceEvent[]
  warnings?: string[]
}

export interface TraceInfo {
  trace_id: string
  root_span_id?: string
  start_time: string
  duration_micros: number
  services: string[]
  spans: TraceSpan[]
}

export interface GraphRunTrace {
  graph_run_id: string
  source: string
  fetched_at: string
  traces: TraceInfo[]
}

export interface Agent extends BaseEntity {
  name: string
  model: string
  soul: string
  instruction: string
  input_schema?: Record<string, unknown>
  output_schema?: Record<string, unknown>
  revision: number
  temperature?: number
  max_tokens?: number
  labels?: Record<string, string>
}

export interface KnowledgeEntry extends BaseEntity {
  title: string
  content: string
  content_type: string
  source: string
  source_ref: string
  tags: string[]
  metadata: Record<string, unknown>
}

export type MemoryScope = 'namespace' | 'graph' | 'run'

export interface McpServer extends BaseEntity {
  name: string
  enabled: boolean
  transport: 'http'
  rpc_url: string
  header_refs?: Record<string, string>
  command?: string[]
  args?: string[]
  env_refs?: Record<string, string>
  labels?: Record<string, string>
}

export interface LlmModel {
  name: string
  temperature: number
  topk: number
  modalities?: { input: string[]; output: string[] }
  thinking?: { type: string; budget_tokens: number }
}

export interface LlmProvider extends BaseEntity {
  name: string
  type: 'openai' | 'anthropic' | 'gemini'
  enabled: boolean
  timeout?: string
  timeout_ms?: number
  base_uri: string
  proxy?: string
  rate_limit?: number
  default_model: string
  models: LlmModel[]
  api_key_env?: string
  key_configured?: boolean
  env_refs?: Record<string, string>
  labels?: Record<string, string>
}

export interface SkillFile {
  id: string
  kind: 'asset' | 'script'
  relative_path: string
  media_type: string
  size_bytes: number
  content_hash: string
  created_at: string
  created_by?: string
  metadata?: Record<string, unknown>
}

export interface SkillDefinition extends BaseEntity {
  name: string
  instruction: string
  model?: string
  temperature?: number
  max_tokens?: number
  tools?: string[]
  revision: number
  assets: SkillFile[]
  scripts: SkillFile[]
}

export type NotificationProvider = 'telegram' | 'dingtalk' | 'slack' | 'email' | 'webhook'

export interface NotificationChannel extends BaseEntity {
  name: string
  provider: NotificationProvider
  config: Record<string, unknown>
  enabled: boolean
  labels?: Record<string, string>
  configured_secret_fields?: string[]
  clear_secret_fields?: string[]
}

export interface NotificationDelivery {
  delivery_id: string
  result_topic: string
}

export interface GraphRunFilters {
  page?: number
  size?: number
  status?: GraphRunStatus
  runtimeMode?: RuntimeMode
  graphId?: string
  search?: string
}

export interface GraphRunMetrics {
  total: number
  running: number
  completed: number
  failed: number
  cancelled: number
  success_rate: number
  failure_rate: number
  average_duration_ms: number
  buckets: Array<{
    start_time: string
    end_time: string
    running: number
    completed: number
    failed: number
    average_duration_ms: number
  }>
}

export interface SubGraphConfig {
  ref: string
  mode?: 'static' | 'adaptive'
  supervisor?: string
  mutation?: { allow: string[] }
  limits?: {
    max_created_nodes?: number
    max_parallelism?: number
    max_patches?: number
    max_depth?: number
    max_active_duration?: string
  }
}

export interface WorkspaceSnapshot {
  id: string
  ref: string
  sha256: string
  base_commit?: string
  created_at: string
}
export interface SubGraph {
  instance_id: string
  revision: number
  config: SubGraphConfig
  graph: AgentGraph
  sealed: boolean
  created_nodes: number
  patches: number
}
export interface GraphPatch {
  id: string
  graph_run_id: string
  subgraph_instance_id: string
  expected_revision: number
  operations: Array<{
    kind: string
    node_id?: string
    node?: GraphNode
    from?: string
    to?: string
    remove?: boolean
  }>
}
export interface GraphCheckpoint {
  id: string
  graph_run_id: string
  event_seq: number
  state_version: number
  snapshot: AgentGraph
  subgraphs?: Record<string, SubGraph>
  node_runs: NodeRun[]
  sha256: string
  created_at: string
}
