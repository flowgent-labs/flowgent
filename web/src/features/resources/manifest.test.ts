import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { validateAgentGraph } from '../agent-graphs/graph-validation'
import { manifestToAgentGraph, manifestToLlm, manifestToMcp } from './manifest'

describe('resource manifest adapters', () => {
  it('rejects envelopes without the canonical console version and data field', () => {
    expect(() =>
      manifestToAgentGraph(
        `apiVersion: core.flowgent.io/v1\nkind: AgentGraph\nmetadata: { name: old }\ndata: { nodes: [], edges: [] }`,
        'AgentGraph',
      ),
    ).toThrow('canonical envelope')
    expect(() =>
      manifestToAgentGraph(
        `consoleVersion: core.flowgent.io/v1\nkind: AgentGraph\nmetadata: { name: old }\nspec: { nodes: [], edges: [] }`,
        'AgentGraph',
      ),
    ).toThrow('canonical envelope')
    expect(() =>
      manifestToAgentGraph(
        `consoleVersion: core.flowgent.io/v1\nkind: AgentGraph\nmetadata: { name: old }\ndata: { runtime_mode: application, nodes: [], edges: [] }\nspec: {}`,
        'AgentGraph',
      ),
    ).toThrow('canonical envelope')
  })

  it('requires an explicit runtime mode for graphs', () => {
    expect(() =>
      manifestToAgentGraph(
        `consoleVersion: core.flowgent.io/v1\nkind: AgentGraph\nmetadata: { name: no-runtime }\ndata: { nodes: [], edges: [] }`,
        'AgentGraph',
      ),
    ).toThrow('runtime_mode')
  })

  it('keeps LLM credentials as environment references only', () => {
    const provider = manifestToLlm(`
consoleVersion: core.flowgent.io/v1
kind: LLMProvider
metadata: { name: deepseek, namespace: default, status: active }
data:
  type: openai
  base_uri: https://api.example/v1
  api_key_env: DEEPSEEK_API_KEY
  default_model: chat
  models: []
`)
    expect(provider.api_key_env).toBe('DEEPSEEK_API_KEY')
  })

  it('preserves MCP secret references without creating plaintext header state', () => {
    const server = manifestToMcp(`
consoleVersion: core.flowgent.io/v1
kind: MCP
metadata: { name: github, namespace: default, status: active }
data:
  transport: http
  rpc_url: https://mcp.example/mcp
  header_refs:
    Authorization: Bearer \${GITHUB_TOKEN}
`)
    expect(server.header_refs).toEqual({ Authorization: 'Bearer ${GITHUB_TOKEN}' })
    expect(server.transport).toBe('http')
  })

  it('keeps canonical flat skill nodes unchanged', () => {
    const skill = manifestToAgentGraph(
      `
consoleVersion: core.flowgent.io/v1
kind: Skill
metadata: { name: nested-skill, namespace: default, status: active }
data:
  id: nested-skill
  nodes:
    - id: query
      kind: sandbox
      runtime: bash
      script: echo ok
  edges: []
`,
      'Skill',
    )
    expect(skill.kind).toBe('skill')
    expect(skill.nodes[0]).toMatchObject({ id: 'query', kind: 'sandbox', runtime: 'bash' })
  })

  it('rejects nested and type-based node shapes', () => {
    expect(() =>
      manifestToAgentGraph(
        `consoleVersion: core.flowgent.io/v1
kind: AgentGraph
metadata: { name: old-node }
data:
  runtime_mode: application
  nodes:
    - id: end
      type: noop
  edges: []`,
        'AgentGraph',
      ),
    ).toThrow('canonical node contract')
  })

  it('imports the canonical security fixer without blocking validation errors', () => {
    const source = readFileSync(
      resolve(
        process.cwd(),
        '../use-cases/security-autonomy-fixer/deploy/flowgent/config/agent-graphs/security-autonomy-fixer.yaml',
      ),
      'utf8',
    ).replaceAll('${FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY}', 'github.com')
    const graph = manifestToAgentGraph(source, 'AgentGraph')
    expect(graph.nodes).toHaveLength(29)
    expect(validateAgentGraph(graph).filter((issue) => issue.code !== 'cycle')).toEqual([])
  })
})
