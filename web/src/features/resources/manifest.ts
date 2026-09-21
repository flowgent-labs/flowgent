import { parse } from 'yaml'
import type { Agent, Flow, FlowNode, LlmProvider, McpServer } from '../../core/domain/types'

interface ManifestMetadata {
  name?: string
  namespace?: string
  labels?: Record<string, string>
  status?: string
  description?: string
}

interface ResourceManifest {
  consoleVersion: 'core.flowgent.io/v1'
  kind: string
  metadata: ManifestMetadata
  data: Record<string, unknown>
}

const emptyAudit = {
  created_at: '',
  updated_at: '',
}

function status(value?: string): string {
  return value?.toUpperCase() || 'ACTIVE'
}

export function parseResourceManifest(source: string, expectedKind: string): ResourceManifest {
  const raw = parse(source) as Record<string, unknown>
  if (!raw || typeof raw !== 'object') throw new Error('Manifest must be a YAML or JSON object.')
  const unknownEnvelopeField = Object.keys(raw).find(
    (field) => !['consoleVersion', 'kind', 'metadata', 'data'].includes(field),
  )
  if (unknownEnvelopeField) {
    throw new Error(`Manifest field ${unknownEnvelopeField} is not part of the canonical envelope.`)
  }
  if (raw.consoleVersion !== 'core.flowgent.io/v1') {
    throw new Error('Manifest requires consoleVersion core.flowgent.io/v1.')
  }
  const kind = String(raw.kind ?? '')
  if (kind !== expectedKind) {
    throw new Error(`Expected kind ${expectedKind}, received ${kind || 'unknown'}.`)
  }
  const metadata = (raw.metadata ?? {}) as ManifestMetadata
  const data = raw.data as Record<string, unknown> | undefined
  if (!metadata.name || !data || typeof data !== 'object') {
    throw new Error('Manifest requires metadata.name and data.')
  }
  return { consoleVersion: 'core.flowgent.io/v1', kind, metadata, data }
}

function manifestNodes(value: unknown): FlowNode[] {
  if (value === undefined) return []
  if (!Array.isArray(value)) throw new Error('data.nodes must be an array.')
  value.forEach((item, index) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      throw new Error(`data.nodes[${index}] must be an object.`)
    }
    const node = item as Record<string, unknown>
    for (const field of ['type', 'data', 'spec', 'input']) {
      if (field in node) {
        throw new Error(`data.nodes[${index}].${field} is not part of the canonical node contract.`)
      }
    }
  })
  return value as FlowNode[]
}

export function manifestToFlow(source: string, expectedKind: 'Flow' | 'Skill'): Flow {
  const manifest = parseResourceManifest(source, expectedKind)
  const data = manifest.data
  const runtimeMode = data.runtime_mode
  if (expectedKind === 'Flow' && runtimeMode !== 'application' && runtimeMode !== 'session') {
    throw new Error('Flow data.runtime_mode must be application or session.')
  }
  return {
    ...emptyAudit,
    ...(data as unknown as Flow),
    id: String(data.id ?? manifest.metadata.name),
    kind: expectedKind.toLowerCase(),
    namespace_id: manifest.metadata.namespace ?? 'default',
    description: String(data.description ?? manifest.metadata.description ?? ''),
    labels: (data.labels as Record<string, string> | undefined) ?? manifest.metadata.labels ?? {},
    status: status(manifest.metadata.status),
    runtime_mode:
      runtimeMode === 'application' || runtimeMode === 'session' ? runtimeMode : 'session',
    nodes: manifestNodes(data.nodes),
    edges: Array.isArray(data.edges) ? (data.edges as Flow['edges']) : [],
  }
}

export function manifestToAgent(source: string): Agent {
  const { metadata, data } = parseResourceManifest(source, 'Agent')
  return {
    ...emptyAudit,
    ...(data as unknown as Agent),
    id: '',
    name: metadata.name ?? '',
    namespace_id: metadata.namespace ?? 'default',
    description: metadata.description ?? '',
    labels: metadata.labels ?? {},
    status: status(metadata.status),
  }
}

function environmentName(value: unknown): string {
  const name = typeof value === 'string' ? value.trim() : ''
  if (!/^[A-Z_][A-Z0-9_]*$/.test(name)) {
    throw new Error('api_key_env must name an injected environment variable.')
  }
  return name
}

export function manifestToLlm(source: string): LlmProvider {
  const { metadata, data } = parseResourceManifest(source, 'LLMProvider')
  const legacyProvider = String(data.provider ?? '')
  const requestedType = String(data.type ?? legacyProvider).toLowerCase()
  const type =
    requestedType === 'anthropic' || requestedType === 'gemini' ? requestedType : 'openai'
  return {
    ...emptyAudit,
    ...(data as unknown as LlmProvider),
    id: '',
    name: metadata.name ?? '',
    type,
    provider: metadata.name ?? '',
    namespace_id: metadata.namespace ?? 'default',
    description: metadata.description ?? '',
    labels: metadata.labels ?? {},
    status: status(metadata.status),
    enabled: status(metadata.status) === 'ACTIVE',
    api_key_env: environmentName(data.api_key_env),
    key_configured: true,
  }
}

export function manifestToMcp(source: string): McpServer {
  const { metadata, data } = parseResourceManifest(source, 'MCP')
  if (data.type !== 'streamable-http') {
    throw new Error('MCP data.type must be streamable-http.')
  }
  return {
    ...emptyAudit,
    ...(data as unknown as McpServer),
    id: '',
    name: metadata.name ?? '',
    namespace_id: metadata.namespace ?? 'default',
    description: metadata.description ?? '',
    labels: metadata.labels ?? {},
    status: status(metadata.status),
    enabled: status(metadata.status) === 'ACTIVE',
    type: data.type,
    header_refs: (data.header_refs as Record<string, string> | undefined) ?? {},
    env_refs: (data.env_refs as Record<string, string> | undefined) ?? {},
  }
}
