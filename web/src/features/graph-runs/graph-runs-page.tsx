import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, Filter, Play, Search, Trash2, Workflow } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { graphPath, graphRunPath } from '../../app/paths'
import {
  Button,
  EmptyState,
  ErrorState,
  LoadingState,
  Modal,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { GraphRun, GraphRunStatus } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { duration, formatDate } from './run-utils'

const runStatuses: GraphRunStatus[] = [
  'PENDING',
  'RUNNING',
  'COMPLETED',
  'FAILED',
  'PAUSED',
  'CANCELLED',
]

export function GraphRunsPage() {
  const { t } = useTranslation()
  const { graphId } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const status = (params.get('status') || '') as GraphRunStatus | ''
  const search = params.get('q') ?? ''
  const trigger = params.get('trigger') ?? ''
  const [deleteRun, setDeleteRun] = useState<GraphRun | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'runs', { graphId, status }],
    queryFn: ({ signal }) =>
      repositories.graphRuns.list(
        namespace,
        { page: 1, size: 200, graphId, status: status || undefined },
        signal,
      ),
    refetchInterval: (state) =>
      state.state.data?.items.some((run) => ['PENDING', 'RUNNING'].includes(run.status))
        ? 5_000
        : false,
  })
  const cancel = useMutation({
    mutationFn: (id: string) => repositories.graphRuns.cancel(namespace, id, graphId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'runs'] }),
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.graphRuns.remove(namespace, id, graphId),
    onSuccess: () => {
      setDeleteRun(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runs'] })
    },
  })
  const triggerRun = useMutation({
    mutationFn: () => repositories.graphs.trigger(namespace, graphId ?? '', {}),
    onSuccess: (graphRunId) => navigate(graphRunPath(namespace, graphId ?? '', graphRunId)),
  })
  const rows = useMemo(
    () =>
      query.data?.items.filter(
        (run) =>
          (!search ||
            `${run.id} ${run.agent_graph_name}`.toLowerCase().includes(search.toLowerCase())) &&
          (!trigger || run.trigger_type === trigger),
      ) ?? [],
    [query.data, search, trigger],
  )
  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={graphId ? t('runs.graphContext', { graphId }) : t('runs.inventory')}
        title={t('runs.title')}
        description={t('runs.subtitle')}
        actions={
          graphId && (
            <Button
              data-testid="graph-run"
              onClick={() => triggerRun.mutate()}
              disabled={triggerRun.isPending}
            >
              <Play size={16} />
              {t('common.run')}
            </Button>
          )
        }
      />
      <div className="toolbar toolbar--wrap">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setFilter('q', event.target.value)}
            placeholder={t('runs.search')}
          />
        </label>
        <label className="filter-control">
          <Filter size={15} />
          <select value={status} onChange={(event) => setFilter('status', event.target.value)}>
            <option value="">
              {t('common.all')} {t('common.status')}
            </option>
            {runStatuses.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </select>
        </label>
        <label className="filter-control">
          <Workflow size={15} />
          <select value={trigger} onChange={(event) => setFilter('trigger', event.target.value)}>
            <option value="">{t('runs.allTriggers')}</option>
            <option value="manual">{t('runs.triggerTypes.manual')}</option>
            <option value="webhook">{t('runs.triggerTypes.webhook')}</option>
            <option value="cron">{t('runs.triggerTypes.cron')}</option>
          </select>
        </label>
        <div className="toolbar__end">
          {query.data && (
            <span className="result-count">
              {rows.length} / {query.data.total_count}
            </span>
          )}
        </div>
      </div>
      {query.isLoading ? (
        <LoadingState rows={6} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState title={t('common.noData')} />
      ) : (
        <section className="surface table-surface">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t('runs.graphRunId')}</th>
                  {!graphId && <th>{t('runs.graph')}</th>}
                  <th>{t('common.status')}</th>
                  <th>{t('runs.trigger')}</th>
                  <th>{t('graphs.version')}</th>
                  <th>{t('runs.started')}</th>
                  <th>{t('runs.duration')}</th>
                  <th aria-label={t('common.actions')} />
                </tr>
              </thead>
              <tbody>
                {rows.map((run) => (
                  <tr key={run.id}>
                    <td>
                      <Link
                        className="mono-link"
                        to={graphRunPath(namespace, run.agent_graph_name, run.id)}
                      >
                        {run.id}
                      </Link>
                    </td>
                    {!graphId && (
                      <td>
                        <Link to={graphPath(namespace, run.agent_graph_name, '/graph-runs')}>
                          {run.agent_graph_name}
                        </Link>
                      </td>
                    )}
                    <td>
                      <StatusBadge status={run.status} />
                    </td>
                    <td>
                      <span className="trigger-label">{run.trigger_type || 'manual'}</span>
                      <small className="cell-subtitle">{run.trigger_source}</small>
                    </td>
                    <td>v{run.agent_graph_revision}</td>
                    <td>{formatDate(run.started_at)}</td>
                    <td>{duration(run.started_at, run.finished_at)}</td>
                    <td className="row-actions">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          navigate(graphRunPath(namespace, run.agent_graph_name, run.id))
                        }
                      >
                        <Eye size={14} />
                        {t('common.details')}
                      </Button>
                      {['PENDING', 'RUNNING', 'PAUSED'].includes(run.status) && (
                        <Button size="sm" variant="secondary" onClick={() => cancel.mutate(run.id)}>
                          {t('common.cancel')}
                        </Button>
                      )}
                      {!['PENDING', 'RUNNING'].includes(run.status) && (
                        <Button size="sm" variant="ghost" onClick={() => setDeleteRun(run)}>
                          <Trash2 size={14} />
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      <Modal
        open={Boolean(deleteRun)}
        title={`${t('runs.deleteRun')} · ${deleteRun?.id ?? ''}`}
        onClose={() => setDeleteRun(null)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeleteRun(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="danger"
              onClick={() => deleteRun && remove.mutate(deleteRun.id)}
              disabled={remove.isPending}
            >
              {t('common.delete')}
            </Button>
          </>
        }
      >
        <p>{t('common.confirmDelete')}</p>
      </Modal>
    </div>
  )
}
