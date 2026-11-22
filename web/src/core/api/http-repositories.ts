import { ApiClient, namespacePath } from './client'
import type {
  AgentRepository,
  AnalyticsRepository,
  AgentGraphRepository,
  KnowledgeRepository,
  LlmRepository,
  McpRepository,
  NotificationRepository,
  RuntimeSkillRepository,
  SkillRepository,
  GraphRunRepository,
  TraceRepository,
} from '../domain/repositories'
import type {
  Agent,
  AgentGraph,
  GraphRun,
  HumanApproval,
  KnowledgeEntry,
  LlmProvider,
  McpServer,
  NotificationChannel,
  NotificationDelivery,
  Page,
  GraphRunFilters,
  GraphRunMetrics,
  GraphRunTrace,
  SkillDefinition,
  NodeRun,
} from '../domain/types'

function queryString(input: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams()
  Object.entries(input).forEach(
    ([key, value]) => value !== undefined && value !== '' && params.set(key, String(value)),
  )
  const query = params.toString()
  return query ? `?${query}` : ''
}

function withoutAuditFields<T extends object>(
  value: T,
): Omit<T, 'created_at' | 'updated_at' | 'created_by' | 'updated_by'> {
  const payload = { ...value } as T & {
    created_at?: unknown
    updated_at?: unknown
    created_by?: unknown
    updated_by?: unknown
  }
  delete payload.created_at
  delete payload.updated_at
  delete payload.created_by
  delete payload.updated_by
  return payload
}

export class HttpAgentGraphRepository implements AgentGraphRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, signal?: AbortSignal): Promise<AgentGraph[]> {
    return this.api.request<AgentGraph[]>(namespacePath(namespace, 'agent-graphs'), {
      signal,
    })
  }

  get(namespace: string, id: string, signal?: AbortSignal) {
    return this.api.request<AgentGraph>(
      namespacePath(namespace, `agent-graphs/${encodeURIComponent(id)}`),
      {
        signal,
      },
    )
  }

  save(namespace: string, graph: AgentGraph, isNew: boolean) {
    const path = namespacePath(
      namespace,
      isNew ? 'agent-graphs' : `agent-graphs/${encodeURIComponent(graph.name)}`,
    )
    return this.api.request<AgentGraph>(path, {
      method: isNew ? 'POST' : 'PUT',
      body: JSON.stringify(withoutAuditFields(graph)),
    })
  }

  remove(namespace: string, id: string) {
    return this.api.request<void>(
      namespacePath(namespace, `agent-graphs/${encodeURIComponent(id)}`),
      {
        method: 'DELETE',
      },
    )
  }

  async trigger(namespace: string, id: string, vars: Record<string, unknown>) {
    const result = await this.api.request<{ graph_run_id: string }>(
      namespacePath(namespace, `agent-graphs/${encodeURIComponent(id)}/trigger`),
      {
        method: 'POST',
        body: JSON.stringify({
          input: vars,
          trigger: { type: 'manual', source: 'ui', payload: {} },
        }),
      },
    )
    return result.graph_run_id
  }
}

export class HttpRuntimeSkillRepository implements RuntimeSkillRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, signal?: AbortSignal): Promise<AgentGraph[]> {
    return this.api.request<AgentGraph[]>(namespacePath(namespace, 'skills'), {
      signal,
    })
  }

  save(namespace: string, skill: AgentGraph, isNew: boolean) {
    return this.api.request<AgentGraph>(
      namespacePath(namespace, isNew ? 'skills' : `skills/${encodeURIComponent(skill.id)}`),
      {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify(withoutAuditFields({ ...skill, kind: 'skill' })),
      },
    )
  }

  remove(namespace: string, id: string) {
    return this.api.request<void>(namespacePath(namespace, `skills/${encodeURIComponent(id)}`), {
      method: 'DELETE',
    })
  }
}

