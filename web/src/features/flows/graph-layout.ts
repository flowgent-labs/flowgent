import type { FlowEdge } from '../../core/domain/types'

/**
 * Compute finite display layers while preserving feedback edges for rendering.
 * DFS back-edges are excluded only from rank calculation. This keeps a
 * persisted feedback graph readable without pretending the runtime provides
 * loop/reset semantics.
 */
export function graphLayers(nodeIds: string[], edges: FlowEdge[]): Map<string, number> {
  const known = new Set(nodeIds)
  const outgoing = new Map<string, FlowEdge[]>()
  edges.forEach((edge) => {
    if (!known.has(edge.from) || !known.has(edge.to)) return
    outgoing.set(edge.from, [...(outgoing.get(edge.from) ?? []), edge])
  })
  const state = new Map<string, 'visiting' | 'visited'>()
  const forwardEdges: FlowEdge[] = []
  const visit = (nodeId: string) => {
    state.set(nodeId, 'visiting')
    for (const edge of outgoing.get(nodeId) ?? []) {
      if (state.get(edge.to) === 'visiting') continue
      forwardEdges.push(edge)
      if (!state.has(edge.to)) visit(edge.to)
    }
    state.set(nodeId, 'visited')
  }
  nodeIds.forEach((nodeId) => !state.has(nodeId) && visit(nodeId))

  const levels = new Map(nodeIds.map((nodeId) => [nodeId, 0]))
  for (let pass = 0; pass < nodeIds.length; pass += 1) {
    forwardEdges.forEach((edge) =>
      levels.set(edge.to, Math.max(levels.get(edge.to) ?? 0, (levels.get(edge.from) ?? 0) + 1)),
    )
  }
  return levels
}
