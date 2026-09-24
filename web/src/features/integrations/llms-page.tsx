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
  name: '',
  type: 'openai',
  enabled: true,
  base_uri: '',
  default_model: '',
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
        `${item.name} ${item.type} ${item.base_uri} ${item.default_model}`
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
            <Button data-testid="llm-create" onClick={() => setSelected(blankLlm())}>
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
            <article
              className="integration-card llm-card"
              data-testid={`llm-card-${item.name || item.id}`}
              key={item.id}
            >
              <header>
                <span className="resource-card__icon resource-card__icon--purple">
                  <BrainCircuit size={20} />
                </span>
                <StatusBadge status={item.enabled ? 'ACTIVE' : 'INACTIVE'} />
              </header>
              <button type="button" onClick={() => setSelected(structuredClone(item))}>
                <h2>{item.name}</h2>
                <p>
                  <Sparkles size={14} />
                  {item.type} · {item.default_model}
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
                <code>{endpointHostname(item.base_uri)}</code>
                <IconButton
                  data-testid={`llm-delete-${item.name || item.id}`}
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
    if (!current?.name || !current.base_uri || !current.default_model) return
    try {
      onSave({ ...current, models: JSON.parse(models) }, !current.id)
    } catch {
      window.alert(t('errors.invalidJson'))
    }
  }
  return (
    <Drawer
      open={Boolean(value)}
      title={current?.id ? current.name : t('llms.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            data-testid="llm-save"
            disabled={saving || !current?.name || !current.base_uri}
            onClick={submit}
          >
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
          <span className="field__label">{t('common.name')}</span>
          <input
            data-testid="llm-name"
            pattern="[a-zA-Z0-9_-]+"
            value={current?.name ?? ''}
            onChange={(event) => update({ name: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.type')}</span>
          <select
            data-testid="llm-type"
            value={current?.type ?? 'openai'}
            onChange={(event) => update({ type: event.target.value as LlmProvider['type'] })}
          >
            <option value="openai">OpenAI-compatible</option>
            <option value="anthropic">Anthropic</option>
            <option value="gemini">Gemini</option>
          </select>
        </label>
        <label className="field">
          <span className="field__label">{t('llms.endpoint')}</span>
          <input
            data-testid="llm-endpoint"
            type="url"
            value={current?.base_uri ?? ''}
            onChange={(event) => update({ base_uri: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('llms.defaultModel')}</span>
          <input
            data-testid="llm-default-model"
            value={current?.default_model ?? ''}
            onChange={(event) => update({ default_model: event.target.value })}
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
        <EnvReferences
          value={current?.env_refs ?? {}}
          onChange={(env_refs) => update({ env_refs })}
        />
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

function EnvReferences({
  value,
  onChange,
}: {
  value: Record<string, string>
  onChange: (value: Record<string, string>) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => JSON.stringify(value, null, 2))
  return (
    <label className="field">
      <span className="field__label">{t('skills.environment')} (JSON)</span>
      <textarea
        data-testid="llm-env-refs"
        className="code-input"
        rows={5}
        value={draft}
        onChange={(event) => {
          const next = event.target.value
          setDraft(next)
          try {
            onChange(JSON.parse(next) as Record<string, string>)
          } catch {
            // Do not overwrite the last valid value while the JSON is edited.
          }
        }}
      />
      <span className="field__hint">{t('llms.environmentReferenceHint')}</span>
    </label>
  )
}

function endpointHostname(endpoint: string) {
  try {
    return new URL(endpoint).hostname
  } catch {
    return endpoint || '—'
  }
}
