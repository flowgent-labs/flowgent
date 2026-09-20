import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BrainCircuit, Gauge, KeyRound, Plus, Search, Sparkles, Trash2, Zap } from 'lucide-react'
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
import type { LlmProvider } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToLlm } from '../resources/manifest'

const blankLlm = (): LlmProvider => ({
  id: '',
  provider: '',
  enabled: true,
  endpoint: '',
  defaultModel: '',
  models: [],
  rate_limit: 60,
  timeout: '90s',
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
  api_key_env: '',
})

export function LlmsPage() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<LlmProvider | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'llms'],
    queryFn: ({ signal }) => repositories.llms.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: ({ item, isNew }: { item: LlmProvider; isNew: boolean }) =>
      repositories.llms.save(namespace, item, isNew),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'llms'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.llms.remove(namespace, id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'llms'] }),
  })
  const rows = useMemo(
    () =>
      query.data?.filter((item) =>
        `${item.provider} ${item.endpoint} ${item.defaultModel}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('llms.eyebrow')}
        title={t('llms.title')}
        description={t('llms.subtitle')}
        actions={
          <>
            <ManifestImportButton
              label={t('common.importManifest')}
              onImport={(source) => {
                try {
                  setSelected(manifestToLlm(source))
                } catch (error) {
                  window.alert(error instanceof Error ? error.message : String(error))
                }
              }}
            />
            <Button onClick={() => setSelected(blankLlm())}>
              <Plus size={16} />
              {t('llms.new')}
            </Button>
          </>
        }
      />
      <div className="contract-warning">
        <KeyRound size={16} />
        <span>{t('llms.secretNotice')}</span>
      </div>
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('llms.search')}
          />
        </label>
        <span className="result-count">{t('llms.providerCount', { count: rows.length })}</span>
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
            <article className="integration-card llm-card" key={item.id}>
              <header>
                <span className="resource-card__icon resource-card__icon--purple">
                  <BrainCircuit size={20} />
                </span>
                <StatusBadge status={item.enabled ? 'ACTIVE' : 'INACTIVE'} />
              </header>
              <button type="button" onClick={() => setSelected(structuredClone(item))}>
                <h2>{item.provider}</h2>
                <p>
                  <Sparkles size={14} />
                  {item.defaultModel}
                </p>
                <dl>
                  <div>
                    <dt>
                      <Zap size={13} />
                      {t('llms.models')}
                    </dt>
                    <dd>{item.models.length}</dd>
                  </div>
                  <div>
                    <dt>
                      <Gauge size={13} />
                      {t('llms.rateLimit')}
                    </dt>
                    <dd>{item.rate_limit ?? '—'} rpm</dd>
                  </div>
                </dl>
                <div className="capability-row">
                  {capabilities(item).map((capability) => (
                    <span key={capability}>{capability}</span>
                  ))}
                </div>
              </button>
              <footer>
                <code>{endpointHostname(item.endpoint)}</code>
                <IconButton
                  label={t('common.delete')}
                  onClick={() => {
                    if (window.confirm(t('common.confirmDelete'))) remove.mutate(item.id)
                  }}
                >
                  <Trash2 size={15} />
                </IconButton>
              </footer>
            </article>
          ))}
        </div>
      )}
      <LlmDrawer
        key={selected ? selected.id || 'new' : 'closed'}
        value={selected}
        saving={save.isPending}
        error={save.error}
        onClose={() => setSelected(null)}
        onSave={(item, isNew) => save.mutate({ item, isNew })}
      />
    </div>
  )
}

function LlmDrawer({
  value,
  saving,
  error,
  onClose,
  onSave,
}: {
  value: LlmProvider | null
  saving: boolean
  error: unknown
  onClose: () => void
  onSave: (value: LlmProvider, isNew: boolean) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<LlmProvider | null>(() =>
    value ? structuredClone(value) : null,
  )
  const [models, setModels] = useState(() => JSON.stringify(value?.models ?? [], null, 2))
  const current = draft
  const update = (updates: Partial<LlmProvider>) => current && setDraft({ ...current, ...updates })
  const submit = () => {
    if (!current?.provider || !current.endpoint || !current.defaultModel) return
    try {
      onSave({ ...current, models: JSON.parse(models) }, !current.id)
    } catch {
      window.alert(t('errors.invalidJson'))
    }
  }
  return (
    <Drawer
      open={Boolean(value)}
      title={current?.id ? current.provider : t('llms.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button disabled={saving || !current?.provider || !current.endpoint} onClick={submit}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="form-stack">
        {error != null && <ErrorState error={error} />}
        <div className="contract-warning">
          <KeyRound size={15} />
          <span>{t('llms.secretNotice')}</span>
        </div>
        <label className="field">
          <span className="field__label">{t('llms.provider')}</span>
          <input
            value={current?.provider ?? ''}
            onChange={(event) => update({ provider: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.endpoint')}</span>
          <input
            type="url"
            value={current?.endpoint ?? ''}
            onChange={(event) => update({ endpoint: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.defaultModel')}</span>
          <input
            value={current?.defaultModel ?? ''}
            onChange={(event) => update({ defaultModel: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.apiKeyEnvironment')}</span>
          <input
            data-testid="llm-api-key-env"
            value={current?.api_key_env ?? ''}
            onChange={(event) => update({ api_key_env: event.target.value.toUpperCase() })}
            placeholder="DEEPSEEK_API_KEY_FLOWGENT"
          />
          <span className="field__hint">{t('llms.environmentReferenceHint')}</span>
        </label>
        <div className="form-grid">
          <label className="field">
            <span className="field__label">{t('llms.rateLimit')}</span>
            <input
              type="number"
              value={current?.rate_limit ?? 60}
              onChange={(event) => update({ rate_limit: Number(event.target.value) })}
            />
          </label>
          <label className="field">
            <span className="field__label">{t('flows.timeout')}</span>
            <input
              value={current?.timeout ?? '90s'}
              onChange={(event) => update({ timeout: event.target.value })}
            />
          </label>
        </div>
        <label className="switch-row">
          <span>
            <strong>{t('common.enabled')}</strong>
            <small>{t('llms.availableToAgents')}</small>
          </span>
          <input
            type="checkbox"
            checked={current?.enabled ?? false}
            onChange={(event) => update({ enabled: event.target.checked })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.models')} (JSON)</span>
          <textarea
            className="code-input"
            rows={16}
            value={models}
            onChange={(event) => setModels(event.target.value)}
          />
        </label>
      </div>
    </Drawer>
  )
}

function capabilities(provider: LlmProvider) {
  const result = new Set<string>()
  provider.models.forEach((model) => {
    model.modalities?.input.forEach((value) => result.add(value))
    if (model.thinking) result.add('thinking')
  })
  return [...result]
}

function endpointHostname(endpoint: string) {
  try {
    return new URL(endpoint).hostname
  } catch {
    return endpoint || '—'
  }
}