export class HttpGraphRunRepository implements GraphRunRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, filters: GraphRunFilters = {}, signal?: AbortSignal) {
    const query = queryString({
      page: filters.page ?? 1,
      size: filters.size ?? 100,
      status: filters.status,
      runtime_mode: filters.runtimeMode,
      agent_graph_id: filters.graphId,
    })
    const resource = `graph-runs${query}`
    return this.api.request<Page<GraphRun>>(namespacePath(namespace, resource), { signal })
  }
  get(namespace: string, id: string, graphId?: string, signal?: AbortSignal) {
    return this.api.request<GraphRun>(namespacePath(namespace, runResource(id, graphId)), {
      signal,
    })
  }
  nodeRuns(namespace: string, id: string, graphId?: string, signal?: AbortSignal) {
    return this.api.request<NodeRun[]>(
      namespacePath(namespace, `${runResource(id, graphId)}/node-runs`),
      { signal },
    )
  }
  approvals(namespace: string, id: string, graphId?: string, signal?: AbortSignal) {
    return this.api.request<HumanApproval[]>(
      namespacePath(namespace, `${runResource(id, graphId)}/approvals`),
      { signal },
    )
  }
  async resolveApproval(
    namespace: string,
    graphRunId: string,
    approvalId: string,
    decision: 'approve' | 'reject',
    graphId?: string,
  ) {
    await this.api.request<{ status: string }>(
      namespacePath(
        namespace,
        `${runResource(graphRunId, graphId)}/approvals/${encodeURIComponent(approvalId)}/${decision}`,
      ),
      { method: 'POST', body: JSON.stringify({}) },
    )
  }
  cancel(namespace: string, id: string, graphId?: string) {
    return this.api.request<void>(namespacePath(namespace, `${runResource(id, graphId)}/cancel`), {
      method: 'POST',
    })
  }
  remove(namespace: string, id: string, graphId?: string) {
    return this.api.request<void>(namespacePath(namespace, runResource(id, graphId)), {
      method: 'DELETE',
    })
  }
}

export class HttpTraceRepository implements TraceRepository {
  constructor(private readonly api: ApiClient) {}

  getGraphRunTrace(namespace: string, graphRunId: string, graphId?: string, signal?: AbortSignal) {
    return this.api.request<GraphRunTrace>(
      namespacePath(namespace, `${runResource(graphRunId, graphId)}/trace`),
      { signal },
    )
  }
}

function runResource(graphRunId: string, graphId?: string): string {
  const run = encodeURIComponent(graphRunId)
  void graphId
  return `graph-runs/${run}`
}

export class HttpAnalyticsRepository implements AnalyticsRepository {
  constructor(private readonly api: ApiClient) {}
  metrics(namespace: string, hours: number, signal?: AbortSignal) {
    const buckets = hours <= 24 ? Math.min(hours, 12) : 14
    return this.api.request<GraphRunMetrics>(
      namespacePath(namespace, `graph-runs/metrics${queryString({ hours, buckets })}`),
      { signal },
    )
  }
}

export class HttpKnowledgeRepository implements KnowledgeRepository {
  constructor(private readonly api: ApiClient) {}
  async list(namespace: string, scope: 'namespace' | 'graph' | 'run', signal?: AbortSignal) {
    const response = await this.api.request<Page<KnowledgeEntry>>(
      namespacePath(namespace, `knowledge${queryString({ scope, page: 1, size: 500 })}`),
      { signal },
    )
    return response.items
  }
}

export class HttpAgentRepository implements AgentRepository {
  constructor(private readonly api: ApiClient) {}
  async list(namespace: string, signal?: AbortSignal) {
    const response = await this.api.request<Page<Agent>>(namespacePath(namespace, 'agents'), {
      signal,
    })
    return response.items
  }
  save(namespace: string, agent: Agent, isNew: boolean, originalName = agent.name) {
    return this.api.request<Agent>(
      namespacePath(namespace, isNew ? 'agents' : `agents/${encodeURIComponent(originalName)}`),
      { method: isNew ? 'POST' : 'PUT', body: JSON.stringify(withoutAuditFields(agent)) },
    )
  }
  remove(namespace: string, name: string) {
    return this.api.request<void>(namespacePath(namespace, `agents/${encodeURIComponent(name)}`), {
      method: 'DELETE',
    })
  }
}

