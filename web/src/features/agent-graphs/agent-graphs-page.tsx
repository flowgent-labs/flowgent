import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNowStrict } from 'date-fns'
import { enUS, zhCN } from 'date-fns/locale'
import { Copy, GitBranch, MoreHorizontal, Play, Plus, Search, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { graphPath, namespacePathname } from '../../app/paths'
import {
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  LoadingState,
  Modal,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { AgentGraph } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

export function AgentGraphsPage() {
  const { t, i18n } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [triggerAgentGraph, setTriggerAgentGraph] = useState<AgentGraph | null>(null)
  const [deleteAgentGraph, setDeleteAgentGraph] = useState<AgentGraph | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'graphs'],
    queryFn: ({ signal }) => repositories.graphs.list(namespace, signal),
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.graphs.remove(namespace, id),
    onSuccess: () => {
      setDeleteAgentGraph(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'graphs'] })
    },
  })
  const duplicate = useMutation({
    mutationFn: (graph: AgentGraph) =>
      repositories.graphs.save(
        namespace,
        {
          ...structuredClone(graph),
          id: '',
          name: `${graph.name}-copy`,
          summary: `${graph.summary ?? graph.name} (copy)`,
          revision: 1,
          created_at: '',
          updated_at: '',
        },
        true,
      ),
    onSuccess: (graph) => {
      void queryClient.invalidateQueries({ queryKey: [namespace, 'graphs'] })
      navigate(graphPath(namespace, graph.name))
    },
  })
  const filtered = useMemo(
    () =>
      query.data?.filter((graph) =>
        `${graph.name} ${graph.id} ${graph.summary} ${graph.description} ${Object.values(graph.labels ?? {}).join(' ')}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('graphs.eyebrow')}
        title={t('graphs.title')}
        description={t('graphs.subtitle')}
        actions={
          <Button onClick={() => navigate(namespacePathname(namespace, '/agent-graphs/new'))}>
            <Plus size={16} />
            {t('graphs.new')}
          </Button>
        }
      />
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('graphs.search')}
          />
        </label>
        <div className="toolbar__end">
          <span className="result-count">{t('graphs.graphCount', { count: filtered.length })}</span>
        </div>
      </div>
      {query.isLoading ? (
        <LoadingState rows={5} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : filtered.length === 0 ? (
        <EmptyState
          title={search ? t('common.noData') : t('graphs.createFirst')}
          action={
            !search && (
              <Button onClick={() => navigate(namespacePathname(namespace, '/agent-graphs/new'))}>
                <Plus size={16} />
                {t('graphs.new')}
              </Button>
            )
          }
        />
      ) : (
        <section className="surface table-surface">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('runs.graph')}</th>
                  <th>{t('graphs.nodes')}</th>
                  <th>{t('graphs.trigger')}</th>
                  <th>{t('common.status')}</th>
                  <th>{t('common.updated')}</th>
                  <th aria-label={t('common.actions')} />
                </tr>
              </thead>
              <tbody>
                {filtered.map((graph) => (
                  <tr key={graph.id} data-testid={`graph-row-${graph.name}`}>
                    <td>
                      <button
                        type="button"
                        className="resource-cell"
                        onClick={() => navigate(graphPath(namespace, graph.name))}
                      >
                        <span className="resource-icon">
                          <GitBranch size={17} />
                        </span>
                        <span>
                          <strong>{graph.name}</strong>
                          <small>{graph.summary || graph.description || '—'}</small>
                        </span>
                      </button>
                    </td>
                    <td>
                      <strong>{graph.nodes.length}</strong>
                      <small className="cell-subtitle">
                        {t('graphs.edgeCount', { count: graph.edges.length })}
                      </small>
                    </td>
                    <td>
                      <span className="trigger-label">{graph.triggers?.[0]?.type ?? 'manual'}</span>
                    </td>
                    <td>
                      <StatusBadge status={graph.status || 'ACTIVE'} />
                    </td>
                    <td>
                      <span title={graph.updated_at}>
                        {formatRelative(graph.updated_at, i18n.language)}
                      </span>
                      <small className="cell-subtitle">v{graph.revision}</small>
                    </td>
                    <td className="row-actions">
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => setTriggerAgentGraph(graph)}
                      >
                        <Play size={14} />
                        {t('common.run')}
                      </Button>
                      <div className="more-menu">
                        <IconButton
                          data-testid={`graph-actions-${graph.name}`}
                          label={t('common.actions')}
                          onClick={() => setMenu(menu === graph.name ? null : graph.name)}
                        >
                          <MoreHorizontal size={17} />
                        </IconButton>
                        {menu === graph.name && (
                          <div className="more-menu__popover">
                            <button
                              type="button"
                              onClick={() =>
                                navigate(graphPath(namespace, graph.name, '/graph-runs'))
                              }
                            >
                              {t('common.runs')}
                            </button>
                            <button type="button" onClick={() => duplicate.mutate(graph)}>
                              <Copy size={14} />
                              {t('graphs.duplicate')}
                            </button>
                            <button
                              type="button"
                              className="danger"
                              data-testid={`graph-delete-${graph.name}`}
                              onClick={() => {
                                setDeleteAgentGraph(graph)
                                setMenu(null)
                              }}
                            >
                              <Trash2 size={14} />
                              {t('common.delete')}
                            </button>
                          </div>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      <TriggerModal graph={triggerAgentGraph} onClose={() => setTriggerAgentGraph(null)} />
      <Modal
        open={Boolean(deleteAgentGraph)}
        title={`${t('common.delete')} ${deleteAgentGraph?.name ?? ''}?`}
        onClose={() => setDeleteAgentGraph(null)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeleteAgentGraph(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              data-testid="graph-delete-confirm"
              variant="danger"
              disabled={remove.isPending}
              onClick={() => deleteAgentGraph && remove.mutate(deleteAgentGraph.name)}
            >
              {t('common.delete')}
            </Button>
          </>
        }
      >
        <p>{t('common.confirmDelete')}</p>
        <p className="muted">{t('graphs.deleteNotice')}</p>
      </Modal>
    </div>
  )
}

function TriggerModal({ graph, onClose }: { graph: AgentGraph | null; onClose: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const [vars, setVars] = useState('{}')
  const [error, setError] = useState('')
  const trigger = useMutation({
    mutationFn: () =>
      repositories.graphs.trigger(
        namespace,
        graph?.name ?? '',
        JSON.parse(vars) as Record<string, unknown>,
      ),
    onSuccess: (graphRunId) => {
      const id = graph?.name
      onClose()
      if (id) navigate(graphPath(namespace, id, `/graph-runs/${encodeURIComponent(graphRunId)}`))
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : String(reason)),
  })
  const submit = () => {
    try {
      const parsed = JSON.parse(vars)
      if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object')
        throw new Error(t('errors.invalidJson'))
      setError('')
      trigger.mutate()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : t('errors.invalidJson'))
    }
  }
  return (
    <Modal
      open={Boolean(graph)}
      title={`${t('graphs.runTitle')} · ${graph?.name ?? ''}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={submit} disabled={trigger.isPending}>
            <Play size={15} />
            {t('common.run')}
          </Button>
        </>
      }
    >
      <label className="field">
        <span className="field__label">{t('graphs.variables')}</span>
        <textarea
          className="code-input"
          rows={10}
          value={vars}
          onChange={(event) => setVars(event.target.value)}
        />
      </label>
      {error && <p className="field__error">{error}</p>}
    </Modal>
  )
}

function formatRelative(value: string, language: string) {
  try {
    return formatDistanceToNowStrict(new Date(value), {
      addSuffix: true,
      locale: language === 'zh' ? zhCN : enUS,
    })
  } catch {
    return '—'
  }
}
