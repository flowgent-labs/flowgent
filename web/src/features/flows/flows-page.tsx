import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNowStrict } from 'date-fns'
import { enUS, zhCN } from 'date-fns/locale'
import { Copy, GitBranch, MoreHorizontal, Play, Plus, Search, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { flowPath, namespacePathname } from '../../app/paths'
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
import type { Flow } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

export function FlowsPage() {
  const { t, i18n } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [triggerFlow, setTriggerFlow] = useState<Flow | null>(null)
  const [deleteFlow, setDeleteFlow] = useState<Flow | null>(null)
  const [menu, setMenu] = useState<string | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'flows'],
    queryFn: ({ signal }) => repositories.flows.list(namespace, signal),
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.flows.remove(namespace, id),
    onSuccess: () => {
      setDeleteFlow(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'flows'] })
    },
  })
  const duplicate = useMutation({
    mutationFn: (flow: Flow) =>
      repositories.flows.save(
        namespace,
        {
          ...structuredClone(flow),
          id: `${flow.id}-copy`,
          summary: `${flow.summary ?? flow.id} (copy)`,
          version: 1,
          created_at: '',
          updated_at: '',
        },
        true,
      ),
    onSuccess: (flow) => {
      void queryClient.invalidateQueries({ queryKey: [namespace, 'flows'] })
      navigate(flowPath(namespace, flow.id))
    },
  })
  const filtered = useMemo(
    () =>
      query.data?.filter((flow) =>
        `${flow.id} ${flow.summary} ${flow.description} ${Object.values(flow.labels ?? {}).join(' ')}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('flows.eyebrow')}
        title={t('flows.title')}
        description={t('flows.subtitle')}
        actions={
          <Button onClick={() => navigate(namespacePathname(namespace, '/flows/new'))}>
            <Plus size={16} />
            {t('flows.new')}
          </Button>
        }
      />
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('flows.search')}
          />
        </label>
        <div className="toolbar__end">
          <span className="result-count">{t('flows.flowCount', { count: filtered.length })}</span>
        </div>
      </div>
      {query.isLoading ? (
        <LoadingState rows={5} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : filtered.length === 0 ? (
        <EmptyState
          title={search ? t('common.noData') : t('flows.createFirst')}
          action={
            !search && (
              <Button onClick={() => navigate(namespacePathname(namespace, '/flows/new'))}>
                <Plus size={16} />
                {t('flows.new')}
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
                  <th>{t('runs.flow')}</th>
                  <th>{t('flows.nodes')}</th>
                  <th>{t('flows.trigger')}</th>
                  <th>{t('common.status')}</th>
                  <th>{t('common.updated')}</th>
                  <th aria-label={t('common.actions')} />
                </tr>
              </thead>
              <tbody>
                {filtered.map((flow) => (
                  <tr key={flow.id} data-testid={`flow-row-${flow.id}`}>
                    <td>
                      <button
                        type="button"
                        className="resource-cell"
                        onClick={() => navigate(flowPath(namespace, flow.id))}
                      >
                        <span className="resource-icon">
                          <GitBranch size={17} />
                        </span>
                        <span>
                          <strong>{flow.id}</strong>
                          <small>{flow.summary || flow.description || '—'}</small>
                        </span>
                      </button>
                    </td>
                    <td>
                      <strong>{flow.nodes.length}</strong>
                      <small className="cell-subtitle">
                        {t('flows.edgeCount', { count: flow.edges.length })}
                      </small>
                    </td>
                    <td>
                      <span className="trigger-label">{flow.triggers?.[0]?.type ?? 'manual'}</span>
                    </td>
                    <td>
                      <StatusBadge status={flow.status || 'ACTIVE'} />
                    </td>
                    <td>
                      <span title={flow.updated_at}>
                        {formatRelative(flow.updated_at, i18n.language)}
                      </span>
                      <small className="cell-subtitle">v{flow.version ?? 1}</small>
                    </td>
                    <td className="row-actions">
                      <Button size="sm" variant="secondary" onClick={() => setTriggerFlow(flow)}>
                        <Play size={14} />
                        {t('common.run')}
                      </Button>
                      <div className="more-menu">
                        <IconButton
                          data-testid={`flow-actions-${flow.id}`}
                          label={t('common.actions')}
                          onClick={() => setMenu(menu === flow.id ? null : flow.id)}
                        >
                          <MoreHorizontal size={17} />
                        </IconButton>
                        {menu === flow.id && (
                          <div className="more-menu__popover">
                            <button
                              type="button"
                              onClick={() => navigate(flowPath(namespace, flow.id, '/runs'))}
                            >
                              {t('common.runs')}
                            </button>
                            <button type="button" onClick={() => duplicate.mutate(flow)}>
                              <Copy size={14} />
                              {t('flows.duplicate')}
                            </button>
                            <button
                              type="button"
                              className="danger"
                              data-testid={`flow-delete-${flow.id}`}
                              onClick={() => {
                                setDeleteFlow(flow)
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
      <TriggerModal flow={triggerFlow} onClose={() => setTriggerFlow(null)} />
      <Modal
        open={Boolean(deleteFlow)}
        title={`${t('common.delete')} ${deleteFlow?.id ?? ''}?`}
        onClose={() => setDeleteFlow(null)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeleteFlow(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              data-testid="flow-delete-confirm"
              variant="danger"
              disabled={remove.isPending}
              onClick={() => deleteFlow && remove.mutate(deleteFlow.id)}
            >
              {t('common.delete')}
            </Button>
          </>
        }
      >
        <p>{t('common.confirmDelete')}</p>
        <p className="muted">{t('flows.deleteNotice')}</p>
      </Modal>
    </div>
  )
}

function TriggerModal({ flow, onClose }: { flow: Flow | null; onClose: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const [vars, setVars] = useState('{}')
  const [error, setError] = useState('')
  const trigger = useMutation({
    mutationFn: () =>
      repositories.flows.trigger(
        namespace,
        flow?.id ?? '',
        JSON.parse(vars) as Record<string, unknown>,
      ),
    onSuccess: (runId) => {
      const id = flow?.id
      onClose()
      if (id) navigate(flowPath(namespace, id, `/runs/${encodeURIComponent(runId)}`))
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
      open={Boolean(flow)}
      title={`${t('flows.runTitle')} · ${flow?.id ?? ''}`}
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
        <span className="field__label">{t('flows.variables')}</span>
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
