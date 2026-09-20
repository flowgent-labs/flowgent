import { describe, expect, it } from 'vitest'
import type { Flow } from '../../core/domain/types'
import { validateFlow } from './flow-validation'

const flow = (overrides: Partial<Flow> = {}): Flow => ({
  id: 'valid-flow',
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
  kind: overrides.kind ?? 'flow',
})

describe('validateFlow', () => {
  it('accepts a valid conditional DAG', () => {
    expect(validateFlow(flow())).toEqual([])
  })

  it('enforces the public name contract and case-insensitive uniqueness', () => {
    expect(validateFlow(flow({ id: '1-invalid' }))).toContainEqual(
      expect.objectContaining({ code: 'flowNameInvalid' }),
    )
    expect(validateFlow(flow({ id: 'Security_Fixer' }), ['security_fixer'])).toContainEqual(
      expect.objectContaining({ code: 'flowNameExists' }),
    )
  })

  it('rejects duplicates, dangling edges and cycles', () => {
    const issues = validateFlow(
      flow({
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
    const issues = validateFlow(flow({ edges: [{ from: 'gate', to: 'yes', condition: true }] }))
    expect(issues).toContainEqual(
      expect.objectContaining({ message: expect.stringContaining('true and false') }),
    )
  })
})
