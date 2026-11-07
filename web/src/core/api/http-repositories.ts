import { ApiClient, namespacePath } from './client'
import type {
  AgentRepository,
  AnalyticsRepository,
  FlowRepository,
  KnowledgeRepository,
  LlmRepository,
  McpRepository,
  NotificationRepository,
  RuntimeSkillRepository,
  SkillRepository,
  RunRepository,
  TraceRepository,
} from '../domain/repositories'
import type {
  Agent,
  Flow,
  FlowRun,
  HumanApproval,
  KnowledgeEntry,
  LlmProvider,
  McpServer,
  NotificationChannel,
  NotificationDelivery,
  Page,
  RunFilters,
  RunMetrics,
  RunTrace,
  SkillDefinition,
  TaskRun,
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

export class HttpFlowRepository implements FlowRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, signal?: AbortSignal): Promise<Flow[]> {
    return this.api.request<Flow[]>(namespacePath(namespace, 'flows'), {
      signal,
    })
  }

  get(namespace: string, id: string, signal?: AbortSignal) {
    return this.api.request<Flow>(namespacePath(namespace, `flows/${encodeURIComponent(id)}`), {
      signal,
    })
  }

  save(namespace: string, flow: Flow, isNew: boolean) {
    const path = namespacePath(
      namespace,
      isNew ? 'flows' : `flows/${encodeURIComponent(flow.name)}`,
    )
    return this.api.request<Flow>(path, {
      method: isNew ? 'POST' : 'PUT',
      body: JSON.stringify(withoutAuditFields(flow)),
    })
  }

  remove(namespace: string, id: string) {
    return this.api.request<void>(namespacePath(namespace, `flows/${encodeURIComponent(id)}`), {
      method: 'DELETE',
    })
  }

  async trigger(namespace: string, id: string, vars: Record<string, unknown>) {
    const result = await this.api.request<{ run_id: string }>(
      namespacePath(namespace, `flows/${encodeURIComponent(id)}/trigger`),
      {
        method: 'POST',
        body: JSON.stringify({
          input: vars,
          trigger: { type: 'manual', source: 'ui', payload: {} },
        }),
      },
    )
    return result.run_id
  }
}

export class HttpRuntimeSkillRepository implements RuntimeSkillRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, signal?: AbortSignal): Promise<Flow[]> {
    return this.api.request<Flow[]>(namespacePath(namespace, 'skills'), {
      signal,
    })
  }

  save(namespace: string, skill: Flow, isNew: boolean) {
    return this.api.request<Flow>(
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

export class HttpRunRepository implements RunRepository {
  constructor(private readonly api: ApiClient) {}

  list(namespace: string, filters: RunFilters = {}, signal?: AbortSignal) {
    const query = queryString({
      page: filters.page ?? 1,
      size: filters.size ?? 100,
      status: filters.status,
      runtime_mode: filters.runtimeMode,
      flow_id: filters.flowId,
    })
    const resource = filters.flowId
      ? `flows/${encodeURIComponent(filters.flowId)}/runs${query}`
      : `runs${query}`
    return this.api.request<Page<FlowRun>>(namespacePath(namespace, resource), { signal })
  }
  get(namespace: string, id: string, flowId?: string, signal?: AbortSignal) {
    return this.api.request<FlowRun>(namespacePath(namespace, runResource(id, flowId)), { signal })
  }
  tasks(namespace: string, id: string, flowId?: string, signal?: AbortSignal) {
    return this.api.request<TaskRun[]>(
      namespacePath(namespace, `${runResource(id, flowId)}/node-runs`),
      { signal },
    )
  }
  approvals(namespace: string, id: string, flowId?: string, signal?: AbortSignal) {
    return this.api.request<HumanApproval[]>(
      namespacePath(namespace, `${runResource(id, flowId)}/approvals`),
      { signal },
    )
  }
  async resolveApproval(
    namespace: string,
    runId: string,
    approvalId: string,
    decision: 'approve' | 'reject',
    flowId?: string,
  ) {
    await this.api.request<{ status: string }>(
      namespacePath(
        namespace,
        `${runResource(runId, flowId)}/approvals/${encodeURIComponent(approvalId)}/${decision}`,
      ),
      { method: 'POST', body: JSON.stringify({}) },
    )
  }
  cancel(namespace: string, id: string, flowId?: string) {
    return this.api.request<void>(namespacePath(namespace, `${runResource(id, flowId)}/cancel`), {
      method: 'POST',
    })
  }
  remove(namespace: string, id: string, flowId?: string) {
    return this.api.request<void>(namespacePath(namespace, runResource(id, flowId)), {
      method: 'DELETE',
    })
  }
}

export class HttpTraceRepository implements TraceRepository {
  constructor(private readonly api: ApiClient) {}

  getRunTrace(namespace: string, runId: string, flowId?: string, signal?: AbortSignal) {
    return this.api.request<RunTrace>(
      namespacePath(namespace, `${runResource(runId, flowId)}/trace`),
      { signal },
    )
  }
}

function runResource(runId: string, flowId?: string): string {
  const run = encodeURIComponent(runId)
  return flowId ? `flows/${encodeURIComponent(flowId)}/runs/${run}` : `runs/${run}`
}

export class HttpAnalyticsRepository implements AnalyticsRepository {
  constructor(private readonly api: ApiClient) {}
  metrics(namespace: string, hours: number, signal?: AbortSignal) {
    const buckets = hours <= 24 ? Math.min(hours, 12) : 14
    return this.api.request<RunMetrics>(
      namespacePath(namespace, `runs/metrics${queryString({ hours, buckets })}`),
      { signal },
    )
  }
}

export class HttpKnowledgeRepository implements KnowledgeRepository {
  constructor(private readonly api: ApiClient) {}
  async list(namespace: string, scope: 'namespace' | 'flow' | 'run', signal?: AbortSignal) {
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
