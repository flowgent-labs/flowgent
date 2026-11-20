import type { TaskRun } from '../../core/domain/types'

export type FileChangeKind = 'added' | 'modified' | 'deleted'

export interface TaskFileChange {
  path: string
  before: string
  after: string
  kind: FileChangeKind
  rule?: string
  description?: string
}

export interface DiffLine {
  kind: 'context' | 'addition' | 'deletion' | 'omitted'
  text: string
  oldLine?: number
  newLine?: number
  omitted?: number
}

type JsonRecord = Record<string, unknown>

function record(value: unknown): JsonRecord | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as JsonRecord)
    : undefined
}

function parsedRecord(value: unknown): JsonRecord | undefined {
  const direct = record(value)
  if (direct) return direct
  if (typeof value !== 'string' || !value.trim()) return undefined
  try {
    return record(JSON.parse(value))
  } catch {
    return undefined
  }
}

function payloadRecords(value: unknown): JsonRecord[] {
  const root = parsedRecord(value)
  if (!root) return []
  const records = [root]
  for (const key of ['args', 'result', 'data', 'payload', 'output', 'stdout']) {
    const nested = parsedRecord(root[key])
    if (nested) records.push(nested)
  }
  return records
}

interface FilePayload {
  path: string
  content: string
  deleted: boolean
}

function filePayload(value: unknown): FilePayload | undefined {
  const item = record(value)
  if (!item) return undefined
  const pathValue = item.path ?? item.file ?? item.filename
  if (typeof pathValue !== 'string' || !pathValue.trim()) return undefined
  const contentValue =
    item.content ?? item.after ?? item.new_content ?? item.newContent ?? item.modified_content
  const deleted = item.deleted === true || String(item.status ?? '').toLowerCase() === 'deleted'
  if (!deleted && typeof contentValue !== 'string') return undefined
  return { path: pathValue.trim(), content: deleted ? '' : String(contentValue), deleted }
}

function filesFrom(records: JsonRecord[], keys: string[]): FilePayload[] {
  for (const candidate of records) {
    for (const key of keys) {
      if (!Array.isArray(candidate[key])) continue
      const files = candidate[key].map(filePayload).filter((item): item is FilePayload => !!item)
      if (files.length) return files
    }
  }
  return []
}

function patchMetadata(
  records: JsonRecord[],
): Map<string, { rule?: string; description?: string }> {
  const metadata = new Map<string, { rule?: string; description?: string }>()
  for (const candidate of records) {
    const patches = Array.isArray(candidate.patches)
      ? candidate.patches
      : Array.isArray(candidate.fixes)
        ? candidate.fixes
        : []
    patches.forEach((value) => {
      const patch = record(value)
      const path = patch && (patch.file ?? patch.path ?? patch.filename)
      if (!patch || typeof path !== 'string') return
      metadata.set(path, {
        rule: typeof patch.rule === 'string' ? patch.rule : undefined,
        description: typeof patch.description === 'string' ? patch.description : undefined,
      })
    })
  }
  return metadata
}

export function extractTaskFileChanges(task: Pick<TaskRun, 'input' | 'output'>): TaskFileChange[] {
  const inputs = payloadRecords(task.input)
  const outputs = payloadRecords(task.output)
  const original = new Map(
    filesFrom(inputs, ['source_files', 'original_files', 'files']).map((file) => [
      file.path,
      file.content,
    ]),
  )
  const changed = filesFrom(outputs, ['files', 'changed_files', 'changes'])
  const metadata = patchMetadata(outputs)

  return changed.map((file) => {
    const before = original.get(file.path) ?? ''
    const details = metadata.get(file.path)
    return {
      path: file.path,
      before,
      after: file.content,
      kind: file.deleted ? 'deleted' : original.has(file.path) ? 'modified' : 'added',
      ...details,
    }
  })
}

