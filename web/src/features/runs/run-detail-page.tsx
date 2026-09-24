import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Clock3,
  ExternalLink,
  GitCommitHorizontal,
  PlayCircle,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowPath, flowRunPath } from '../../app/paths'
import {
  Button,
  ErrorState,
  JsonView,
  LoadingState,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import { useAppStore } from '../../app/store'
import type { TaskRun } from '../../core/domain/types'
import { attemptsForNode } from './run-attempts'
import { RunGraph } from './run-graph'
import { duration, formatDate } from './run-utils'

export function RunDetailPage() {
  const { t } = useTranslation()
  const { flowId = '', runId = '' } = useParams()
  const navigate = useNavigate()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const run = useQuery({
    queryKey: [namespace, 'runs', runId],
    queryFn: ({ signal }) => repositories.runs.get(namespace, runId, flowId, signal),
    refetchInterval: (query) =>
      query.state.data && ['PENDING', 'RUNNING'].includes(query.state.data.status) ? 4_000 : false,
  })
  const flow = useQuery({
    queryKey: [namespace, 'flows', flowId],
    queryFn: ({ signal }) => repositories.flows.get(namespace, flowId, signal),
  })
  const tasks = useQuery({
    queryKey: [namespace, 'runs', runId, 'tasks'],
    queryFn: ({ signal }) => repositories.runs.tasks(namespace, runId, flowId, signal),
    refetchInterval: run.data && ['PENDING', 'RUNNING'].includes(run.data.status) ? 4_000 : false,
  })
  const terminalTaskSync = useRef<string | undefined>(undefined)
  const terminalRevision =
    run.data && ['COMPLETED', 'FAILED', 'CANCELLED'].includes(run.data.status)
      ? `${run.data.status}:${run.data.updated_at}`
      : undefined
  useEffect(() => {
    if (!terminalRevision || terminalTaskSync.current === terminalRevision) return
    terminalTaskSync.current = terminalRevision
    // Run and TaskRun polling are independent. The final Run update can stop
    // polling before the task query observes the last persisted attempts, so
    // every terminal revision gets one authoritative TaskRun refresh.
    void queryClient.refetchQueries({
      queryKey: [namespace, 'runs', runId, 'tasks'],
      exact: true,
      type: 'active',
    })
  }, [namespace, queryClient, runId, terminalRevision])
  const approvals = useQuery({
    queryKey: [namespace, 'runs', runId, 'approvals'],
    queryFn: ({ signal }) => repositories.runs.approvals(namespace, runId, flowId, signal),
    refetchInterval: 4_000,
  })
  const resolveApproval = useMutation({
    mutationFn: ({
      approvalId,
      decision,
    }: {
      approvalId: string
      decision: 'approve' | 'reject'
    }) => repositories.runs.resolveApproval(namespace, runId, approvalId, decision, flowId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runs', runId] })
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runs', runId, 'tasks'] })
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runs', runId, 'approvals'] })
    },
  })
  const [selected, setSelected] = useState<string | undefined>()
  const [selectedAttemptId, setSelectedAttemptId] = useState<string>()
  if (run.isLoading || flow.isLoading || tasks.isLoading || approvals.isLoading)
    return <LoadingState rows={7} />
  if (run.isError) return <ErrorState error={run.error} onRetry={() => void run.refetch()} />
  if (flow.isError) return <ErrorState error={flow.error} onRetry={() => void flow.refetch()} />
  if (tasks.isError) return <ErrorState error={tasks.error} onRetry={() => void tasks.refetch()} />
  if (approvals.isError)
    return <ErrorState error={approvals.error} onRetry={() => void approvals.refetch()} />
  if (!run.data || !flow.data || !tasks.data || !approvals.data) return null
  const selectedAttempts = attemptsForNode(tasks.data, selected)
  const selectedTask =
    selectedAttempts.find((task) => task.id === selectedAttemptId) ?? selectedAttempts.at(-1)
  return (
    <div className="page-stack">
      <button
        type="button"
        className="back-link"
        onClick={() => navigate(flowPath(namespace, flowId, '/runs'))}
      >
        <ArrowLeft size={15} />
        {t('runs.title')}
      </button>
      <PageHeader
        eyebrow={t('runs.runEyebrow', { runId: run.data.id })}
        title={flow.data.summary || flowId}
        description={`${t('runs.summary')} · ${run.data.trigger_type || 'manual'}`}
        actions={
          <>
            <Button
              variant="secondary"
              onClick={() => navigate(flowRunPath(namespace, flowId, runId, '/tracking'))}
            >
              <Activity size={16} />
              {t('common.tracking')}
            </Button>
            <span data-testid="run-status" data-status={run.data.status}>
              <StatusBadge status={run.data.status} />
            </span>
          </>
        }
      />
      <div className="page-meta">
        <span>{formatDate(run.data.updated_at)}</span>
      </div>
      <section className="run-summary-grid">
        <SummaryItem
          icon={<PlayCircle />}
          label={t('runs.started')}
          value={formatDate(run.data.started_at)}
        />
        <SummaryItem
          icon={<Clock3 />}
          label={t('runs.duration')}
          value={duration(run.data.started_at, run.data.finished_at)}
        />
        <SummaryItem
          icon={<GitCommitHorizontal />}
          label={t('flows.version')}
          value={`v${run.data.flow_revision}`}
        />
        <SummaryItem
          icon={<Activity />}
          label={t('runs.trigger')}
          value={`${run.data.trigger_type || 'manual'} / ${run.data.trigger_source || 'ui'}`}
        />
      </section>
      {run.data.error && (
        <div className="error-banner">
          <AlertTriangle size={18} />
          <div>
            <strong>{t('runs.error')}</strong>
            <p>{run.data.error}</p>
          </div>
        </div>
      )}
      {approvals.data.map((approval) => (
        <section className="contract-warning" data-testid="pending-approval" key={approval.id}>
          <ShieldApproval />
          <span>
            <strong>{t('runs.humanApproval')}</strong>
            {' · '}
            {t('runs.humanApprovalNotice')}
          </span>
          <Button
            size="sm"
            variant="secondary"
            disabled={resolveApproval.isPending}
            onClick={() => resolveApproval.mutate({ approvalId: approval.id, decision: 'reject' })}
          >
            {t('runs.reject')}
          </Button>
          <Button
            size="sm"
            disabled={resolveApproval.isPending}
            onClick={() => resolveApproval.mutate({ approvalId: approval.id, decision: 'approve' })}
          >
            {t('runs.approve')}
          </Button>
        </section>
      ))}
      <div className="run-layout">
        <section className="surface run-dag-panel">
          <header className="surface__header">
            <div>
              <span className="surface__eyebrow">
                {t('runs.dagNodes', { count: flow.data.nodes.length })}
              </span>
              <h2>{t('runs.tasks')}</h2>
            </div>
            <Link className="text-link" to={flowPath(namespace, flowId)}>
              {t('runs.viewDefinition')}
            </Link>
          </header>
          <RunGraph
            flow={flow.data}
            tasks={tasks.data}
            selected={selected}
            onSelect={(nodeId) => {
              setSelected(nodeId)
              setSelectedAttemptId(undefined)
            }}
          />
        </section>
        <aside className="surface task-panel">
          <header className="surface__header">
            <div>
              <span className="surface__eyebrow">{t('runs.nodeInspector')}</span>
              <h2>{selected || t('runs.selectNode')}</h2>
            </div>
            {selectedTask && <StatusBadge status={selectedTask.status} />}
          </header>
          {selectedTask ? (
            <>
              <div className="attempt-list">
                <div className="attempt-list__title">
                  <strong>{t('runs.attemptHistory')}</strong>
                  <span>{t('runs.attemptCount', { count: selectedAttempts.length })}</span>
                </div>
                <div className="attempt-list__items">
                  {selectedAttempts.map((attempt) => (
                    <button
                      type="button"
                      className={selectedTask.id === attempt.id ? 'is-active' : ''}
                      key={attempt.id}
                      onClick={() => setSelectedAttemptId(attempt.id)}
                    >
                      <span>
                        <strong>{t('runs.attemptNumber', { count: attempt.attempt })}</strong>
                        <small>{formatDate(attempt.started_at)}</small>
                      </span>
                      <StatusBadge status={attempt.status} />
                    </button>
                  ))}
                </div>
              </div>
              <TaskAttemptDetail
                task={selectedTask}
                traceHref={`${flowRunPath(namespace, flowId, runId, '/tracking')}?task=${encodeURIComponent(selectedTask.id)}`}
              />
            </>
          ) : (
            <div className="task-placeholder">
              <GitCommitHorizontal size={25} />
              <p>{t('runs.selectNodeHelp')}</p>
            </div>
          )}
        </aside>
      </div>
      <div className="json-grid">
        <JsonView label={t('runs.variables')} value={run.data.input} />
        <JsonView label={t('runs.output')} value={run.data.output} />
      </div>
    </div>
  )
}

function TaskAttemptDetail({ task, traceHref }: { task: TaskRun; traceHref: string }) {
  const { t } = useTranslation()
  return (
    <div className="task-details">
      <dl>
        <div>
          <dt>{t('runs.executionId')}</dt>
          <dd>
            <code>{task.execution_id || '—'}</code>
          </dd>
        </div>
        <div>
          <dt>{t('runs.attempt')}</dt>
          <dd>
            {task.attempt} / {task.max_retries + 1}
          </dd>
        </div>
        <div>
          <dt>{t('runs.duration')}</dt>
          <dd>{duration(task.started_at, task.finished_at)}</dd>
        </div>
      </dl>
      <Link className="attempt-trace-link" to={traceHref}>
        <Activity size={14} />
        {t('runs.viewAttemptTrace')}
        <ExternalLink size={12} />
      </Link>
      {task.error && <div className="inline-error">{task.error}</div>}
      <JsonView label={t('runs.request')} value={task.input} />
      <JsonView label={t('runs.response')} value={task.output} />
    </div>
  )
}

function SummaryItem({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: string
}) {
  return (
    <div className="summary-item">
      <span className="summary-item__icon">{icon}</span>
      <span>
        <small>{label}</small>
        <strong>{value}</strong>
      </span>
    </div>
  )
}

function ShieldApproval() {
  return <AlertTriangle size={18} aria-hidden="true" />
}
