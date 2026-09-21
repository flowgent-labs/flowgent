import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, Filter, Play, Search, Trash2, Workflow } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowPath, flowRunPath } from '../../app/paths'
import {
  Button,
  EmptyState,
  ErrorState,
  LoadingState,
  Modal,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { FlowRun, RunStatus } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { duration, formatDate } from './run-utils'

const runStatuses: RunStatus[] = [
  'PENDING',
  'RUNNING',
  'COMPLETED',
  'FAILED',
  'PAUSED',
  'CANCELLED',
]

export function RunsPage() {
  const { t } = useTranslation()
  const { flowId } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const status = (params.get('status') || '') as RunStatus | ''
  const search = params.get('q') ?? ''
  const trigger = params.get('trigger') ?? ''
  const [deleteRun, setDeleteRun] = useState<FlowRun | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'runs', { flowId, status }],
    queryFn: ({ signal }) =>
      repositories.runs.list(
        namespace,
        { page: 1, size: 200, flowId, status: status || undefined },
        signal,
      ),
    refetchInterval: (state) =>
      state.state.data?.items.some((run) => ['PENDING', 'RUNNING'].includes(run.status))
        ? 5_000
        : false,
  })
  const cancel = useMutation({
    mutationFn: (id: string) => repositories.runs.cancel(namespace, id, flowId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'runs'] }),
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.runs.remove(namespace, id, flowId),
    onSuccess: () => {
      setDeleteRun(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runs'] })
    },
  })
  const triggerRun = useMutation({
    mutationFn: () => repositories.flows.trigger(namespace, flowId ?? '', {}),
    onSuccess: (runId) => navigate(flowRunPath(namespace, flowId ?? '', runId)),
  })
  const rows = useMemo(
    () =>
      query.data?.items.filter(
        (run) =>
          (!search ||
            `${run.id} ${run.agentflow_id}`.toLowerCase().includes(search.toLowerCase())) &&
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
        eyebrow={flowId ? t('runs.flowContext', { flowId }) : t('runs.inventory')}
        title={t('runs.title')}
        description={t('runs.subtitle')}
        actions={
          flowId && (
            <Button
              data-testid="flow-run"
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
                  <th>{t('runs.runId')}</th>
                  {!flowId && <th>{t('runs.flow')}</th>}
                  <th>{t('common.status')}</th>
                  <th>{t('runs.trigger')}</th>
                  <th>{t('flows.version')}</th>
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
                        to={flowRunPath(namespace, run.agentflow_id, run.id)}
                      >
                        {run.id}
                      </Link>
                    </td>
                    {!flowId && (
                      <td>
                        <Link to={flowPath(namespace, run.agentflow_id, '/runs')}>
                          {run.agentflow_id}
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
                    <td>v{run.version}</td>
                    <td>{formatDate(run.started_at)}</td>
                    <td>{duration(run.started_at, run.finished_at)}</td>
                    <td className="row-actions">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => navigate(flowRunPath(namespace, run.agentflow_id, run.id))}
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
