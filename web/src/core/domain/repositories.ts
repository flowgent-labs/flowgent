import type {
  Agent,
  Flow,
  FlowRun,
  HumanApproval,
  KnowledgeEntry,
  LlmProvider,
  McpServer,
  MemoryScope,
  Page,
  RunFilters,
  RunMetrics,
  RunTrace,
  SkillDefinition,
  TaskRun,
  NotificationChannel,
  NotificationDelivery,
  RuntimeConfigView,
  RuntimeSecretUpdate,
} from './types'

export interface FlowRepository {
  list(namespace: string, signal?: AbortSignal): Promise<Flow[]>
  get(namespace: string, id: string, signal?: AbortSignal): Promise<Flow>
  save(namespace: string, flow: Flow, isNew: boolean): Promise<Flow>
  remove(namespace: string, id: string): Promise<void>
  trigger(namespace: string, id: string, vars: Record<string, unknown>): Promise<string>
}

export interface RunRepository {
  list(namespace: string, filters?: RunFilters, signal?: AbortSignal): Promise<Page<FlowRun>>
  get(namespace: string, id: string, flowId?: string, signal?: AbortSignal): Promise<FlowRun>
  tasks(namespace: string, id: string, flowId?: string, signal?: AbortSignal): Promise<TaskRun[]>
  approvals(
    namespace: string,
    id: string,
    flowId?: string,
    signal?: AbortSignal,
  ): Promise<HumanApproval[]>
  resolveApproval(
    namespace: string,
    runId: string,
    approvalId: string,
    decision: 'approve' | 'reject',
    flowId?: string,
  ): Promise<void>
  cancel(namespace: string, id: string, flowId?: string): Promise<void>
  remove(namespace: string, id: string, flowId?: string): Promise<void>
}

export interface TraceRepository {
  getRunTrace(
    namespace: string,
    runId: string,
    flowId?: string,
    signal?: AbortSignal,
  ): Promise<RunTrace>
}

export interface AnalyticsRepository {
  metrics(namespace: string, hours: number, signal?: AbortSignal): Promise<RunMetrics>
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
  list(namespace: string, signal?: AbortSignal): Promise<Flow[]>
  save(namespace: string, skill: Flow, isNew: boolean): Promise<Flow>
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
  getFlow(namespace: string, flowId: string, signal?: AbortSignal): Promise<RuntimeConfigView>
  updateFlowEnvironment(
    namespace: string,
    flowId: string,
    environment: Record<string, string>,
  ): Promise<RuntimeConfigView>
  updateFlowSecrets(
    namespace: string,
    flowId: string,
    update: RuntimeSecretUpdate,
  ): Promise<RuntimeConfigView>
}

export interface Repositories {
  flows: FlowRepository
  runs: RunRepository
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
