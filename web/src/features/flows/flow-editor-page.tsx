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
  X,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowPath } from '../../app/paths'
import {
  Button,
  ErrorState,
  IconButton,
  JsonView,
  LoadingState,
  StatusBadge,
} from '../../shared/components/ui'
import type { Flow, FlowEdge, FlowNode, NodeKind } from '../../core/domain/types'
import {
  RESOURCE_NAME_MAX_LENGTH,
  RESOURCE_NAME_PATTERN_SOURCE,
} from '../../core/domain/resource-name'
import { nodeKinds } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { validateFlow } from './flow-validation'
import { graphLayers } from './graph-layout'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToFlow } from '../resources/manifest'

interface CanvasData extends Record<string, unknown> {
  domain: FlowNode
}
type EditorNode = CanvasNode<CanvasData, 'flowgent'>

const nodeIcons: Record<NodeKind, React.ComponentType<{ size?: number }>> = {
  agent: Bot,
  tool: Box,
  skill: Sparkles,
  map: Workflow,
  agentflow: Waypoints,
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
  { label: 'composition', kinds: ['map', 'agentflow'] },
]

const emptyFlow = (namespace: string): Flow => ({
  id: '',
  description: '',
  summary: '',
  namespace_id: namespace,
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
  kind: 'flow',
  runtime_mode: 'application',
  nodes: [],
  edges: [],
  vars: {},
  labels: {},
  triggers: [],
})

function toCanvasNodes(nodes: FlowNode[]): EditorNode[] {
  return nodes.map((node, index) => ({
    id: node.id,
    type: 'flowgent',
    position: { x: 80 + (index % 3) * 270, y: 70 + Math.floor(index / 3) * 170 },
    data: { domain: node },
  }))
}

function toCanvasEdges(edges: FlowEdge[]): CanvasEdge[] {
  return edges.map((edge, index) => ({
    id: `edge-${index}-${edge.from}-${edge.to}`,
    source: edge.from,
    target: edge.to,
    label: edge.condition === undefined ? undefined : String(edge.condition),
    markerEnd: { type: MarkerType.ArrowClosed },
    className:
      edge.condition === false
        ? 'flow-edge--false'
        : edge.condition === true
          ? 'flow-edge--true'
          : '',
  }))
}

export function FlowEditorPage() {
  return (
    <ReactFlowProvider>
      <FlowEditor />
    </ReactFlowProvider>
  )
}

