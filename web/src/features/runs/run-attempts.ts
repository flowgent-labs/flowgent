import type { TaskRun, TraceSpan } from '../../core/domain/types'

function attemptTime(task: TaskRun): number {
  return new Date(task.started_at ?? task.created_at).getTime()
}

export function sortAttempts(tasks: TaskRun[]): TaskRun[] {
  return [...tasks].sort(
    (left, right) =>
      left.sequence - right.sequence ||
      left.retry_count - right.retry_count ||
      attemptTime(left) - attemptTime(right),
  )
}

export function attemptsForNode(tasks: TaskRun[], nodeId?: string): TaskRun[] {
  if (!nodeId) return []
  return sortAttempts(tasks.filter((task) => task.node_id === nodeId))
}

export function latestAttempt(tasks: TaskRun[], nodeId: string): TaskRun | undefined {
  return attemptsForNode(tasks, nodeId).at(-1)
}

export function retryCount(tasks: TaskRun[], nodeId: string): number {
  const attempts = attemptsForNode(tasks, nodeId)
  if (!attempts.length) return 0
  return Math.max(attempts.length - 1, ...attempts.map((task) => task.retry_count))
}

function attributeString(span: TraceSpan, key: string): string | undefined {
  const value = span.attributes[key]
  return value === undefined || value === null ? undefined : String(value)
}

export function correlateAttempt(span: TraceSpan, tasks: TaskRun[]): TaskRun | undefined {
  const taskId = attributeString(span, 'flowgent.task_id')
  return taskId ? tasks.find((task) => task.id === taskId) : undefined
}

export interface TraceTreeRow {
  span: TraceSpan
  depth: number
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
