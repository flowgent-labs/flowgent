import type { NodeRun, TraceSpan } from '../../core/domain/types'

function attemptTime(task: NodeRun): number {
  return new Date(task.started_at ?? task.created_at).getTime()
}

export function sortAttempts(tasks: NodeRun[]): NodeRun[] {
  return [...tasks].sort(
    (left, right) =>
      left.attempt - right.attempt ||
      left.sequence - right.sequence ||
      attemptTime(left) - attemptTime(right),
  )
}

export function attemptsForNode(tasks: NodeRun[], nodeId?: string): NodeRun[] {
  if (!nodeId) return []
  return sortAttempts(tasks.filter((task) => task.node_instance_id === nodeId))
}

export function latestAttempt(tasks: NodeRun[], nodeId: string): NodeRun | undefined {
  return attemptsForNode(tasks, nodeId).at(-1)
}

export function retryCount(tasks: NodeRun[], nodeId: string): number {
  const attempts = attemptsForNode(tasks, nodeId)
  if (!attempts.length) return 0
  return Math.max(attempts.length - 1, ...attempts.map((task) => task.attempt - 1))
}

function attributeString(span: TraceSpan, key: string): string | undefined {
  const value = span.attributes[key]
  return value === undefined || value === null ? undefined : String(value)
}

export function correlateAttempt(span: TraceSpan, tasks: NodeRun[]): NodeRun | undefined {
  const nodeRunId = attributeString(span, 'flowgent.node_run_id')
  return nodeRunId ? tasks.find((task) => task.id === nodeRunId) : undefined
}

export interface TraceTreeRow {
  span: TraceSpan
  depth: number
}

export function traceSpanKey(span: TraceSpan): string {
  return `${span.trace_id}:${span.span_id}`
}

export function traceRowForAttempt<T extends TraceTreeRow>(
  rows: T[],
  nodeRunId?: string | null,
): T | undefined {
  if (!nodeRunId) return undefined
  return rows
    .filter(({ span }) => attributeString(span, 'flowgent.node_run_id') === nodeRunId)
    .sort(
      (left, right) =>
        left.depth - right.depth || right.span.duration_micros - left.span.duration_micros,
    )[0]
}

export function flattenSpanTree(spans: TraceSpan[]): TraceTreeRow[] {
  const byParent = new Map<string, TraceSpan[]>()
  const ids = new Set(spans.map((span) => span.span_id))
  const roots: TraceSpan[] = []
  const compare = (left: TraceSpan, right: TraceSpan) =>
    new Date(left.start_time).getTime() - new Date(right.start_time).getTime()

  spans.forEach((span) => {
    if (!span.parent_span_id || !ids.has(span.parent_span_id)) {
      roots.push(span)
      return
    }
    byParent.set(span.parent_span_id, [...(byParent.get(span.parent_span_id) ?? []), span])
  })
  roots.sort(compare)
  byParent.forEach((children) => children.sort(compare))

  const rows: TraceTreeRow[] = []
  const visited = new Set<string>()
  const visit = (span: TraceSpan, depth: number) => {
    if (visited.has(span.span_id)) return
    visited.add(span.span_id)
    rows.push({ span, depth })
    ;(byParent.get(span.span_id) ?? []).forEach((child) => visit(child, depth + 1))
  }
  roots.forEach((root) => visit(root, 0))
  spans
    .filter((span) => !visited.has(span.span_id))
    .sort(compare)
    .forEach((span) => visit(span, 0))
  return rows
}