function FlowEditor() {
  const { t } = useTranslation()
  const { flowId } = useParams()
  const isNew = !flowId
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: [namespace, 'flows', flowId],
    queryFn: ({ signal }) => repositories.flows.get(namespace, flowId ?? '', signal),
    enabled: !isNew,
  })
  const existingFlows = useQuery({
    queryKey: [namespace, 'flows'],
    queryFn: ({ signal }) => repositories.flows.list(namespace, signal),
    enabled: isNew,
  })
  const [flow, setFlow] = useState<Flow>(() => emptyFlow(namespace))
  const [nodes, setNodes] = useState<EditorNode[]>([])
  const [edges, setEdges] = useState<CanvasEdge[]>([])
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [preview, setPreview] = useState(false)
  const [issuesVisible, setIssuesVisible] = useState(false)
  const [history, setHistory] = useState<Flow[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const { screenToFlowPosition, fitView } = useReactFlow()
  const issues = useMemo(
    () => validateFlow(flow, isNew ? (existingFlows.data ?? []).map((item) => item.id) : []),
    [existingFlows.data, flow, isNew],
  )
  const blockingIssues = useMemo(() => issues.filter((issue) => issue.code !== 'cycle'), [issues])

  const loadFlow = useCallback((value: Flow) => {
    setFlow(value)
    setNodes(autoLayoutNodes(toCanvasNodes(value.nodes), value.edges))
    setEdges(toCanvasEdges(value.edges))
    setHistory([structuredClone(value)])
    setHistoryIndex(0)
    setDirty(false)
  }, [])

  useEffect(() => {
    // Loading an addressable backend resource initializes the editor session.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (query.data) loadFlow(query.data)
  }, [loadFlow, query.data])
  useEffect(() => {
    // A new route initializes a fresh canonical graph exactly once.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (isNew && historyIndex < 0) loadFlow(emptyFlow(namespace))
  }, [historyIndex, isNew, loadFlow, namespace])
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault()
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [dirty])

  const commit = useCallback(
    (next: Flow, record = true) => {
      setFlow(next)
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
          ...flow,
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
    [commit, flow],
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
      while (flow.nodes.some((node) => node.id === id)) id = `${baseId}-${suffix++}`
      const domain: FlowNode = {
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
    [edges, flow.nodes, nodes, syncGraph],
  )

  const onDrop = (event: React.DragEvent) => {
    event.preventDefault()
    const kind = event.dataTransfer.getData('application/flowgent-node') as NodeKind
    if (!nodeKinds.includes(kind)) return
    addNode(kind, screenToFlowPosition({ x: event.clientX, y: event.clientY }))
  }

  const updateNode = (id: string, updates: Partial<FlowNode>) => {
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
            className: condition ? `flow-edge--${condition}` : '',
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
    setFlow(structuredClone(snapshot))
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
    setFlow(structuredClone(snapshot))
    setNodes(autoLayoutNodes(toCanvasNodes(snapshot.nodes), snapshot.edges))
    setEdges(toCanvasEdges(snapshot.edges))
    setDirty(true)
  }, [history, historyIndex])
  const autoLayout = () => {
    const next = autoLayoutNodes(nodes, flow.edges)
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
        const selected = flow.nodes.find((node) => node.id === selectedNode)
        if (selected) sessionStorage.setItem('flowgent-node-clipboard', JSON.stringify(selected))
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'v') {
        const raw = sessionStorage.getItem('flowgent-node-clipboard')
        if (raw) {
          const copied = JSON.parse(raw) as FlowNode
          addNode(copied.kind)
        }
      }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [addNode, flow.nodes, redo, selectedNode, undo])

  const save = useMutation({
    mutationFn: () => repositories.flows.save(namespace, flow, isNew),
    onSuccess: (saved) => {
      void queryClient.invalidateQueries({ queryKey: [namespace, 'flows'] })
      loadFlow(saved)
      if (isNew) navigate(flowPath(namespace, saved.id), { replace: true })
    },
  })
  const saveFlow = () => {
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
          <IconButton label={t('common.back')} onClick={() => navigate('/flows')}>
            <ArrowLeft size={18} />
          </IconButton>
          <div>
            <span className="editor-header__eyebrow">
              {isNew
                ? t('flows.newEyebrow')
                : t('flows.flowVersion', { version: flow.version ?? 1 })}
            </span>
            <h1>{flow.id || t('flows.editor')}</h1>
          </div>
          {dirty && (
            <span className="dirty-indicator">
              <span />
              {t('flows.unsaved')}
            </span>
          )}
        </div>
        <div className="editor-header__actions">
          {isNew && (
            <ManifestImportButton
              label={t('common.importManifest')}
              onImport={(source) => {
                try {
                  loadFlow(manifestToFlow(source, 'Flow'))
                  setDirty(true)
                } catch (error) {
                  window.alert(error instanceof Error ? error.message : String(error))
                }
              }}
            />
          )}
          <Button variant="secondary" size="sm" onClick={() => setPreview((value) => !value)}>
            <Braces size={15} />
            {t('flows.preview')}
          </Button>
          <Button onClick={saveFlow} disabled={save.isPending}>
            <Save size={15} />
            {t('common.save')}
          </Button>
        </div>
      </header>
      {!isNew && (
        <div className="contract-warning">
          <AlertTriangle size={16} />
          <span>{t('flows.partialUpdate')}</span>
        </div>
      )}
      {save.isError && <ErrorState error={save.error} />}
      <div className="editor-workspace">
        <aside className="palette">
          <header>
            <span>{t('flows.palette')}</span>
            <small>{t('flows.primitives', { count: 12 })}</small>
          </header>
          {paletteGroups.map((group) => (
            <section key={group.label}>
              <h3>{t(`flows.paletteGroups.${group.label}`)}</h3>
              {group.kinds.map((kind) => {
                const Icon = nodeIcons[kind]
                return (
                  <button
                    type="button"
                    draggable
                    key={kind}
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
          className="flow-canvas"
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
            nodeTypes={{ flowgent: FlowNodeCard }}
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
                {t('flows.autoLayout')}
              </Button>
              <button
                type="button"
                className={clsx('validation-pill', blockingIssues.length && 'has-errors')}
                onClick={() => setIssuesVisible((value) => !value)}
              >
                {issues.length ? (
                  <>
                    <AlertTriangle size={14} />
                    {t('flows.issues', { count: issues.length })}
                  </>
                ) : (
                  <>
                    <Check size={14} />
                    {t('flows.valid')}
                  </>
                )}
              </button>
            </Panel>
          </ReactFlow>
          {issuesVisible && (
            <div className="validation-popover">
              <header>
                <strong>{t('flows.validation')}</strong>
                <IconButton label={t('common.close')} onClick={() => setIssuesVisible(false)}>
                  <X size={15} />
                </IconButton>
              </header>
              {issues.length ? (
                <ul>
                  {issues.map((issue, index) => (
                    <li key={`${issue.path}-${index}`}>
                      <AlertTriangle size={14} />
                      <span>{t(`flowValidation.${issue.code}`, issue.params)}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>
                  <Check size={15} />
                  {t('flows.valid')}
                </p>
              )}
            </div>
          )}
        </div>
        <aside className="inspector">
          <header>
            <span>{selected ? selected.id : edge ? t('flows.edge') : t('flows.flowSettings')}</span>
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
            <FlowInspector
              flow={flow}
              existing={!isNew}
              onChange={(updates) => commit({ ...flow, ...updates })}
            />
          )}
        </aside>
      </div>
      {preview && (
        <div className="preview-panel">
          <header>
            <strong>{t('flows.preview')}</strong>
            <IconButton label={t('common.close')} onClick={() => setPreview(false)}>
              <X size={16} />
            </IconButton>
          </header>
          <JsonView value={flow} />
        </div>
      )}
    </div>
  )
}

function FlowNodeCard({ data, selected }: NodeProps<EditorNode>) {
  const { t } = useTranslation()
  const node = data.domain
  const Icon = nodeIcons[node.kind]
  const reference =
    node.agent ||
    node.tool ||
    node.skill ||
    node.agentflow ||
    node.expression ||
    node.runtime ||
    t('flows.configureNode')
  return (
    <div className={clsx('flow-node', `flow-node--${node.kind}`, selected && 'is-selected')}>
      <Handle type="target" position={Position.Left} />
      <div className="flow-node__top">
        <span className={`kind-icon kind-icon--${node.kind}`}>
          <Icon size={16} />
        </span>
        <span className="flow-node__kind">{node.kind}</span>
        <span className="flow-node__signal" />
      </div>
      <strong>{node.id}</strong>
      <small>{reference}</small>
      <Handle type="source" position={Position.Right} />
    </div>
  )
}

function FlowInspector({
  flow,
  existing,
  onChange,
}: {
  flow: Flow
  existing: boolean
  onChange: (updates: Partial<Flow>) => void
}) {
  const { t } = useTranslation()
  const setResource = (
    role: 'jobmanager' | 'taskmanager' | 'sandbox',
    field: 'cpu' | 'memory',
    value: string,
  ) =>
    onChange({
      resources: {
        ...(flow.resources ?? {}),
        [role]: {
          ...(flow.resources?.[role] ?? {}),
          [field]: value || undefined,
        },
      },
    })
  return (
    <div className="inspector-form">
      <label>
        <span>{t('flows.flowId')}</span>
        <input
          value={flow.id}
          disabled={existing}
          maxLength={RESOURCE_NAME_MAX_LENGTH}
          pattern={RESOURCE_NAME_PATTERN_SOURCE}
          onChange={(event) => onChange({ id: event.target.value.replace(/[^a-zA-Z0-9-_]/g, '') })}
          placeholder="my-agentflow"
        />
        {!existing && <small>{t('flows.nameHint')}</small>}
      </label>
      <label>
        <span>{t('flows.summary')}</span>
        <input
          value={flow.summary ?? ''}
          disabled={existing}
          onChange={(event) => onChange({ summary: event.target.value })}
        />
      </label>
      <label>
        <span>{t('common.description')}</span>
        <textarea
          rows={5}
          value={flow.description ?? ''}
          onChange={(event) => onChange({ description: event.target.value })}
        />
      </label>
      <label>
        <span>{t('flows.runtimeMode', { defaultValue: 'Runtime mode' })}</span>
        <select
          value={flow.runtime_mode}
          onChange={(event) => {
            const runtimeMode = event.target.value as Flow['runtime_mode']
            onChange({
              runtime_mode: runtimeMode,
              ...(runtimeMode === 'session' ? { resources: undefined } : {}),
            })
          }}
          required
        >
          <option value="application">
            {t('flows.runtimeModeApplication', { defaultValue: 'Application' })}
          </option>
          <option value="session">
            {t('flows.runtimeModeSession', { defaultValue: 'Session' })}
          </option>
        </select>
        <small>
          {t('flows.runtimeModeHint', {
            defaultValue:
              'Application creates an isolated runtime cluster per run. Session uses the Helm-deployed shared runtime cluster.',
          })}
        </small>
      </label>
      {flow.runtime_mode === 'application' && (
        <fieldset className="form-grid">
          <legend>
            {t('flows.applicationResources', { defaultValue: 'Application pod resources' })}
          </legend>
          <RuntimeResourceFields
            label={t('flows.jobManager', { defaultValue: 'JobManager' })}
            cpu={flow.resources?.jobmanager?.cpu ?? ''}
            memory={flow.resources?.jobmanager?.memory ?? ''}
            onCpu={(value) => setResource('jobmanager', 'cpu', value)}
            onMemory={(value) => setResource('jobmanager', 'memory', value)}
          />
          <RuntimeResourceFields
            label={t('flows.taskManager', { defaultValue: 'TaskManager' })}
            cpu={flow.resources?.taskmanager?.cpu ?? ''}
            memory={flow.resources?.taskmanager?.memory ?? ''}
            onCpu={(value) => setResource('taskmanager', 'cpu', value)}
            onMemory={(value) => setResource('taskmanager', 'memory', value)}
          />
          <RuntimeResourceFields
            label={t('flows.sandbox', { defaultValue: 'Sandbox' })}
            cpu={flow.resources?.sandbox?.cpu ?? ''}
            memory={flow.resources?.sandbox?.memory ?? ''}
            onCpu={(value) => setResource('sandbox', 'cpu', value)}
            onMemory={(value) => setResource('sandbox', 'memory', value)}
          />
          <small>
            {t('flows.applicationResourcesHint', {
              defaultValue:
                'Only pod size is flow-scoped. Replica counts and slots stay controlled by platform defaults.',
            })}
          </small>
        </fieldset>
      )}
      <JsonInput
        label={t('runs.variables')}
        value={flow.vars ?? {}}
        onChange={(vars) => onChange({ vars })}
      />
      <JsonInput
        label={t('flows.labels')}
        value={flow.labels ?? {}}
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
  node: FlowNode
  onChange: (updates: Partial<FlowNode>) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="inspector-form">
      <label>
        <span>{t('flows.nodeId')}</span>
        <input
          value={node.id}
          onChange={(event) => onChange({ id: event.target.value.replace(/[^a-zA-Z0-9-_]/g, '') })}
        />
      </label>
      <label>
        <span>{t('flows.kind')}</span>
        <input value={node.kind} disabled />
      </label>
      {node.kind === 'agent' && (
        <TextInput
          label={t('flows.agentReference')}
          value={node.agent}
          onChange={(agent) => onChange({ agent })}
        />
      )}
      {node.kind === 'tool' && (
        <TextInput
          label={t('flows.toolReference')}
          value={node.tool}
          onChange={(tool) => onChange({ tool })}
        />
      )}
      {node.kind === 'skill' && (
        <TextInput
          label={t('flows.skillReference')}
          value={node.skill}
          onChange={(skill) => onChange({ skill })}
        />
      )}
      {node.kind === 'agentflow' && (
        <TextInput
          label={t('flows.subflowId')}
          value={node.agentflow}
          onChange={(agentflow) => onChange({ agentflow })}
        />
      )}
      {node.kind === 'condition' && (
        <TextInput
          label={t('flows.expression')}
          value={node.expression}
          onChange={(expression) => onChange({ expression })}
        />
      )}
      {node.kind === 'sandbox' && (
        <>
          <label>
            <span>{t('flows.runtime')}</span>
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
            <span>{t('flows.script')}</span>
            <textarea
              className="code-input"
              rows={8}
              value={node.script ?? ''}
              onChange={(event) => onChange({ script: event.target.value })}
            />
          </label>
          <TextInput
            label={t('flows.timeout')}
            value={node.timeout}
            onChange={(timeout) => onChange({ timeout })}
          />
          <TextInput
            label={t('flows.workspace')}
            value={node.workspace}
            onChange={(workspace) => onChange({ workspace })}
          />
          <JsonInput
            label={t('flows.networkPolicy')}
            value={node.network_policy ?? {}}
            onChange={(network_policy) => onChange({ network_policy })}
          />
        </>
      )}
      {node.kind === 'human' && (
        <>
          <TextInput
            label={t('flows.approvalTimeout')}
            value={node.approval?.timeout}
            onChange={(timeout) => onChange({ approval: { ...node.approval, timeout } })}
          />
          <TextInput
            label={t('flows.onApprove')}
            value={node.approval?.on_approve}
            onChange={(on_approve) => onChange({ approval: { ...node.approval, on_approve } })}
          />
          <TextInput
            label={t('flows.onReject')}
            value={node.approval?.on_reject}
            onChange={(on_reject) => onChange({ approval: { ...node.approval, on_reject } })}
          />
        </>
      )}
      {node.kind === 'supervisor' && (
        <JsonInput
          label={t('flows.supervisorLimits')}
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
          label={t('flows.nestedNode')}
          value={(node.node ?? { id: 'item', kind: 'noop' }) as unknown as Record<string, unknown>}
          onChange={(value) => onChange({ node: value as unknown as FlowNode })}
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
        label={t('flows.inputArgs')}
        value={node.args ?? {}}
        onChange={(args) => onChange({ args })}
      />
      <JsonInput
        label={t('agents.schema')}
        value={node.output_schema ?? {}}
        onChange={(output_schema) => onChange({ output_schema })}
      />
      <JsonInput
        label={t('flows.retryPolicy')}
        value={node.retry ?? {}}
        onChange={(retry) => onChange({ retry: retry as FlowNode['retry'] })}
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
        <span>{t('flows.target')}</span>
        <input value={edge.target} disabled />
      </label>
      <label>
        <span>{t('flows.conditionBranch')}</span>
        <select
          value={edge.label === undefined ? '' : String(edge.label)}
          onChange={(event) => onCondition(event.target.value as '' | 'true' | 'false')}
        >
          <option value="">{t('flows.unconditional')}</option>
          <option value="true">{t('flows.trueBranch')}</option>
          <option value="false">{t('flows.falseBranch')}</option>
        </select>
        <small>{t('flows.conditionHint')}</small>
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

function autoLayoutNodes(nodes: EditorNode[], edges: FlowEdge[]): EditorNode[] {
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
      agentflow: '#6dd4c1',
      join: '#8aa29c',
      noop: '#758984',
    } as Record<NodeKind, string>
  )[kind]
}
