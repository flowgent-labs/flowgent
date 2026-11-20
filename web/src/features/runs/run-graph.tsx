import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import clsx from 'clsx'
import {
  Activity,
  Bot,
  Box,
  Braces,
  GitBranch,
  Network,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  Workflow,
} from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { Flow, FlowNode, TaskRun } from '../../core/domain/types'
import { StatusBadge } from '../../shared/components/ui'
import { latestAttempt, retryCount } from './run-attempts'
import { graphLayers } from '../flows/graph-layout'

interface RunNodeData extends Record<string, unknown> {
  node: FlowNode
  task?: TaskRun
  retries: number
}
type RunCanvasNode = Node<RunNodeData, 'run-node'>

const runNodeIcons: Record<FlowNode['kind'], React.ComponentType<{ size?: number }>> = {
  agent: Bot,
  condition: GitBranch,
  sandbox: Braces,
  human: ShieldCheck,
  tool: Box,
  skill: Sparkles,
  supervisor: Activity,
  map: Workflow,
  agentflow: Network,
  committee: Network,
  join: Network,
  noop: Network,
}

export function RunGraph({
  flow,
  tasks,
  selected,
  onSelect,
}: {
  flow: Flow
  tasks: TaskRun[]
  selected?: string
  onSelect: (nodeId: string) => void
}) {
  const graph = useMemo(() => layout(flow, tasks), [flow, tasks])
  return (
    <div
      className="run-graph"
      data-testid="run-graph"
      data-task-count={tasks.length}
      data-task-node-count={new Set(tasks.map((task) => task.node_key)).size}
    >
      <ReactFlow<RunCanvasNode, Edge>
        nodes={graph.nodes.map((node) => ({ ...node, selected: node.id === selected }))}
        edges={graph.edges}
        nodeTypes={{ 'run-node': RunNodeCard }}
        onNodeClick={(_, node) => onSelect(node.id)}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        fitView
        fitViewOptions={{ padding: 0.12, maxZoom: 1 }}
        minZoom={0.08}
        maxZoom={1.4}
      >
        <Background variant={BackgroundVariant.Dots} gap={18} size={1} />
        <MiniMap pannable zoomable />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function RunNodeCard({ data, selected }: NodeProps<RunCanvasNode>) {
  const { t } = useTranslation()
  const Icon = runNodeIcons[data.node.kind]
  return (
    <div
      data-testid="run-node"
      data-node-id={data.node.id}
      data-has-task={Boolean(data.task)}
      data-retry-count={data.retries}
      className={clsx(
        'flow-node run-node',
        selected && 'is-selected',
        data.task && `run-node--${data.task.status.toLowerCase().replace('_', '-')}`,
      )}
    >
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="flow-node__top">
        <span className={`kind-icon kind-icon--${data.node.kind}`}>
          <Icon size={15} />
        </span>
        <span className="flow-node__kind">{data.node.kind}</span>
      </div>
      <strong>{data.node.id}</strong>
      {data.task ? <StatusBadge status={data.task.status} /> : <small>{t('runs.noTask')}</small>}
      {data.retries > 0 && (
        <span className="run-node__retries">
          <RotateCcw size={10} />
          {t('runs.retryCount', { count: data.retries })}
        </span>
      )}
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  )
}

function layout(flow: Flow, tasks: TaskRun[]) {
  const levels = graphLayers(
    flow.nodes.map((node) => node.id),
    flow.edges,
  )
  const columns = new Map<number, string[]>()
  flow.nodes.forEach((node) => {
    const level = levels.get(node.id) ?? 0
    columns.set(level, [...(columns.get(level) ?? []), node.id])
  })
  const nodes: RunCanvasNode[] = flow.nodes.map((node) => {
    const level = levels.get(node.id) ?? 0
    const row = columns.get(level)?.indexOf(node.id) ?? 0
    return {
      id: node.id,
      type: 'run-node',
      position: { x: 60 + level * 260, y: 55 + row * 140 },
      data: {
        node,
        task: latestAttempt(tasks, node.id),
        retries: retryCount(tasks, node.id),
      },
    }
  })
  const edges: Edge[] = flow.edges.map((edge, index) => ({
    id: `run-edge-${index}`,
    source: edge.from,
    target: edge.to,
    label: edge.condition === undefined ? undefined : String(edge.condition),
    markerEnd: { type: MarkerType.ArrowClosed },
    animated: latestAttempt(tasks, edge.to)?.status === 'RUNNING',
  }))
  return { nodes, edges }
}