function rawLineDiff(before: string, after: string): DiffLine[] {
  const oldLines = before ? before.split('\n') : []
  const newLines = after ? after.split('\n') : []
  const columns = newLines.length + 1
  const cells = (oldLines.length + 1) * columns

  if (cells > 1_000_000) {
    let prefix = 0
    while (
      prefix < oldLines.length &&
      prefix < newLines.length &&
      oldLines[prefix] === newLines[prefix]
    )
      prefix += 1
    let suffix = 0
    while (
      suffix < oldLines.length - prefix &&
      suffix < newLines.length - prefix &&
      oldLines[oldLines.length - suffix - 1] === newLines[newLines.length - suffix - 1]
    )
      suffix += 1
    return [
      ...oldLines.slice(0, prefix).map((text, index) => ({
        kind: 'context' as const,
        text,
        oldLine: index + 1,
        newLine: index + 1,
      })),
      ...oldLines.slice(prefix, oldLines.length - suffix).map((text, index) => ({
        kind: 'deletion' as const,
        text,
        oldLine: prefix + index + 1,
      })),
      ...newLines.slice(prefix, newLines.length - suffix).map((text, index) => ({
        kind: 'addition' as const,
        text,
        newLine: prefix + index + 1,
      })),
      ...oldLines.slice(oldLines.length - suffix).map((text, index) => ({
        kind: 'context' as const,
        text,
        oldLine: oldLines.length - suffix + index + 1,
        newLine: newLines.length - suffix + index + 1,
      })),
    ]
  }

  const lcs = new Uint32Array(cells)
  for (let oldIndex = oldLines.length - 1; oldIndex >= 0; oldIndex -= 1) {
    for (let newIndex = newLines.length - 1; newIndex >= 0; newIndex -= 1) {
      const offset = oldIndex * columns + newIndex
      lcs[offset] =
        oldLines[oldIndex] === newLines[newIndex]
          ? (lcs[(oldIndex + 1) * columns + newIndex + 1] ?? 0) + 1
          : Math.max(lcs[(oldIndex + 1) * columns + newIndex] ?? 0, lcs[offset + 1] ?? 0)
    }
  }

  const lines: DiffLine[] = []
  let oldIndex = 0
  let newIndex = 0
  while (oldIndex < oldLines.length || newIndex < newLines.length) {
    if (
      oldIndex < oldLines.length &&
      newIndex < newLines.length &&
      oldLines[oldIndex] === newLines[newIndex]
    ) {
      lines.push({
        kind: 'context',
        text: oldLines[oldIndex]!,
        oldLine: oldIndex + 1,
        newLine: newIndex + 1,
      })
      oldIndex += 1
      newIndex += 1
    } else if (
      newIndex < newLines.length &&
      (oldIndex === oldLines.length ||
        (lcs[oldIndex * columns + newIndex + 1] ?? 0) >=
          (lcs[(oldIndex + 1) * columns + newIndex] ?? 0))
    ) {
      lines.push({ kind: 'addition', text: newLines[newIndex]!, newLine: newIndex + 1 })
      newIndex += 1
    } else {
      lines.push({ kind: 'deletion', text: oldLines[oldIndex]!, oldLine: oldIndex + 1 })
      oldIndex += 1
    }
  }
  return lines
}

function compactContext(lines: DiffLine[], contextSize = 3): DiffLine[] {
  const compacted: DiffLine[] = []
  let index = 0
  while (index < lines.length) {
    if (lines[index]!.kind !== 'context') {
      compacted.push(lines[index]!)
      index += 1
      continue
    }
    let end = index
    while (end < lines.length && lines[end]!.kind === 'context') end += 1
    const run = lines.slice(index, end)
    const edge = index === 0 || end === lines.length ? contextSize : contextSize * 2
    if (run.length <= edge + 1) compacted.push(...run)
    else {
      const head = index === 0 ? 0 : contextSize
      const tail = end === lines.length ? 0 : contextSize
      if (head) compacted.push(...run.slice(0, head))
      compacted.push({
        kind: 'omitted',
        text: '',
        omitted: run.length - head - tail,
      })
      if (tail) compacted.push(...run.slice(-tail))
    }
    index = end
  }
  return compacted
}

export function buildLineDiff(before: string, after: string): DiffLine[] {
  return compactContext(rawLineDiff(before, after))
}

export function diffStats(lines: DiffLine[]): { additions: number; deletions: number } {
  return lines.reduce(
    (stats, line) => {
      if (line.kind === 'addition') stats.additions += 1
      if (line.kind === 'deletion') stats.deletions += 1
      return stats
    },
    { additions: 0, deletions: 0 },
  )
}