export class HttpMcpRepository implements McpRepository {
  constructor(private readonly api: ApiClient) {}
  async list(namespace: string, signal?: AbortSignal): Promise<McpServer[]> {
    return this.api.request<McpServer[]>(namespacePath(namespace, 'mcp'), { signal })
  }
  async save(
    namespace: string,
    item: McpServer,
    isNew: boolean,
    originalName = item.name,
  ): Promise<McpServer> {
    return this.api.request<McpServer>(
      namespacePath(namespace, isNew ? 'mcp' : `mcp/${encodeURIComponent(originalName)}`),
      {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify({
          ...withoutAuditFields(item),
          transport: 'http',
          header_refs: item.header_refs ?? {},
          env_refs: item.env_refs ?? {},
        }),
      },
    )
  }
  async remove(namespace: string, name: string): Promise<void> {
    await this.api.request<void>(namespacePath(namespace, `mcp/${encodeURIComponent(name)}`), {
      method: 'DELETE',
    })
  }
}

export class HttpLlmRepository implements LlmRepository {
  constructor(private readonly api: ApiClient) {}
  async list(namespace: string, signal?: AbortSignal): Promise<LlmProvider[]> {
    return this.api.request<LlmProvider[]>(namespacePath(namespace, 'llm/providers'), { signal })
  }
  async save(namespace: string, item: LlmProvider, isNew: boolean): Promise<LlmProvider> {
    return this.api.request<LlmProvider>(
      namespacePath(
        namespace,
        isNew ? 'llm/providers' : `llm/providers/${encodeURIComponent(item.id)}`,
      ),
      {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify(withoutAuditFields(item)),
      },
    )
  }
  async remove(namespace: string, id: string): Promise<void> {
    await this.api.request<void>(
      namespacePath(namespace, `llm/providers/${encodeURIComponent(id)}`),
      { method: 'DELETE' },
    )
  }
}

export class HttpSkillRepository implements SkillRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, signal?: AbortSignal): Promise<SkillDefinition[]> {
    return this.api.request<SkillDefinition[]>(namespacePath(namespace, 'skill-definitions'), {
      signal,
    })
  }

  save(namespace: string, item: SkillDefinition, isNew: boolean, originalName = item.name) {
    const payload = {
      name: item.name,
      description: item.description,
      instruction: item.instruction,
      model: item.model,
      temperature: item.temperature,
      max_tokens: item.max_tokens,
      tools: item.tools ?? [],
    }
    return this.api.request<SkillDefinition>(
      namespacePath(
        namespace,
        isNew ? 'skill-definitions' : `skill-definitions/${encodeURIComponent(originalName)}`,
      ),
      { method: isNew ? 'POST' : 'PUT', body: JSON.stringify(payload) },
    )
  }

  upload(namespace: string, name: string, kind: 'assets' | 'scripts', file: File) {
    const body = new FormData()
    body.set('file', file)
    return this.api.request<SkillDefinition>(
      namespacePath(namespace, `skill-definitions/${encodeURIComponent(name)}/${kind}`),
      { method: 'POST', body },
    )
  }

  remove(namespace: string, name: string) {
    return this.api.request<void>(
      namespacePath(namespace, `skill-definitions/${encodeURIComponent(name)}`),
      { method: 'DELETE' },
    )
  }
}

export class HttpNotificationRepository implements NotificationRepository {
  constructor(private readonly api: ApiClient) {}

  async list(namespace: string, signal?: AbortSignal): Promise<NotificationChannel[]> {
    const response = await this.api.request<Page<NotificationChannel>>(
      namespacePath(namespace, 'notifications/channels'),
      { signal },
    )
    return response.items
  }

  save(namespace: string, item: NotificationChannel, isNew: boolean) {
    const payload: Partial<NotificationChannel> = {
      name: item.name,
      provider: item.provider,
      config: item.config,
      enabled: item.enabled,
      labels: item.labels,
      clear_secret_fields: item.clear_secret_fields,
    }
    return this.api.request<NotificationChannel>(
      namespacePath(
        namespace,
        isNew ? 'notifications/channels' : `notifications/channels/${encodeURIComponent(item.id)}`,
      ),
      {
        method: isNew ? 'POST' : 'PUT',
        body: JSON.stringify(payload),
      },
    )
  }

  remove(namespace: string, id: string) {
    return this.api.request<void>(
      namespacePath(namespace, `notifications/channels/${encodeURIComponent(id)}`),
      { method: 'DELETE' },
    )
  }

  test(namespace: string, id: string, message: string) {
    return this.api.request<NotificationDelivery>(namespacePath(namespace, 'notifications/test'), {
      method: 'POST',
      body: JSON.stringify({ channel_id: id, message }),
    })
  }
}
