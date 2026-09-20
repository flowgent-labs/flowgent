import type { Flow } from '../../core/domain/types'
import { sameResourceName, validResourceName } from '../../core/domain/resource-name'

export interface FlowValidationIssue {
  path: string
  code: string
  params?: Record<string, string>
  message: string
}

export function validateFlow(
  flow: Pick<Flow, 'id' | 'runtime_mode' | 'nodes' | 'edges'>,
  existingNames: string[] = [],
): FlowValidationIssue[] {
  const issues: FlowValidationIssue[] = []
  const add = (path: string, code: string, message: string, params?: Record<string, string>) =>
    issues.push({ path, code, message, params })
  if (!flow.id.trim()) add('id', 'flowIdRequired', 'Flow ID is required.')
  else if (!validResourceName(flow.id))
    add(
      'id',
      'flowNameInvalid',
      "Flow name must start with a letter, use only letters, numbers, '_' or '-', and contain at most 32 characters.",
    )
  else if (existingNames.some((name) => sameResourceName(name, flow.id)))
    add('id', 'flowNameExists', 'Flow name already exists in this namespace.')
  if (!['application', 'session'].includes(flow.runtime_mode))
    add('runtime_mode', 'runtimeModeRequired', 'Runtime mode must be application or session.')
  const ids = new Set<string>()
  for (const [index, node] of flow.nodes.entries()) {
    const path = `nodes.${index}`
    if (!node.id.trim()) add(`${path}.id`, 'nodeIdRequired', 'Node ID is required.')
    if (ids.has(node.id))
      add(`${path}.id`, 'duplicateNode', `Duplicate node ID: ${node.id}`, { nodeId: node.id })
    ids.add(node.id)
    if (node.kind === 'agent' && !node.agent)
      add(path, 'agentRequired', `${node.id}: agent reference is required.`, { nodeId: node.id })
    if (node.kind === 'tool' && !node.tool)
      add(path, 'toolRequired', `${node.id}: tool reference is required.`, { nodeId: node.id })
    if (node.kind === 'skill' && !node.skill)
      add(path, 'skillRequired', `${node.id}: skill reference is required.`, { nodeId: node.id })
    if (node.kind === 'agentflow' && !node.agentflow)
      add(path, 'subflowRequired', `${node.id}: sub-flow reference is required.`, {
        nodeId: node.id,
      })
    if (node.kind === 'condition' && !node.expression)
      add(path, 'conditionRequired', `${node.id}: condition expression is required.`, {
        nodeId: node.id,
      })
    if (node.kind === 'human' && !node.approval?.timeout)
      add(path, 'approvalRequired', `${node.id}: approval timeout is required.`, {
        nodeId: node.id,
      })
    if (node.kind === 'sandbox' && (!node.runtime || !node.script))
      add(path, 'sandboxRequired', `${node.id}: runtime and script are required.`, {
        nodeId: node.id,
      })
    if (node.kind === 'map' && !node.node)
      add(path, 'nestedRequired', `${node.id}: nested node is required.`, { nodeId: node.id })
  }
  for (const [index, edge] of flow.edges.entries()) {
    if (!ids.has(edge.from))
      add(`edges.${index}.from`, 'unknownSource', `Unknown source node: ${edge.from}`, {
        nodeId: edge.from,
      })
    if (!ids.has(edge.to))
      add(`edges.${index}.to`, 'unknownTarget', `Unknown target node: ${edge.to}`, {
        nodeId: edge.to,
      })
    if (edge.from === edge.to)
      add(`edges.${index}`, 'selfCycle', `Self-cycle is not allowed: ${edge.from}`, {
        nodeId: edge.from,
      })
  }
  for (const node of flow.nodes.filter((item) => item.kind === 'condition')) {
    const branches = flow.edges.filter(
      (edge) => edge.from === node.id && edge.condition !== undefined,
    )
    if (
      !branches.some((edge) => edge.condition === true) ||
      !branches.some((edge) => edge.condition === false)
    ) {
      issues.push({
        path: `nodes.${node.id}`,
        code: 'conditionBranches',
        params: { nodeId: node.id },
        message: `${node.id}: condition requires true and false outgoing branches.`,
      })
    }
  }
  if (
    hasCycle(
      flow.nodes.map((node) => node.id),
      flow.edges,
    )
  )
    add('edges', 'cycle', 'The graph contains a cycle.')
  return issues
}

function hasCycle(nodes: string[], edges: Flow['edges']): boolean {
  const indegree = new Map(nodes.map((id) => [id, 0]))
  const outgoing = new Map(nodes.map((id) => [id, [] as string[]]))
  edges.forEach((edge) => {
    if (!indegree.has(edge.from) || !indegree.has(edge.to)) return
    indegree.set(edge.to, (indegree.get(edge.to) ?? 0) + 1)
    outgoing.get(edge.from)?.push(edge.to)
  })
  const ready = nodes.filter((id) => indegree.get(id) === 0)
  let visited = 0
  while (ready.length) {
    const current = ready.shift()
    if (!current) continue
    visited += 1
    outgoing.get(current)?.forEach((target) => {
      indegree.set(target, (indegree.get(target) ?? 0) - 1)
      if (indegree.get(target) === 0) ready.push(target)
    })
  }
  return visited !== nodes.length
}
