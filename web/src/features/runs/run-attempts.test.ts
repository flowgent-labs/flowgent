import { describe, expect, it } from 'vitest'
import type { TaskRun, TraceSpan } from '../../core/domain/types'
import { attemptsForNode, correlateAttempt, flattenSpanTree, retryCount } from './run-attempts'

const task = (id: string, sequence: number): TaskRun => ({
  id,
  namespace_id: 'default',
  status: sequence === 3 ? 'SUCCESS' : 'FAILED',
  created_at: `2026-08-14T00:00:0${sequence}Z`,
  updated_at: `2026-08-14T00:00:0${sequence}Z`,
  run_id: 'run-1',
  node_key: 'agent',
  attempt: sequence,
  input: { sequence },
  output: {},
  error: '',
  max_retries: 2,
  execution_id: `exec-${sequence}`,
  parent_node_run_id: sequence > 1 ? `task-${sequence - 1}` : undefined,
  sequence,
  fencing_token: sequence,
})

const span = (id: string, parent = '', attributes: Record<string, unknown> = {}): TraceSpan => ({
  trace_id: 'trace-1',
  span_id: id,
  parent_span_id: parent,
  operation_name: id,
  service_name: 'flowgent-jobmanager',
  start_time: `2026-08-14T00:00:0${id.length}Z`,
  duration_micros: 1_000,
  status: 'OK',
  attributes,
})

describe('run attempt projections', () => {
  it('orders retries and derives the logical node retry count', () => {
    const attempts = [task('task-3', 3), task('task-1', 1), task('task-2', 2)]
    expect(attemptsForNode(attempts, 'agent').map((item) => item.id)).toEqual([
      'task-1',
      'task-2',
      'task-3',
    ])
    expect(retryCount(attempts, 'agent')).toBe(2)
  })

  it('correlates a span to the exact persisted attempt', () => {
    const attempts = [task('task-1', 1), task('task-2', 2)]
    expect(
      correlateAttempt(span('child', '', { 'flowgent.task_id': 'task-2' }), attempts)?.id,
    ).toBe('task-2')
    expect(
      correlateAttempt(span('unlinked', '', { 'flowgent.node_id': 'agent' }), attempts),
    ).toBeUndefined()
  })

  it('uses real parentage for tree depth and keeps orphan spans visible', () => {
    const rows = flattenSpanTree([
      span('grandchild', 'child'),
      span('orphan', 'missing'),
      span('root'),
      span('child', 'root'),
    ])
    expect(rows.map(({ span: item, depth }) => [item.span_id, depth])).toEqual([
      ['root', 0],
      ['child', 1],
      ['grandchild', 2],
      ['orphan', 0],
    ])
  })
})
