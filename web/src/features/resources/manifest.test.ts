import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { validateFlow } from '../flows/flow-validation'
import { manifestToFlow, manifestToLlm, manifestToMcp } from './manifest'

describe('resource manifest adapters', () => {
  it('rejects envelopes without the canonical console version and data field', () => {
    expect(() =>
      manifestToFlow(
        `apiVersion: core.flowgent.io/v1\nkind: Flow\nmetadata: { name: old }\ndata: { nodes: [], edges: [] }`,
        'Flow',
      ),
    ).toThrow('canonical envelope')
    expect(() =>
      manifestToFlow(
        `consoleVersion: core.flowgent.io/v1\nkind: Flow\nmetadata: { name: old }\nspec: { nodes: [], edges: [] }`,
        'Flow',
      ),
    ).toThrow('canonical envelope')
    expect(() =>
      manifestToFlow(
        `consoleVersion: core.flowgent.io/v1\nkind: Flow\nmetadata: { name: old }\ndata: { runtime_mode: application, nodes: [], edges: [] }\nspec: {}`,
        'Flow',
      ),
    ).toThrow('canonical envelope')
  })

  it('requires an explicit runtime mode for flows', () => {
    expect(() =>
      manifestToFlow(
        `consoleVersion: core.flowgent.io/v1\nkind: Flow\nmetadata: { name: no-runtime }\ndata: { nodes: [], edges: [] }`,
        'Flow',
      ),
    ).toThrow('runtime_mode')
  })

  it('keeps LLM credentials as environment references only', () => {
    const provider = manifestToLlm(`
consoleVersion: core.flowgent.io/v1
kind: LLMProvider
metadata: { name: deepseek, namespace: default, status: active }
data:
  provider: OPENAI
  endpoint: https://api.example/v1
  api_key_env: DEEPSEEK_API_KEY
  defaultModel: chat
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
  type: streamable-http
  url: https://mcp.example/mcp
  header_refs:
    Authorization: Bearer \${GITHUB_TOKEN}
`)
    expect(server.header_refs).toEqual({ Authorization: 'Bearer ${GITHUB_TOKEN}' })
    expect(server.type).toBe('streamable-http')
  })

  it('keeps canonical flat skill nodes unchanged', () => {
    const skill = manifestToFlow(
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
      manifestToFlow(
        `consoleVersion: core.flowgent.io/v1
kind: Flow
metadata: { name: old-node }
data:
  runtime_mode: application
  nodes:
    - id: end
      type: noop
  edges: []`,
        'Flow',
      ),
    ).toThrow('canonical node contract')
  })

  it('imports the canonical security fixer without blocking validation errors', () => {
    const source = readFileSync(
      resolve(
        process.cwd(),
        '../use-cases/security-autonomy-fixer/e2e/config/flows/security-autonomy-fixer.yaml',
      ),
      'utf8',
    ).replaceAll('${FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY}', 'github.com')
    const flow = manifestToFlow(source, 'Flow')
    expect(flow.nodes).toHaveLength(29)
    expect(validateFlow(flow).filter((issue) => issue.code !== 'cycle')).toEqual([])
  })
})
