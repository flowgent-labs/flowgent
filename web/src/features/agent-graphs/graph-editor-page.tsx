import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge as CanvasEdge,
  type EdgeChange,
  type Node as CanvasNode,
  type NodeChange,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import clsx from 'clsx'
import {
  AlertTriangle,
  Activity,
  ArrowLeft,
  Bot,
  Box,
  Braces,
  Check,
  GitBranch,
  GripVertical,
  Network,
  Redo2,
  Save,
  ShieldCheck,
  Sparkles,
  Undo2,
  WandSparkles,
  Waypoints,
  Workflow,
  ExternalLink,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { graphPath, graphRunPath } from '../../app/paths'
import {
  Button,
  ErrorState,
  IconButton,
  JsonView,
  LoadingState,
  StatusBadge,
} from '../../shared/components/ui'
import type { AgentGraph, GraphEdge, GraphNode, GraphRun, NodeKind } from '../../core/domain/types'
import {
  RESOURCE_NAME_MAX_LENGTH,
  RESOURCE_NAME_PATTERN_SOURCE,
} from '../../core/domain/resource-name'
import { nodeKinds } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { validateAgentGraph } from './graph-validation'
import { graphLayers } from './graph-layout'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToAgentGraph } from '../resources/manifest'
import { duration, formatDate } from '../graph-runs/run-utils'

interface CanvasData extends Record<string, unknown> {
  domain: GraphNode
}
type EditorNode = CanvasNode<CanvasData, 'flowgent'>

const nodeIcons: Record<NodeKind, React.ComponentType<{ size?: number }>> = {
  agent: Bot,
  tool: Box,
  skill: Sparkles,
  map: Workflow,
  subgraph: Waypoints,
  condition: GitBranch,
  committee: Network,
  human: ShieldCheck,
  supervisor: WandSparkles,
  sandbox: Braces,
  join: Network,
  noop: Check,
}

const paletteGroups: Array<{ label: string; kinds: NodeKind[] }> = [
  { label: 'intelligence', kinds: ['agent', 'supervisor', 'committee'] },
  { label: 'tooling', kinds: ['tool', 'skill', 'sandbox'] },
  { label: 'control', kinds: ['condition', 'human', 'join', 'noop'] },
  { label: 'composition', kinds: ['map', 'subgraph'] },
]

const emptyAgentGraph = (namespace: string): AgentGraph => ({
  id: '',
  name: '',
  revision: 1,
  summarize_enabled: false,
  description: '',
  summary: '',
  namespace_id: namespace,
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
  kind: 'graph',
  runtime_mode: 'application',
  nodes: [],
  edges: [],
  vars: {},
  labels: {},
  triggers: [],
})

function toCanvasNodes(nodes: GraphNode[]): EditorNode[] {
  return nodes.map((node, index) => ({
    id: node.id,
    type: 'flowgent',
    position: { x: 80 + (index % 3) * 270, y: 70 + Math.floor(index / 3) * 170 },
    data: { domain: node },
  }))
}

function toCanvasEdges(edges: GraphEdge[]): CanvasEdge[] {
  return edges.map((edge, index) => ({
    id: `edge-${index}-${edge.from}-${edge.to}`,
    source: edge.from,
    target: edge.to,
    label: edge.condition === undefined ? undefined : String(edge.condition),
    markerEnd: { type: MarkerType.ArrowClosed },
    className:
      edge.condition === false
        ? 'graph-edge--false'
        : edge.condition === true
          ? 'graph-edge--true'
          : '',
  }))
}

export function AgentGraphEditorPage() {
  return (
    <ReactFlowProvider>
      <AgentGraphEditor />
    </ReactFlowProvider>
  )
}

