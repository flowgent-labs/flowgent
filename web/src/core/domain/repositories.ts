import type {
  Agent,
  AgentGraph,
  GraphRun,
  HumanApproval,
  KnowledgeEntry,
  LlmProvider,
  McpServer,
  MemoryScope,
  Page,
  GraphRunFilters,
  GraphRunMetrics,
  GraphRunTrace,
  SkillDefinition,
  NodeRun,
  NotificationChannel,
  NotificationDelivery,
  RuntimeConfigView,
  RuntimeSecretUpdate,
} from './types'

export interface AgentGraphRepository {
  list(namespace: string, signal?: AbortSignal): Promise<AgentGraph[]>
  get(namespace: string, id: string, signal?: AbortSignal): Promise<AgentGraph>
  save(namespace: string, graph: AgentGraph, isNew: boolean): Promise<AgentGraph>
  remove(namespace: string, id: string): Promise<void>
  trigger(namespace: string, id: string, vars: Record<string, unknown>): Promise<string>
}

export interface GraphRunRepository {
  list(namespace: string, filters?: GraphRunFilters, signal?: AbortSignal): Promise<Page<GraphRun>>
  get(namespace: string, id: string, graphId?: string, signal?: AbortSignal): Promise<GraphRun>
  nodeRuns(
    namespace: string,
    id: string,
    graphId?: string,
    signal?: AbortSignal,
  ): Promise<NodeRun[]>
  approvals(
    namespace: string,
    id: string,
    graphId?: string,
    signal?: AbortSignal,
  ): Promise<HumanApproval[]>
  resolveApproval(
    namespace: string,
    graphRunId: string,
    approvalId: string,
    decision: 'approve' | 'reject',
    graphId?: string,
  ): Promise<void>
  cancel(namespace: string, id: string, graphId?: string): Promise<void>
  remove(namespace: string, id: string, graphId?: string): Promise<void>
}

export interface TraceRepository {
  getGraphRunTrace(
    namespace: string,
    graphRunId: string,
    graphId?: string,
    signal?: AbortSignal,
  ): Promise<GraphRunTrace>
}

export interface AnalyticsRepository {
  metrics(namespace: string, hours: number, signal?: AbortSignal): Promise<GraphRunMetrics>
}

export interface KnowledgeRepository {
  list(namespace: string, scope: MemoryScope, signal?: AbortSignal): Promise<KnowledgeEntry[]>
}

export interface AgentRepository {
  list(namespace: string, signal?: AbortSignal): Promise<Agent[]>
  save(namespace: string, agent: Agent, isNew: boolean, originalName?: string): Promise<Agent>
  remove(namespace: string, name: string): Promise<void>
}

export interface McpRepository {
  list(namespace: string, signal?: AbortSignal): Promise<McpServer[]>
  save(
    namespace: string,
    item: McpServer,
    isNew: boolean,
    originalName?: string,
  ): Promise<McpServer>
  remove(namespace: string, name: string): Promise<void>
}

export interface LlmRepository {
  list(namespace: string, signal?: AbortSignal): Promise<LlmProvider[]>
  save(namespace: string, item: LlmProvider, isNew: boolean): Promise<LlmProvider>
  remove(namespace: string, id: string): Promise<void>
}

export interface NotificationRepository {
  list(namespace: string, signal?: AbortSignal): Promise<NotificationChannel[]>
  save(namespace: string, item: NotificationChannel, isNew: boolean): Promise<NotificationChannel>
  remove(namespace: string, id: string): Promise<void>
  test(namespace: string, id: string, message: string): Promise<NotificationDelivery>
}

export interface RuntimeSkillRepository {
  list(namespace: string, signal?: AbortSignal): Promise<AgentGraph[]>
  save(namespace: string, skill: AgentGraph, isNew: boolean): Promise<AgentGraph>
  remove(namespace: string, id: string): Promise<void>
}

export interface SkillRepository {
  list(namespace: string, signal?: AbortSignal): Promise<SkillDefinition[]>
  save(
    namespace: string,
    item: SkillDefinition,
    isNew: boolean,
    originalName?: string,
  ): Promise<SkillDefinition>
  upload(
    namespace: string,
    name: string,
    kind: 'assets' | 'scripts',
    file: File,
  ): Promise<SkillDefinition>
  remove(namespace: string, name: string): Promise<void>
}

export interface RuntimeConfigRepository {
  getNamespace(namespace: string, signal?: AbortSignal): Promise<RuntimeConfigView>
  updateNamespaceEnvironment(
    namespace: string,
    environment: Record<string, string>,
  ): Promise<RuntimeConfigView>
  updateNamespaceSecrets(namespace: string, update: RuntimeSecretUpdate): Promise<RuntimeConfigView>
  getAgentGraph(
    namespace: string,
    graphId: string,
    signal?: AbortSignal,
  ): Promise<RuntimeConfigView>
  updateAgentGraphEnvironment(
    namespace: string,
    graphId: string,
    environment: Record<string, string>,
  ): Promise<RuntimeConfigView>
  updateAgentGraphSecrets(
    namespace: string,
    graphId: string,
    update: RuntimeSecretUpdate,
  ): Promise<RuntimeConfigView>
}

export interface Repositories {
  graphs: AgentGraphRepository
  graphRuns: GraphRunRepository
  traces: TraceRepository
  analytics: AnalyticsRepository
  knowledge: KnowledgeRepository
  agents: AgentRepository
  mcps: McpRepository
  llms: LlmRepository
  notifications: NotificationRepository
  runtimeSkills: RuntimeSkillRepository
  skills: SkillRepository
  runtimeConfig: RuntimeConfigRepository
}
