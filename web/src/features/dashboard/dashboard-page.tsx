import { useQuery } from '@tanstack/react-query'
import { format, formatDistanceToNowStrict } from 'date-fns'
import { enUS, zhCN } from 'date-fns/locale'
import {
  Activity,
  ArrowRight,
  CheckCircle2,
  CircleGauge,
  Clock3,
  ShieldAlert,
  XCircle,
} from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowRunPath } from '../../app/paths'
import {
  EmptyState,
  ErrorState,
  LoadingState,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import { LineChartView, type LineChartSeries } from '../../shared/components/line-chart'
import type { RunStatus } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

const ranges = [
  { label: '1h', hours: 1 },
  { label: '24h', hours: 24 },
  { label: '7d', hours: 168 },
  { label: '30d', hours: 720 },
]

export function DashboardPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const [hours, setHours] = useState(24)
  const metrics = useQuery({
    queryKey: [namespace, 'dashboard', hours],
    queryFn: ({ signal }) => repositories.analytics.metrics(namespace, hours, signal),
  })
  const runs = useQuery({
    queryKey: [namespace, 'runs', 'dashboard'],
    queryFn: ({ signal }) => repositories.runs.list(namespace, { page: 1, size: 100 }, signal),
    refetchInterval: 15_000,
  })
  const data = metrics.data
  const chartSeries = useMemo<LineChartSeries[]>(
    () => [
      {
        name: t('dashboard.running'),
        color: '#43d6a2',
        values: data?.buckets.map((bucket) => bucket.running) ?? [],
      },
      {
        name: t('dashboard.completed'),
        color: '#6f9cff',
        values: data?.buckets.map((bucket) => bucket.completed) ?? [],
      },
      {
        name: t('dashboard.failed'),
        color: '#ff6f7d',
        values: data?.buckets.map((bucket) => bucket.failed) ?? [],
      },
    ],
    [data, t],
  )

  const goToRuns = useCallback(
    (status?: RunStatus) => navigate(`/runs${status ? `?status=${status}` : ''}`),
    [navigate],
  )
  const handleLegendSelect = useCallback(
    (name: string) => {
      if (name === t('dashboard.failed')) goToRuns('FAILED')
      if (name === t('dashboard.completed')) goToRuns('COMPLETED')
      if (name === t('dashboard.running')) goToRuns('RUNNING')
    },
    [goToRuns, t],
  )
  const failures = runs.data?.items.filter((run) => run.status === 'FAILED').slice(0, 5) ?? []
  const active =
    runs.data?.items
      .filter((run) => ['PENDING', 'RUNNING', 'PAUSED'].includes(run.status))
      .slice(0, 5) ?? []

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('dashboard.eyebrow')}
        title={t('dashboard.title')}
        description={t('dashboard.subtitle')}
        actions={
          <div className="range-switch" aria-label={t('dashboard.range')}>
            {ranges.map((range) => (
              <button
                key={range.hours}
                data-testid={`dashboard-range-${range.hours}`}
                type="button"
                className={hours === range.hours ? 'is-active' : ''}
                onClick={() => setHours(range.hours)}
              >
                {range.label}
              </button>
            ))}
          </div>
        }
      />
      <div className="page-meta">
        <span className="live-indicator">
          <span />
          {t('dashboard.updatedNow')}
        </span>
      </div>
      {metrics.isLoading ? (
        <LoadingState rows={3} />
      ) : metrics.isError ? (
        <ErrorState error={metrics.error} onRetry={() => void metrics.refetch()} />
      ) : (
        data && (
          <>
            <section className="kpi-grid">
              <Kpi
                label={t('dashboard.total')}
                value={data.total}
                icon={<CircleGauge />}
                accent="neutral"
                onClick={() => goToRuns()}
              />
              <Kpi
                label={t('dashboard.running')}
                value={data.running}
                icon={<Activity />}
                accent="success"
                onClick={() => goToRuns('RUNNING')}
                pulse
              />
              <Kpi
                label={t('dashboard.completed')}
                value={data.completed}
                icon={<CheckCircle2 />}
                accent="blue"
                onClick={() => goToRuns('COMPLETED')}
              />
              <Kpi
                label={t('dashboard.failed')}
                value={data.failed}
                icon={<XCircle />}
                accent="danger"
                onClick={() => goToRuns('FAILED')}
              />
              <Kpi
                label={t('dashboard.successRate')}
                value={`${Math.round(data.success_rate * 100)}%`}
                icon={<ShieldAlert />}
                accent="success"
                onClick={() => goToRuns('COMPLETED')}
              />
              <Kpi
                label={t('dashboard.failureRate')}
                value={`${Math.round(data.failure_rate * 100)}%`}
                icon={<ShieldAlert />}
                accent="warning"
                onClick={() => goToRuns('FAILED')}
              />
              <Kpi
                label={t('dashboard.averageDuration')}
                value={formatDuration(data.average_duration_ms)}
                icon={<Clock3 />}
                accent="blue"
                onClick={() => goToRuns()}
              />
            </section>
            <section className="surface chart-panel" data-testid="dashboard-telemetry">
              <header className="surface__header">
                <div>
                  <span className="surface__eyebrow">{t('dashboard.telemetry')}</span>
                  <h2>{t('dashboard.throughput')}</h2>
                </div>
                <span className="chart-summary">
                  {t('dashboard.events', { count: data.total })}
                </span>
              </header>
              <LineChartView
                labels={data.buckets.map((bucket) =>
                  format(new Date(bucket.start_time), hours <= 24 ? 'HH:mm' : 'MM-dd'),
                )}
                series={chartSeries}
                onLegendSelect={handleLegendSelect}
              />
            </section>
            <div className="dashboard-grid">
              <section className="surface">
                <header className="surface__header">
                  <div>
                    <span className="surface__eyebrow">{t('dashboard.attention')}</span>
                    <h2>{t('dashboard.recentFailures')}</h2>
                  </div>
                  <button className="text-link" type="button" onClick={() => goToRuns('FAILED')}>
                    {t('common.viewAll')} <ArrowRight size={14} />
                  </button>
                </header>
                {failures.length ? (
                  <div className="activity-list">
                    {failures.map((run) => (
                      <button
                        type="button"
                        className="activity-row"
                        key={run.id}
                        onClick={() => navigate(flowRunPath(namespace, run.agentflow_id, run.id))}
                      >
                        <span className="activity-row__icon activity-row__icon--danger">
                          <XCircle size={16} />
                        </span>
                        <span className="activity-row__copy">
                          <strong>{run.agentflow_id}</strong>
                          <span>{run.error || t('dashboard.executionFailed')}</span>
                        </span>
                        <span className="activity-row__meta">
                          <code>{run.id}</code>
                          <span>{timeAgo(run.updated_at, i18n.language)}</span>
                        </span>
                      </button>
                    ))}
                  </div>
                ) : (
                  <EmptyState title={t('dashboard.noFailures')} />
                )}
              </section>
              <section className="surface">
                <header className="surface__header">
                  <div>
                    <span className="surface__eyebrow">{t('dashboard.inFlight')}</span>
                    <h2>{t('dashboard.activeRuns')}</h2>
                  </div>
                  <Activity size={18} />
                </header>
                {active.length ? (
                  <div className="activity-list">
                    {active.map((run) => (
                      <button
                        type="button"
                        className="activity-row"
                        key={run.id}
                        onClick={() => navigate(flowRunPath(namespace, run.agentflow_id, run.id))}
                      >
                        <span className="activity-row__icon">
                          <Clock3 size={16} />
                        </span>
                        <span className="activity-row__copy">
                          <strong>{run.agentflow_id}</strong>
                          <span>
                            {run.trigger_type || 'manual'} · v{run.version}
                          </span>
                        </span>
                        <span className="activity-row__meta">
                          <StatusBadge status={run.status} />
                          <span>{timeAgo(run.updated_at, i18n.language)}</span>
                        </span>
                      </button>
                    ))}
                  </div>
                ) : (
                  <EmptyState title={t('common.noData')} />
                )}
              </section>
            </div>
          </>
        )
      )}
    </div>
  )
}

function Kpi({
  label,
  value,
  icon,
  accent,
  onClick,
  pulse,
}: {
  label: string
  value: string | number
  icon: React.ReactNode
  accent: string
  onClick: () => void
  pulse?: boolean
}) {
  return (
    <button type="button" className={`kpi kpi--${accent}`} onClick={onClick}>
      <span className="kpi__icon">
        {icon}
        {pulse && <span className="kpi__pulse" />}
      </span>
      <span className="kpi__copy">
        <span>{label}</span>
        <strong>{value}</strong>
      </span>
      <ArrowRight size={15} className="kpi__arrow" />
    </button>
  )
}

function timeAgo(value: string, language: string) {
  try {
    return formatDistanceToNowStrict(new Date(value), {
      addSuffix: true,
      locale: language === 'zh' ? zhCN : enUS,
    })
  } catch {
    return '—'
  }
}

function formatDuration(milliseconds: number) {
  if (!milliseconds || milliseconds < 0) return '—'
  if (milliseconds < 1000) return `${milliseconds} ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} s`
  return `${Math.floor(milliseconds / 60_000)}m ${Math.round((milliseconds % 60_000) / 1000)}s`
}