function AgentGraphEditor() {
  const { t } = useTranslation()
  const { graphId } = useParams()
  const isNew = !graphId
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: [namespace, 'graphs', graphId],
    queryFn: ({ signal }) => repositories.graphs.get(namespace, graphId ?? '', signal),
    enabled: !isNew,
  })
  const existingAgentGraphs = useQuery({
    queryKey: [namespace, 'graphs'],
    queryFn: ({ signal }) => repositories.graphs.list(namespace, signal),
    enabled: isNew,
  })
  const recentRuns = useQuery({
    queryKey: [namespace, 'runs', 'graph-editor', graphId],
    queryFn: ({ signal }) =>
      repositories.graphRuns.list(namespace, { graphId, page: 1, size: 5 }, signal),
    enabled: !isNew,
  })
  const [graph, setAgentGraph] = useState<AgentGraph>(() => emptyAgentGraph(namespace))
  const [nodes, setNodes] = useState<EditorNode[]>([])
  const [edges, setEdges] = useState<CanvasEdge[]>([])
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [preview, setPreview] = useState(false)
  const [issuesVisible, setIssuesVisible] = useState(false)
  const [history, setHistory] = useState<AgentGraph[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const { screenToFlowPosition, fitView } = useReactFlow()
  const issues = useMemo(
    () =>
      validateAgentGraph(
        graph,
        isNew ? (existingAgentGraphs.data ?? []).map((item) => item.name) : [],
      ),
    [existingAgentGraphs.data, graph, isNew],
  )
  const blockingIssues = useMemo(() => issues.filter((issue) => issue.code !== 'cycle'), [issues])

  const loadAgentGraph = useCallback((value: AgentGraph) => {
    setAgentGraph(value)
    setNodes(autoLayoutNodes(toCanvasNodes(value.nodes), value.edges))
    setEdges(toCanvasEdges(value.edges))
    setHistory([structuredClone(value)])
    setHistoryIndex(0)
    setDirty(false)
  }, [])

  useEffect(() => {
    // Loading an addressable backend resource initializes the editor session.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (query.data) loadAgentGraph(query.data)
  }, [loadAgentGraph, query.data])
  useEffect(() => {
    // A new route initializes a fresh canonical graph exactly once.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (isNew && historyIndex < 0) loadAgentGraph(emptyAgentGraph(namespace))
  }, [historyIndex, isNew, loadAgentGraph, namespace])
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault()
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [dirty])

  const commit = useCallback(
    (next: AgentGraph, record = true) => {
      setAgentGraph(next)
      setDirty(true)
      if (record) {
        setHistory((current) =>
          [...current.slice(0, historyIndex + 1), structuredClone(next)].slice(-40),
        )
        setHistoryIndex((current) => Math.min(current + 1, 39))
      }
    },
    [historyIndex],
  )

  const syncGraph = useCallback(
    (nextNodes: EditorNode[], nextEdges: CanvasEdge[], record = true) => {
      setNodes(nextNodes)
      setEdges(nextEdges)
      commit(
        {
          ...graph,
          nodes: nextNodes.map((node) => node.data.domain),
          edges: nextEdges.map((edge) => ({
            from: edge.source,
            to: edge.target,
            condition: edge.label === undefined ? undefined : edge.label === 'true',
          })),
        },
        record,
      )
    },
    [commit, graph],
  )

  const onNodesChange = (changes: NodeChange<EditorNode>[]) => {
    const next = applyNodeChanges(changes, nodes)
    const removed = changes.some((change) => change.type === 'remove')
    if (removed)
      syncGraph(
        next,
        edges.filter(
          (edge) =>
            next.some((node) => node.id === edge.source) &&
            next.some((node) => node.id === edge.target),
        ),
      )
    else {
      setNodes(next)
      if (changes.some((change) => change.type === 'position' && change.dragging === false))
        setDirty(true)
    }
  }
  const onEdgesChange = (changes: EdgeChange<CanvasEdge>[]) => {
    const next = applyEdgeChanges(changes, edges)
    syncGraph(nodes, next)
  }
  const onConnect = (connection: Connection) =>
    syncGraph(nodes, addEdge({ ...connection, markerEnd: { type: MarkerType.ArrowClosed } }, edges))

  const addNode = useCallback(
    (kind: NodeKind, position = { x: 120 + nodes.length * 24, y: 90 + nodes.length * 18 }) => {
      const baseId = kind
      let id: string = baseId
      let suffix = 2
      while (graph.nodes.some((node) => node.id === id)) id = `${baseId}-${suffix++}`
      const domain: GraphNode = {
        id,
        kind,
        ...(kind === 'agent' ? { agent: '' } : {}),
        ...(kind === 'tool' ? { tool: '' } : {}),
        ...(kind === 'condition' ? { expression: '' } : {}),
        ...(kind === 'sandbox' ? { runtime: 'bash', script: '', timeout: '5m' } : {}),
        ...(kind === 'human' ? { approval: { timeout: '24h' } } : {}),
      }
      const nextNodes = [...nodes, { id, type: 'flowgent' as const, position, data: { domain } }]
      syncGraph(nextNodes, edges)
      setSelectedNode(id)
    },
    [edges, graph.nodes, nodes, syncGraph],
  )

  const onDrop = (event: React.DragEvent) => {
    event.preventDefault()
    const kind = event.dataTransfer.getData('application/flowgent-node') as NodeKind
    if (!nodeKinds.includes(kind)) return
    addNode(kind, screenToFlowPosition({ x: event.clientX, y: event.clientY }))
  }

  const updateNode = (id: string, updates: Partial<GraphNode>) => {
    const old = nodes.find((node) => node.id === id)
    if (!old) return
    const newId = updates.id?.trim() || id
    const nextNodes = nodes.map((node) =>
      node.id === id
        ? { ...node, id: newId, data: { domain: { ...node.data.domain, ...updates, id: newId } } }
        : node,
    )
    const nextEdges = edges.map((edge) => ({
      ...edge,
      source: edge.source === id ? newId : edge.source,
      target: edge.target === id ? newId : edge.target,
    }))
    syncGraph(nextNodes, nextEdges)
    setSelectedNode(newId)
  }

  const updateSelectedEdge = (condition: '' | 'true' | 'false') => {
    const next = edges.map((edge) =>
      edge.id === selectedEdge
        ? {
            ...edge,
            label: condition || undefined,
            className: condition ? `graph-edge--${condition}` : '',
          }
        : edge,
    )
    syncGraph(nodes, next)
  }

  const undo = useCallback(() => {
    if (historyIndex <= 0) return
    const nextIndex = historyIndex - 1
    const snapshot = history[nextIndex]
    if (!snapshot) return
    setHistoryIndex(nextIndex)
    setAgentGraph(structuredClone(snapshot))
    setNodes(autoLayoutNodes(toCanvasNodes(snapshot.nodes), snapshot.edges))
    setEdges(toCanvasEdges(snapshot.edges))
    setDirty(true)
  }, [history, historyIndex])
  const redo = useCallback(() => {
    if (historyIndex >= history.length - 1) return
    const nextIndex = historyIndex + 1
    const snapshot = history[nextIndex]
    if (!snapshot) return
    setHistoryIndex(nextIndex)
    setAgentGraph(structuredClone(snapshot))
    setNodes(autoLayoutNodes(toCanvasNodes(snapshot.nodes), snapshot.edges))
    setEdges(toCanvasEdges(snapshot.edges))
    setDirty(true)
  }, [history, historyIndex])
  const autoLayout = () => {
    const next = autoLayoutNodes(nodes, graph.edges)
    setNodes(next)
    setDirty(true)
    requestAnimationFrame(() => void fitView({ padding: 0.2, duration: 350 }))
  }

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'z') {
        event.preventDefault()
        if (event.shiftKey) redo()
        else undo()
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'c' && selectedNode) {
        const selected = graph.nodes.find((node) => node.id === selectedNode)
        if (selected) sessionStorage.setItem('flowgent-node-clipboard', JSON.stringify(selected))
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'v') {
        const raw = sessionStorage.getItem('flowgent-node-clipboard')
        if (raw) {
          const copied = JSON.parse(raw) as GraphNode
          addNode(copied.kind)
        }
      }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [addNode, graph.nodes, redo, selectedNode, undo])

  const save = useMutation({
    mutationFn: () => repositories.graphs.save(namespace, graph, isNew),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: [namespace, 'graphs'] })
      loadAgentGraph(saved)
      if (isNew) navigate(graphPath(namespace, saved.name), { replace: true })
    },
  })
  const saveAgentGraph = () => {
    if (blockingIssues.length) {
      setIssuesVisible(true)
      return
    }
    save.mutate()
  }

  if (!isNew && query.isLoading) return <LoadingState rows={6} />
  if (!isNew && query.isError)
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const selected = nodes.find((node) => node.id === selectedNode)?.data.domain
  const edge = edges.find((item) => item.id === selectedEdge)
  return (
    <div className="editor-page">
      <header className="editor-header">
        <div className="editor-header__left">
          <IconButton label={t('common.back')} onClick={() => navigate('/agent-graphs')}>
            <ArrowLeft size={18} />
          </IconButton>
          <div>
            <span className="editor-header__eyebrow" data-testid="graph-version">
              {isNew
                ? t('graphs.newEyebrow')
                : t('graphs.graphVersion', { version: graph.revision })}
            </span>
            <h1>{graph.name || t('graphs.editor')}</h1>
          </div>
          {dirty && (
            <span className="dirty-indicator" data-testid="graph-unsaved">
              <span />
              {t('graphs.unsaved')}
            </span>
          )}
        </div>
        <div className="editor-header__actions">
          {isNew && (
            <ManifestImportButton
              label={t('common.importManifest')}
              onImport={(source) => {
                try {
                  loadAgentGraph(manifestToAgentGraph(source, 'AgentGraph'))
                  setDirty(true)
                } catch (error) {
                  window.alert(error instanceof Error ? error.message : String(error))
                }
              }}
            />
          )}
          <Button variant="secondary" size="sm" onClick={() => setPreview((value) => !value)}>
            <Braces size={15} />
            {t('graphs.preview')}
          </Button>
          <Button
            data-testid="graph-editor-save"
            onClick={saveAgentGraph}
            disabled={save.isPending}
          >
            <Save size={15} />
            {t('common.save')}
          </Button>
        </div>
      </header>
      {!isNew && (
        <div className="contract-warning">
          <AlertTriangle size={16} />
          <span>{t('graphs.partialUpdate')}</span>
        </div>
      )}
      {save.isError && <ErrorState error={save.error} />}
      <div className="editor-workspace">
        <aside className="palette">
          <header>
            <span>{t('graphs.palette')}</span>
            <small>{t('graphs.primitives', { count: 12 })}</small>
          </header>
          {paletteGroups.map((group) => (
            <section key={group.label}>
              <h3>{t(`graphs.paletteGroups.${group.label}`)}</h3>
              {group.kinds.map((kind) => {
                const Icon = nodeIcons[kind]
                return (
                  <button
                    type="button"
                    draggable
                    key={kind}
                    data-testid={`graph-node-add-${kind}`}
                    onDragStart={(event) => {
                      event.dataTransfer.setData('application/flowgent-node', kind)
                      event.dataTransfer.effectAllowed = 'move'
                    }}
                    onClick={() => addNode(kind)}
                  >
                    <span className={`kind-icon kind-icon--${kind}`}>
                      <Icon size={15} />
                    </span>
                    <span>{kind}</span>
                    <GripVertical size={13} />
                  </button>
                )
              })}
            </section>
          ))}
        </aside>
        <div
          className="graph-canvas"
          ref={wrapperRef}
          onDragOver={(event) => {
            event.preventDefault()
            event.dataTransfer.dropEffect = 'move'
          }}
          onDrop={onDrop}
        >
          <ReactFlow<EditorNode, CanvasEdge>
            nodes={nodes}
            edges={edges}
            nodeTypes={{ flowgent: GraphNodeCard }}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_, node) => {
              setSelectedNode(node.id)
              setSelectedEdge(null)
            }}
            onEdgeClick={(_, selected) => {
              setSelectedEdge(selected.id)
              setSelectedNode(null)
            }}
            onPaneClick={() => {
              setSelectedNode(null)
              setSelectedEdge(null)
            }}
            fitView
            deleteKeyCode={['Backspace', 'Delete']}
            multiSelectionKeyCode={['Meta', 'Control']}
            selectionOnDrag
            snapToGrid
            snapGrid={[16, 16]}
            minZoom={0.25}
            maxZoom={1.8}
          >
            <Background variant={BackgroundVariant.Dots} gap={18} size={1.2} />
            <MiniMap
              nodeColor={(node) => kindColor((node.data as CanvasData).domain.kind)}
              maskColor="rgba(5,12,11,.72)"
            />
            <Controls showInteractive={false} />
            <Panel position="top-right" className="canvas-toolbar">
              <IconButton label={t('common.undo')} disabled={historyIndex <= 0} onClick={undo}>
                <Undo2 size={16} />
              </IconButton>
              <IconButton
                label={t('common.redo')}
                disabled={historyIndex >= history.length - 1}
                onClick={redo}
              >
                <Redo2 size={16} />
              </IconButton>
              <Button size="sm" variant="secondary" onClick={autoLayout}>
                <WandSparkles size={15} />
                {t('graphs.autoLayout')}
              </Button>
              <button
                type="button"
                className={clsx('validation-pill', blockingIssues.length && 'has-errors')}
                onClick={() => setIssuesVisible((value) => !value)}
              >
                {issues.length ? (
                  <>
                    <AlertTriangle size={14} />
                    {t('graphs.issues', { count: issues.length })}
                  </>
                ) : (
                  <>
                    <Check size={14} />
                    {t('graphs.valid')}
                  </>
                )}
              </button>
            </Panel>
          </ReactFlow>
          {issuesVisible && (
            <div className="validation-popover">
              <header>
                <strong>{t('graphs.validation')}</strong>
                <IconButton label={t('common.close')} onClick={() => setIssuesVisible(false)}>
                  <X size={15} />
                </IconButton>
              </header>
              {issues.length ? (
                <ul>
                  {issues.map((issue, index) => (
                    <li key={`${issue.path}-${index}`}>
                      <AlertTriangle size={14} />
                      <span>{t(`graphValidation.${issue.code}`, issue.params)}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>
                  <Check size={15} />
                  {t('graphs.valid')}
                </p>
              )}
            </div>
          )}
        </div>
        <aside className="inspector">
          <header>
            <span>
              {selected ? selected.id : edge ? t('graphs.edge') : t('graphs.graphSettings')}
            </span>
            {selected && <StatusBadge status={selected.kind.toUpperCase()} />}
          </header>
          {selected ? (
            <NodeInspector
              node={selected}
              onChange={(updates) => updateNode(selected.id, updates)}
            />
          ) : edge ? (
            <EdgeInspector edge={edge} onCondition={updateSelectedEdge} />
          ) : (
            <AgentGraphInspector
              graph={graph}
              existing={!isNew}
              onChange={(updates) => commit({ ...graph, ...updates })}
            />
          )}
        </aside>
      </div>
      {!isNew && graphId && (
        <RecentGraphRuns
          namespace={namespace}
          graphName={graphId}
          runs={recentRuns.data?.items ?? []}
          loading={recentRuns.isLoading}
          error={recentRuns.error}
          onRetry={() => void recentRuns.refetch()}
        />
      )}
      {preview && (
        <div className="preview-panel">
          <header>
            <strong>{t('graphs.preview')}</strong>
            <IconButton label={t('common.close')} onClick={() => setPreview(false)}>
              <X size={16} />
            </IconButton>
          </header>
          <JsonView value={graph} />
        </div>
      )}
    </div>
  )
}

function RecentGraphRuns({
  namespace,
  graphName,
  runs,
  loading,
  error,
  onRetry,
}: {
  namespace: string
  graphName: string
  runs: GraphRun[]
  loading: boolean
  error: unknown
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <section className="surface graph-recent-runs" data-testid="graph-recent-runs">
      <header>
        <div>
          <span className="resource-card__icon resource-card__icon--purple">
            <Activity size={18} />
          </span>
          <div>
            <h2>{t('graphs.recentRuns')}</h2>
            <p>{t('graphs.recentRunsHint')}</p>
          </div>
        </div>
        <Link to={graphRunPath(namespace, graphName)}>
          {t('graphs.viewAllRuns')}
          <ExternalLink size={14} />
        </Link>
      </header>
      {loading ? (
        <LoadingState rows={2} />
      ) : error != null ? (
        <ErrorState error={error} onRetry={onRetry} />
      ) : runs.length === 0 ? (
        <p className="graph-recent-runs__empty">{t('graphs.noRuns')}</p>
      ) : (
        <div className="graph-recent-runs__list">
          {runs.map((run) => (
            <article key={run.id} data-testid={`graph-recent-run-${run.id}`}>
              <div>
                <StatusBadge status={run.status} />
                <code>{run.id}</code>
              </div>
              <dl>
                <div>
                  <dt>{t('common.revision')}</dt>
                  <dd>v{run.agent_graph_revision}</dd>
                </div>
                <div>
                  <dt>{t('runs.duration')}</dt>
                  <dd>{duration(run.started_at, run.finished_at)}</dd>
                </div>
                <div>
                  <dt>{t('common.created')}</dt>
                  <dd>{formatDate(run.created_at)}</dd>
                </div>
              </dl>
              <div className="graph-recent-runs__actions">
                <Link to={graphRunPath(namespace, graphName, run.id)}>{t('common.details')}</Link>
                <Link to={graphRunPath(namespace, graphName, run.id, '/tracking')}>
                  {t('runs.trace')}
                </Link>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}

function GraphNodeCard({ data, selected }: NodeProps<EditorNode>) {
  const { t } = useTranslation()
  const node = data.domain
  const Icon = nodeIcons[node.kind]
  const reference =
    node.agent ||
    node.tool ||
    node.skill ||
    node.subgraph?.ref ||
    node.expression ||
    node.runtime ||
    t('graphs.configureNode')
  return (
    <div className={clsx('graph-node', `graph-node--${node.kind}`, selected && 'is-selected')}>
      <Handle type="target" position={Position.Left} />
      <div className="graph-node__top">
        <span className={`kind-icon kind-icon--${node.kind}`}>
          <Icon size={16} />
        </span>
        <span className="graph-node__kind">{node.kind}</span>
        <span className="graph-node__signal" />
      </div>
      <strong>{node.id}</strong>
      <small>{reference}</small>
      <Handle type="source" position={Position.Right} />
    </div>
  )
}

function AgentGraphInspector({
  graph,
  existing,
  onChange,
}: {
  graph: AgentGraph
  existing: boolean
  onChange: (updates: Partial<AgentGraph>) => void
}) {
  const { t } = useTranslation()
  const setResource = (
    role: 'jobmanager' | 'taskmanager' | 'sandbox',
    field: 'cpu' | 'memory',
    value: string,
  ) =>
    onChange({
      resources: {
        ...(graph.resources ?? {}),
        [role]: {
          ...(graph.resources?.[role] ?? {}),
          [field]: value || undefined,
        },
      },
    })
  return (
    <div className="inspector-form">
      <label>
        <span>{t('graphs.graphId')}</span>
        <input
          data-testid="graph-id"
          value={graph.name}
          disabled={existing}
          maxLength={RESOURCE_NAME_MAX_LENGTH}
          pattern={RESOURCE_NAME_PATTERN_SOURCE}
          onChange={(event) =>
            onChange({ name: event.target.value.replace(/[^a-zA-Z0-9-_]/g, '') })
          }
          placeholder="my-agentgraph"
        />
        {!existing && <small>{t('graphs.nameHint')}</small>}
      </label>
      <label>
        <span>{t('graphs.summary')}</span>
        <input
          data-testid="graph-summary"
          value={graph.summary ?? ''}
          disabled={existing}
          onChange={(event) => onChange({ summary: event.target.value })}
        />
      </label>
      <label>
        <span>{t('common.description')}</span>
        <textarea
          data-testid="graph-description"
          rows={5}
          value={graph.description ?? ''}
          onChange={(event) => onChange({ description: event.target.value })}
        />
      </label>
      <label>
        <span>{t('graphs.runtimeMode', { defaultValue: 'Runtime mode' })}</span>
        <select
          data-testid="graph-runtime-mode"
          value={graph.runtime_mode}
          onChange={(event) => {
            const runtimeMode = event.target.value as AgentGraph['runtime_mode']
            onChange({
              runtime_mode: runtimeMode,
              ...(runtimeMode === 'session' ? { resources: undefined } : {}),
            })
          }}
          required
        >
          <option value="application">
            {t('graphs.runtimeModeApplication', { defaultValue: 'Application' })}
          </option>
          <option value="session">
            {t('graphs.runtimeModeSession', { defaultValue: 'Session' })}
          </option>
        </select>
        <small>
          {t('graphs.runtimeModeHint', {
            defaultValue:
              'Application creates an isolated runtime cluster per run. Session uses the Helm-deployed shared runtime cluster.',
          })}
        </small>
      </label>
      {graph.runtime_mode === 'application' && (
        <fieldset className="form-grid">
          <legend>
            {t('graphs.applicationResources', { defaultValue: 'Application pod resources' })}
          </legend>
          <RuntimeResourceFields
            label={t('graphs.jobManager', { defaultValue: 'JobManager' })}
            cpu={graph.resources?.jobmanager?.cpu ?? ''}
            memory={graph.resources?.jobmanager?.memory ?? ''}
            onCpu={(value) => setResource('jobmanager', 'cpu', value)}
            onMemory={(value) => setResource('jobmanager', 'memory', value)}
          />
          <RuntimeResourceFields
            label={t('graphs.taskManager', { defaultValue: 'TaskManager' })}
            cpu={graph.resources?.taskmanager?.cpu ?? ''}
            memory={graph.resources?.taskmanager?.memory ?? ''}
            onCpu={(value) => setResource('taskmanager', 'cpu', value)}
            onMemory={(value) => setResource('taskmanager', 'memory', value)}
          />
          <RuntimeResourceFields
            label={t('graphs.sandbox', { defaultValue: 'Sandbox' })}
            cpu={graph.resources?.sandbox?.cpu ?? ''}
            memory={graph.resources?.sandbox?.memory ?? ''}
            onCpu={(value) => setResource('sandbox', 'cpu', value)}
            onMemory={(value) => setResource('sandbox', 'memory', value)}
          />
          <small>
            {t('graphs.applicationResourcesHint', {
              defaultValue:
                'Only pod size is graph-scoped. Replica counts and slots stay controlled by platform defaults.',
            })}
          </small>
        </fieldset>
      )}
      <JsonInput
        label={t('runs.variables')}
        value={graph.vars ?? {}}
        onChange={(vars) => onChange({ vars })}
      />
      <JsonInput
        label={t('graphs.labels')}
        value={graph.labels ?? {}}
        disabled={existing}
        onChange={(labels) => onChange({ labels: labels as Record<string, string> })}
      />
    </div>
  )
}

function RuntimeResourceFields({
  label,
  cpu,
  memory,
  onCpu,
  onMemory,
}: {
  label: string
  cpu: string
  memory: string
  onCpu: (value: string) => void
  onMemory: (value: string) => void
}) {
  return (
    <div className="resource-fields">
      <span>{label}</span>
      <input value={cpu} onChange={(event) => onCpu(event.target.value)} placeholder="500m" />
      <input
        value={memory}
        onChange={(event) => onMemory(event.target.value)}
        placeholder="512Mi"
      />
    </div>
  )
}

function NodeInspector({
  node,
  onChange,
}: {
  node: GraphNode
  onChange: (updates: Partial<GraphNode>) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="inspector-form">
      <label>
        <span>{t('graphs.nodeId')}</span>
        <input
          data-testid="graph-node-id"
          value={node.id}
          onChange={(event) => onChange({ id: event.target.value.replace(/[^a-zA-Z0-9-_]/g, '') })}
        />
      </label>
      <label>
        <span>{t('graphs.kind')}</span>
        <input value={node.kind} disabled />
      </label>
      {node.kind === 'agent' && (
        <TextInput
          label={t('graphs.agentReference')}
          value={node.agent}
          onChange={(agent) => onChange({ agent })}
        />
      )}
      {node.kind === 'tool' && (
        <TextInput
          label={t('graphs.toolReference')}
          value={node.tool}
          onChange={(tool) => onChange({ tool })}
        />
      )}
      {node.kind === 'skill' && (
        <TextInput
          label={t('graphs.skillReference')}
          value={node.skill}
          onChange={(skill) => onChange({ skill })}
        />
      )}
      {node.kind === 'subgraph' && (
        <TextInput
          label={t('graphs.subgraphId')}
          value={node.subgraph?.ref}
          onChange={(ref) => onChange({ subgraph: { ...node.subgraph, ref } })}
        />
      )}
      {node.kind === 'condition' && (
        <TextInput
          label={t('graphs.expression')}
          value={node.expression}
          onChange={(expression) => onChange({ expression })}
        />
      )}
      {node.kind === 'sandbox' && (
        <>
          <label>
            <span>{t('graphs.runtime')}</span>
            <select
              value={node.runtime ?? 'bash'}
              onChange={(event) => onChange({ runtime: event.target.value })}
            >
              <option>bash</option>
              <option>python3</option>
              <option>node</option>
            </select>
          </label>
          <label>
            <span>{t('graphs.script')}</span>
            <textarea
              data-testid="graph-node-script"
              className="code-input"
              rows={8}
              value={node.script ?? ''}
              onChange={(event) => onChange({ script: event.target.value })}
            />
          </label>
          <TextInput
            label={t('graphs.timeout')}
            value={node.timeout}
            onChange={(timeout) => onChange({ timeout })}
          />
          <TextInput
            label={t('graphs.workspace')}
            value={node.workspace}
            onChange={(workspace) => onChange({ workspace })}
          />
          <JsonInput
            label={t('graphs.networkPolicy')}
            value={node.network_policy ?? {}}
            onChange={(network_policy) => onChange({ network_policy })}
          />
        </>
      )}
      {node.kind === 'human' && (
        <>
          <TextInput
            label={t('graphs.approvalTimeout')}
            value={node.approval?.timeout}
            onChange={(timeout) => onChange({ approval: { ...node.approval, timeout } })}
          />
          <TextInput
            label={t('graphs.onApprove')}
            value={node.approval?.on_approve}
            onChange={(on_approve) => onChange({ approval: { ...node.approval, on_approve } })}
          />
          <TextInput
            label={t('graphs.onReject')}
            value={node.approval?.on_reject}
            onChange={(on_reject) => onChange({ approval: { ...node.approval, on_reject } })}
          />
        </>
      )}
      {node.kind === 'supervisor' && (
        <JsonInput
          label={t('graphs.supervisorLimits')}
          value={
            node.supervisor_config ?? {
              max_retries: 2,
              max_nodes: 4,
              max_injections: 2,
              allowed_actions: ['continue', 'retry', 'inject', 'abort'],
            }
          }
          onChange={(supervisor_config) => onChange({ supervisor_config })}
        />
      )}
      {node.kind === 'map' && (
        <JsonInput
          label={t('graphs.nestedNode')}
          value={(node.node ?? { id: 'item', kind: 'noop' }) as unknown as Record<string, unknown>}
          onChange={(value) => onChange({ node: value as unknown as GraphNode })}
        />
      )}
      <label>
        <span>{t('agents.instruction')}</span>
        <textarea
          rows={5}
          value={node.instruction ?? ''}
          onChange={(event) => onChange({ instruction: event.target.value })}
        />
      </label>
      <JsonInput
        label={t('graphs.inputArgs')}
        value={node.args ?? {}}
        onChange={(args) => onChange({ args })}
      />
      <JsonInput
        label={t('agents.schema')}
        value={node.output_schema ?? {}}
        onChange={(output_schema) => onChange({ output_schema })}
      />
      <JsonInput
        label={t('graphs.retryPolicy')}
        value={node.retry ?? {}}
        onChange={(retry) => onChange({ retry: retry as GraphNode['retry'] })}
      />
    </div>
  )
}

function EdgeInspector({
  edge,
  onCondition,
}: {
  edge: CanvasEdge
  onCondition: (value: '' | 'true' | 'false') => void
}) {
  const { t } = useTranslation()
  return (
    <div className="inspector-form">
      <label>
        <span>{t('common.source')}</span>
        <input value={edge.source} disabled />
      </label>
      <label>
        <span>{t('graphs.target')}</span>
        <input value={edge.target} disabled />
      </label>
      <label>
        <span>{t('graphs.conditionBranch')}</span>
        <select
          value={edge.label === undefined ? '' : String(edge.label)}
          onChange={(event) => onCondition(event.target.value as '' | 'true' | 'false')}
        >
          <option value="">{t('graphs.unconditional')}</option>
          <option value="true">{t('graphs.trueBranch')}</option>
          <option value="false">{t('graphs.falseBranch')}</option>
        </select>
        <small>{t('graphs.conditionHint')}</small>
      </label>
    </div>
  )
}

function TextInput({
  label,
  value = '',
  onChange,
}: {
  label: string
  value?: string
  onChange: (value: string) => void
}) {
  return (
    <label>
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function JsonInput({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [text, setText] = useState(() => JSON.stringify(value, null, 2))
  const [invalid, setInvalid] = useState(false)
  useEffect(() => {
    // Keep the editor synchronized when the selected domain object changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setText(JSON.stringify(value, null, 2))
  }, [value])
  return (
    <label>
      <span>{label}</span>
      <textarea
        className={clsx('code-input', invalid && 'is-invalid')}
        rows={6}
        value={text}
        disabled={disabled}
        onChange={(event) => {
          const next = event.target.value
          setText(next)
          try {
            const parsed = JSON.parse(next) as Record<string, unknown>
            setInvalid(false)
            onChange(parsed)
          } catch {
            setInvalid(true)
          }
        }}
      />
      {invalid && <small className="field__error">{t('errors.invalidJson')}</small>}
    </label>
  )
}

function autoLayoutNodes(nodes: EditorNode[], edges: GraphEdge[]): EditorNode[] {
  const level = graphLayers(
    nodes.map((node) => node.id),
    edges,
  )
  const rows = new Map<number, string[]>()
  nodes.forEach((node) =>
    rows.set(level.get(node.id) ?? 0, [...(rows.get(level.get(node.id) ?? 0) ?? []), node.id]),
  )
  return nodes.map((node) => {
    const column = level.get(node.id) ?? 0
    const row = rows.get(column)?.indexOf(node.id) ?? 0
    return { ...node, position: { x: 80 + column * 270, y: 70 + row * 150 } }
  })
}

function kindColor(kind: NodeKind) {
  return (
    {
      agent: '#43d6a2',
      supervisor: '#d18cff',
      committee: '#9a8cff',
      tool: '#6f9cff',
      skill: '#d5a84b',
      sandbox: '#ff916f',
      condition: '#e8c866',
      human: '#ff7fa7',
      map: '#63c7d9',
      subgraph: '#6dd4c1',
      join: '#8aa29c',
      noop: '#758984',
    } as Record<NodeKind, string>
  )[kind]
}
