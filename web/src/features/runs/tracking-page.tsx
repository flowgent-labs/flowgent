import { useQuery } from '@tanstack/react-query'
import clsx from 'clsx'
import { Activity, ArrowLeft, Clock3, RadioTower, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowRunPath } from '../../app/paths'
import {
  ErrorState,
  JsonView,
  LoadingState,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { TaskRun, TraceInfo } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { correlateAttempt, flattenSpanTree, type TraceTreeRow } from './run-attempts'
import { duration, formatDate } from './run-utils'

const tabs = ['overview', 'request', 'response', 'attributes', 'events', 'error', 'raw'] as const

interface DisplaySpan extends TraceTreeRow {
  trace: TraceInfo
}

export function TrackingPage() {
  const { t } = useTranslation()
  const { flowId = '', runId = '' } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const traceQuery = useQuery({
    queryKey: [namespace, 'runs', runId, 'trace'],
    queryFn: ({ signal }) => repositories.traces.getRunTrace(namespace, runId, flowId, signal),
  })
  const taskQuery = useQuery({
    queryKey: [namespace, 'runs', runId, 'tasks'],
    queryFn: ({ signal }) => repositories.runs.tasks(namespace, runId, flowId, signal),
  })
  const [selected, setSelected] = useState<string>()
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [tab, setTab] = useState<(typeof tabs)[number]>('overview')
  const allRows = useMemo(
    () =>
      traceQuery.data?.traces.flatMap((trace) =>
        flattenSpanTree(trace.spans).map((row) => ({ ...row, trace })),
      ) ?? [],
    [traceQuery.data],
  )
  const rows = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    return allRows.filter(({ span }) => {
      const searchable = [
        span.operation_name,
        span.service_name,
        span.span_id,
        span.attributes['flowgent.node_id'],
        span.attributes['flowgent.task_id'],
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return (
        (!normalizedSearch || searchable.includes(normalizedSearch)) &&
        (!status || span.status === status)
      )
    })
  }, [allRows, search, status])
  const focusedTaskId = searchParams.get('task')
  const active =
    rows.find(({ span }) => `${span.trace_id}:${span.span_id}` === selected) ??
    rows.find(({ span }) => span.attributes['flowgent.task_id'] === focusedTaskId) ??
    rows[0]
  const activeAttempt = active ? correlateAttempt(active.span, taskQuery.data ?? []) : undefined

  if (traceQuery.isLoading || taskQuery.isLoading) return <LoadingState rows={7} />
  if (traceQuery.isError)
    return <ErrorState error={traceQuery.error} onRetry={() => void traceQuery.refetch()} />
  if (taskQuery.isError)
    return <ErrorState error={taskQuery.error} onRetry={() => void taskQuery.refetch()} />

  return (
    <div className="page-stack tracking-page">
      <button
        type="button"
        className="back-link"
        onClick={() => navigate(flowRunPath(namespace, flowId, runId))}
      >
        <ArrowLeft size={15} />
        {t('common.details')}
      </button>
      <PageHeader
        eyebrow={t('runs.traceEyebrow', { runId })}
        title={t('common.tracking')}
        description={t('runs.traceNotice')}
      />
      <div
        className="trace-source"
        data-testid="trace-source"
        data-source={traceQuery.data?.source ?? 'OTel'}
        data-trace-count={traceQuery.data?.traces.length ?? 0}
        data-span-count={allRows.length}
      >
        <RadioTower size={15} />
        <span>{t('runs.traceSource', { source: traceQuery.data?.source ?? 'OTel' })}</span>
        <code>
          {t('runs.traceCount', { count: traceQuery.data?.traces.length ?? 0 })} ·{' '}
          {t('runs.spanCount', { count: allRows.length })}
        </code>
      </div>
      <div className="trace-layout">
        <section className="surface trace-list">
          <div className="trace-toolbar">
            <label className="search-control">
              <Search size={15} />
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('runs.searchSpans')}
              />
            </label>
            <select value={status} onChange={(event) => setStatus(event.target.value)}>
              <option value="">{t('runs.allStatuses')}</option>
              <option value="OK">OK</option>
              <option value="ERROR">ERROR</option>
              <option value="UNSET">UNSET</option>
            </select>
          </div>
          <div className="trace-header">
            <span>{t('runs.span')}</span>
            <span>{t('runs.timeline')}</span>
            <span>{t('runs.duration')}</span>
          </div>
          <div className="span-list">
            {rows.map((row) => {
              const { span, trace, depth } = row
              const traceStart = new Date(trace.start_time).getTime()
              const spanStart = new Date(span.start_time).getTime()
              const traceRange = Math.max(trace.duration_micros, 1)
              const rawLeft = (((spanStart - traceStart) * 1_000) / traceRange) * 100
              const left = Math.min(Math.max(rawLeft, 0), 99.3)
              const width = Math.min(
                Math.max((span.duration_micros / traceRange) * 100, 0.7),
                100 - left,
              )
              const spanKey = `${span.trace_id}:${span.span_id}`
              const attempt = correlateAttempt(span, taskQuery.data ?? [])
              return (
                <button
                  type="button"
                  key={`${span.trace_id}:${span.span_id}`}
                  className={clsx(
                    'span-row',
                    selected === spanKey || active === row ? 'is-active' : undefined,
                    span.status === 'ERROR' && 'has-error',
                  )}
                  onClick={() => setSelected(spanKey)}
                >
                  <span
                    className="span-row__name"
                    style={{ paddingLeft: `${Math.min(depth, 10) * 12}px` }}
                  >
                    <Activity size={14} />
                    <span>
                      <strong>{span.operation_name}</strong>
                      <small>
                        {attempt
                          ? `${attempt.node_key} #${attempt.attempt} · ${span.service_name}`
                          : span.service_name}
                      </small>
                    </span>
                  </span>
                  <span className="waterfall">
                    <span style={{ left: `${left}%`, width: `${width}%` }} />
                  </span>
                  <span className="span-row__duration">{formatMicros(span.duration_micros)}</span>
                </button>
              )
            })}
            {!rows.length && <div className="trace-empty">{t('runs.noSpans')}</div>}
          </div>
        </section>
        <aside className="surface span-detail">
          {active ? (
            <>
              <header className="span-detail__header">
                <div>
                  <span className="surface__eyebrow">{t('runs.spanDetail')}</span>
                  <h2>{active.span.operation_name}</h2>
                  <small>{active.span.service_name}</small>
                </div>
                <StatusBadge status={active.span.status} />
              </header>
              {activeAttempt && (
                <div className="span-attempt-link">
                  <span>{t('runs.correlatedAttempt')}</span>
                  <code>
                    {activeAttempt.node_key} #{activeAttempt.attempt}
                  </code>
                </div>
              )}
              <div className="tabs" role="tablist">
                {tabs.map((item) => (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={tab === item}
                    className={tab === item ? 'is-active' : ''}
                    onClick={() => setTab(item)}
                    key={item}
                  >
                    {t(`runs.${item}`)}
                  </button>
                ))}
              </div>
              <SpanTab tab={tab} row={active} task={activeAttempt} />
            </>
          ) : (
            <div className="task-placeholder">
              <Clock3 />
              <p>{t('runs.noSpans')}</p>
            </div>
          )}
        </aside>
      </div>
    </div>
  )
}

