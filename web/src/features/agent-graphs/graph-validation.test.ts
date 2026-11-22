import { describe, expect, it } from 'vitest'
import type { AgentGraph } from '../../core/domain/types'
import { validateAgentGraph } from './graph-validation'

const graph = (overrides: Partial<AgentGraph> = {}): AgentGraph => ({
  id: 'stable-graph-id',
  name: 'valid-graph',
  revision: 1,
  summarize_enabled: false,
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
  runtime_mode: 'application',
  nodes: [
    { id: 'gate', kind: 'condition', expression: 'input.approved' },
    { id: 'yes', kind: 'noop' },
    { id: 'no', kind: 'noop' },
  ],
  edges: [
    { from: 'gate', to: 'yes', condition: true },
    { from: 'gate', to: 'no', condition: false },
  ],
  ...overrides,
  kind: overrides.kind ?? 'graph',
})

describe('validateAgentGraph', () => {
  it('accepts a valid conditional DAG', () => {
    expect(validateAgentGraph(graph())).toEqual([])
  })

  it('enforces the public name contract and case-insensitive uniqueness', () => {
    expect(validateAgentGraph(graph({ name: '1-invalid' }))).toContainEqual(
      expect.objectContaining({ code: 'graphNameInvalid' }),
    )
    expect(
      validateAgentGraph(graph({ name: 'Security_Fixer' }), ['security_fixer']),
    ).toContainEqual(expect.objectContaining({ code: 'graphNameExists' }))
  })

  it('rejects duplicates, dangling edges and cycles', () => {
    const issues = validateAgentGraph(
      graph({
        nodes: [
          { id: 'same', kind: 'noop' },
          { id: 'same', kind: 'agent', agent: '' },
        ],
        edges: [
          { from: 'same', to: 'missing' },
          { from: 'same', to: 'same' },
        ],
      }),
    )
    expect(issues.map((issue) => issue.message).join(' ')).toMatch(/Duplicate node ID/)
    expect(issues.map((issue) => issue.message).join(' ')).toMatch(/Unknown target node/)
    expect(issues.map((issue) => issue.message).join(' ')).toMatch(/Self-cycle/)
    expect(issues.map((issue) => issue.message).join(' ')).toMatch(/agent reference/)
  })

  it('requires both condition branches', () => {
    const issues = validateAgentGraph(
      graph({ edges: [{ from: 'gate', to: 'yes', condition: true }] }),
    )
    expect(issues).toContainEqual(
      expect.objectContaining({ message: expect.stringContaining('true and false') }),
    )
  })
})
