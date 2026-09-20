import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Globe2, KeyRound, Network, Plus, Search, Server, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useRepositories } from '../../app/providers'
import {
  Button,
  Drawer,
  EmptyState,
  ErrorState,
  IconButton,
  LoadingState,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { McpServer } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToMcp } from '../resources/manifest'

const blankMcp = (): McpServer => ({
  id: '',
  name: '',
  enabled: true,
  type: 'streamable-http',
  url: '',
  header_refs: {},
  env_refs: {},
  labels: {},
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
})

export function McpsPage() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<McpServer | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'mcps'],
    queryFn: ({ signal }) => repositories.mcps.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: ({ item, isNew }: { item: McpServer; isNew: boolean }) =>
      repositories.mcps.save(namespace, item, isNew),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'mcps'] })
    },
  })
  const remove = useMutation({
    mutationFn: (name: string) => repositories.mcps.remove(namespace, name),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'mcps'] }),
  })
  const rows = useMemo(
    () =>
      query.data?.filter((item) =>
        `${item.name} ${item.url} ${Object.values(item.labels ?? {})}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('mcps.eyebrow')}
        title={t('mcps.title')}
        description={t('mcps.subtitle')}
        actions={
          <>
            <ManifestImportButton
              label={t('common.importManifest')}
              onImport={(source) => {
                try {
                  setSelected(manifestToMcp(source))
                } catch (error) {
                  window.alert(error instanceof Error ? error.message : String(error))
                }
              }}
            />
            <Button onClick={() => setSelected(blankMcp())}>
              <Plus size={16} />
              {t('mcps.new')}
            </Button>
          </>
        }
      />
      <div className="contract-warning contract-warning--info">
        <KeyRound size={16} />
        <span>{t('mcps.secretNotice')}</span>
      </div>
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('mcps.search')}
          />
        </label>
        <span className="result-count">{t('mcps.serverCount', { count: rows.length })}</span>
      </div>
      {query.isLoading ? (
        <LoadingState rows={4} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState title={t('common.noData')} />
      ) : (
        <div className="integration-grid">
          {rows.map((item) => (
            <article className="integration-card" key={item.id}>
              <header>
                <span className="resource-card__icon">
                  <Network size={20} />
                </span>
                <StatusBadge status={item.enabled ? 'ACTIVE' : 'INACTIVE'} />
              </header>
              <button type="button" onClick={() => setSelected(structuredClone(item))}>
                <h2>{item.name}</h2>
                <p>
                  <Globe2 size={14} />
                  {item.url || t('mcps.endpointMissing')}
                </p>
                <dl>
                  <div>
                    <dt>{t('mcps.transport')}</dt>
                    <dd>{item.type}</dd>
                  </div>
                  <div>
                    <dt>{t('mcps.secrets')}</dt>
                    <dd>
                      {Object.keys(item.header_refs ?? {}).length +
                        Object.keys(item.env_refs ?? {}).length}{' '}
                      {t('mcps.masked')}
                    </dd>
                  </div>
                </dl>
              </button>
              <footer>
                <span className="tag-row">
                  {Object.values(item.labels ?? {}).map((value) => (
                    <span key={value}>{value}</span>
                  ))}
                </span>
                <IconButton
                  label={t('common.delete')}
                  onClick={() => {
                    if (window.confirm(t('common.confirmDelete'))) remove.mutate(item.name)
                  }}
                >
                  <Trash2 size={15} />
                </IconButton>
              </footer>
            </article>
          ))}
        </div>
      )}
      <McpDrawer
        key={selected ? selected.id || 'new' : 'closed'}
        value={selected}
        onClose={() => setSelected(null)}
        onSave={(item, isNew) => save.mutate({ item, isNew })}
        saving={save.isPending}
        error={save.error}
      />
    </div>
  )
}

function McpDrawer({
  value,
  saving,
  error,
  onClose,
  onSave,
}: {
  value: McpServer | null
  saving: boolean
  error: unknown
  onClose: () => void
  onSave: (value: McpServer, isNew: boolean) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<McpServer | null>(() =>
    value ? structuredClone(value) : null,
  )
  const [headers, setHeaders] = useState(() => JSON.stringify(value?.header_refs ?? {}, null, 2))
  const [env, setEnv] = useState(() => JSON.stringify(value?.env_refs ?? {}, null, 2))
  const current = draft
  const update = (updates: Partial<McpServer>) => current && setDraft({ ...current, ...updates })
  const submit = () => {
    if (!current?.name || !current.url) return
    try {
      onSave(
        {
          ...current,
          header_refs: JSON.parse(headers),
          env_refs: JSON.parse(env),
          type: 'streamable-http',
        },
        !current.id,
      )
    } catch {
      window.alert(t('errors.invalidJson'))
    }
  }
  return (
    <Drawer
      open={Boolean(value)}
      title={current?.id ? current.name : t('mcps.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button disabled={saving || !current?.name || !current.url} onClick={submit}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="form-stack">
        {error != null && <ErrorState error={error} />}
        <div className="contract-warning contract-warning--info">
          <Server size={15} />
          <span>{t('mcps.transportNotice')}</span>
        </div>
        <label className="field">
          <span className="field__label">{t('common.name')}</span>
          <input
            value={current?.name ?? ''}
            disabled={Boolean(current?.id)}
            onChange={(event) => update({ name: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('mcps.endpoint')}</span>
          <input
            type="url"
            value={current?.url ?? ''}
            onChange={(event) => update({ url: event.target.value })}
            placeholder="https://mcp.example.com/mcp"
          />
        </label>
        <label className="switch-row">
          <span>
            <strong>{t('common.enabled')}</strong>
            <small>{t('mcps.allowConnect')}</small>
          </span>
          <input
            type="checkbox"
            checked={current?.enabled ?? false}
            onChange={(event) => update({ enabled: event.target.checked })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('mcps.credentials')}</span>
          <textarea
            data-testid="mcp-header-refs"
            className="code-input"
            rows={8}
            value={headers}
            onChange={(event) => setHeaders(event.target.value)}
          />
          <span className="field__hint">{t('mcps.replacementHint')}</span>
        </label>
        <label className="field">
          <span className="field__label">{t('skills.environment')}</span>
          <textarea
            data-testid="mcp-env-refs"
            className="code-input"
            rows={7}
            value={env}
            onChange={(event) => setEnv(event.target.value)}
          />
        </label>
      </div>
    </Drawer>
  )
}