function SpanTab({
  tab,
  row,
  task,
}: {
  tab: (typeof tabs)[number]
  row: DisplaySpan
  task?: TaskRun
}) {
  const { t } = useTranslation()
  const { span } = row
  if (tab === 'request')
    return task ? (
      <JsonView label={t('runs.persistedTaskInput')} value={task.input} />
    ) : (
      <div className="empty-inline">{t('runs.noCorrelatedAttempt')}</div>
    )
  if (tab === 'response')
    return task ? (
      <JsonView label={t('runs.persistedTaskOutput')} value={task.output} />
    ) : (
      <div className="empty-inline">{t('runs.noCorrelatedAttempt')}</div>
    )
  if (tab === 'events') return <JsonView value={span.events ?? []} />
  if (tab === 'error') {
    const error = {
      task_error: task?.error || undefined,
      warnings: span.warnings,
      events: span.events,
    }
    return task?.error || span.warnings?.length || span.events?.length ? (
      <JsonView value={error} />
    ) : (
      <div className="empty-inline">{t('runs.noError')}</div>
    )
  }
  if (tab === 'raw') return <JsonView value={{ span, task_run: task ?? null }} />
  if (tab === 'attributes') return <JsonView value={span.attributes} />
  return (
    <dl className="detail-list">
      <div>
        <dt>{t('common.status')}</dt>
        <dd>
          <StatusBadge status={span.status} />
        </dd>
      </div>
      <div>
        <dt>{t('runs.service')}</dt>
        <dd>{span.service_name}</dd>
      </div>
      <div>
        <dt>{t('runs.started')}</dt>
        <dd>{formatDate(span.start_time)}</dd>
      </div>
      <div>
        <dt>{t('runs.duration')}</dt>
        <dd>{formatMicros(span.duration_micros)}</dd>
      </div>
      <div>
        <dt>{t('runs.traceId')}</dt>
        <dd>
          <code>{span.trace_id}</code>
        </dd>
      </div>
      <div>
        <dt>{t('runs.spanId')}</dt>
        <dd>
          <code>{span.span_id}</code>
        </dd>
      </div>
      <div>
        <dt>{t('runs.parent')}</dt>
        <dd>
          <code>{span.parent_span_id || t('runs.root')}</code>
        </dd>
      </div>
      {task && (
        <div>
          <dt>{t('runs.taskRun')}</dt>
          <dd>
            <code>{task.id}</code>
          </dd>
        </div>
      )}
    </dl>
  )
}

function formatMicros(micros: number): string {
  if (micros < 1_000) return `${Math.round(micros)}µs`
  if (micros < 1_000_000) return `${(micros / 1_000).toFixed(1)}ms`
  if (micros < 60_000_000) return `${(micros / 1_000_000).toFixed(2)}s`
  return duration('1970-01-01T00:00:00.000Z', new Date(micros / 1_000).toISOString())
}
