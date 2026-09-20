import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BellRing, KeyRound, Plus, Search, Send, ShieldCheck, Trash2 } from 'lucide-react'
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
import type { NotificationChannel, NotificationProvider } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

const providers: NotificationProvider[] = ['webhook', 'slack', 'dingtalk', 'telegram', 'email']

const blankChannel = (): NotificationChannel => ({
  id: '',
  name: '',
  provider: 'webhook',
  config: {},
  enabled: true,
  labels: {},
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
})

export function NotificationsPage() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<NotificationChannel | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'notifications'],
    queryFn: ({ signal }) => repositories.notifications.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: ({ item, isNew }: { item: NotificationChannel; isNew: boolean }) =>
      repositories.notifications.save(namespace, item, isNew),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'notifications'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.notifications.remove(namespace, id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'notifications'] }),
  })
  const test = useMutation({
    mutationFn: (id: string) =>
      repositories.notifications.test(namespace, id, t('notifications.testMessage')),
    onSuccess: (delivery) =>
      window.alert(t('notifications.testQueued', { id: delivery.delivery_id })),
  })
  const rows = useMemo(
    () =>
      query.data?.filter((item) =>
        `${item.name} ${item.provider}`.toLowerCase().includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('notifications.eyebrow')}
        title={t('notifications.title')}
        description={t('notifications.subtitle')}
        actions={
          <Button onClick={() => setSelected(blankChannel())}>
            <Plus size={16} />
            {t('notifications.new')}
          </Button>
        }
      />
      <div className="contract-warning contract-warning--info">
        <ShieldCheck size={16} />
        <span>{t('notifications.secretNotice')}</span>
      </div>
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('notifications.search')}
          />
        </label>
        <span className="result-count">
          {t('notifications.channelCount', { count: rows.length })}
        </span>
      </div>
      {query.isLoading ? (
        <LoadingState rows={4} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState title={t('notifications.empty')} />
      ) : (
        <div className="integration-grid">
          {rows.map((item) => (
            <article className="integration-card" key={item.id} data-testid="notification-card">
              <header>
                <span className="resource-card__icon resource-card__icon--purple">
                  <BellRing size={20} />
                </span>
                <StatusBadge status={item.enabled ? 'ACTIVE' : 'INACTIVE'} />
              </header>
              <button type="button" onClick={() => setSelected(structuredClone(item))}>
                <h2>{item.name}</h2>
                <p>{item.provider}</p>
                <dl>
                  <div>
                    <dt>
                      <KeyRound size={13} />
                      {t('notifications.secrets')}
                    </dt>
                    <dd>{item.configured_secret_fields?.length ?? 0}</dd>
                  </div>
                  <div>
                    <dt>{t('common.updated')}</dt>
                    <dd>
                      {item.updated_at ? new Date(item.updated_at).toLocaleDateString() : '—'}
                    </dd>
                  </div>
                </dl>
              </button>
              <footer>
                <Button
                  variant="secondary"
                  disabled={test.isPending || !item.enabled}
                  onClick={() => test.mutate(item.id)}
                >
                  <Send size={14} />
                  {t('notifications.test')}
                </Button>
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
      <NotificationDrawer
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

function NotificationDrawer({
  value,
  saving,
  error,
  onClose,
  onSave,
}: {
  value: NotificationChannel | null
  saving: boolean
  error: unknown
  onClose: () => void
  onSave: (value: NotificationChannel, isNew: boolean) => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<NotificationChannel | null>(() =>
    value ? structuredClone(value) : null,
  )
  const current = draft
  const configured = new Set(current?.configured_secret_fields ?? [])
  const cleared = new Set(current?.clear_secret_fields ?? [])
  const update = (updates: Partial<NotificationChannel>) =>
    current && setDraft({ ...current, ...updates })
  const setConfig = (key: string, next: unknown) => {
    if (!current) return
    const clear = new Set(current.clear_secret_fields ?? [])
    clear.delete(key)
    setDraft({
      ...current,
      config: { ...current.config, [key]: next },
      clear_secret_fields: [...clear],
    })
  }
  const clearSecret = (key: string) => {
    if (!current) return
    const config = { ...current.config }
    delete config[key]
    setDraft({
      ...current,
      config,
      clear_secret_fields: [...new Set([...(current.clear_secret_fields ?? []), key])],
    })
  }
  const setProvider = (provider: NotificationProvider) =>
    current &&
    setDraft({
      ...current,
      provider,
      config: {},
      configured_secret_fields: [],
      clear_secret_fields: [],
    })
  const submit = () => {
    if (!current) return
    const config = { ...current.config }
    if (
      current.provider === 'webhook' &&
      typeof config.headers === 'string' &&
      config.headers.trim()
    ) {
      try {
        config.headers = JSON.parse(config.headers)
      } catch {
        window.alert(t('errors.invalidJson'))
        return
      }
    }
    onSave({ ...current, config }, !current.id)
  }

  return (
    <Drawer
      open={Boolean(value)}
      title={current?.id ? current.name : t('notifications.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            data-testid="notification-save"
            disabled={saving || !current?.name}
            onClick={submit}
          >
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="form-stack">
        {error != null && <ErrorState error={error} />}
        <div className="contract-warning contract-warning--info">
          <KeyRound size={15} />
          <span>{t('notifications.writeOnlyHint')}</span>
        </div>
        <label className="field">
          <span className="field__label">{t('common.name')}</span>
          <input
            data-testid="notification-name"
            value={current?.name ?? ''}
            onChange={(event) => update({ name: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('notifications.provider')}</span>
          <select
            data-testid="notification-provider"
            value={current?.provider ?? 'webhook'}
            disabled={Boolean(current?.id)}
            onChange={(event) => setProvider(event.target.value as NotificationProvider)}
          >
            {providers.map((provider) => (
              <option key={provider}>{provider}</option>
            ))}
          </select>
        </label>
        {current && (
          <ProviderFields
            channel={current}
            configured={configured}
            cleared={cleared}
            setConfig={setConfig}
            clearSecret={clearSecret}
          />
        )}
        <label className="switch-row">
          <span>
            <strong>{t('common.enabled')}</strong>
            <small>{t('notifications.enabledHint')}</small>
          </span>
          <input
            type="checkbox"
            checked={current?.enabled ?? false}
            onChange={(event) => update({ enabled: event.target.checked })}
          />
        </label>
      </div>
    </Drawer>
  )
}

function ProviderFields({
  channel,
  configured,
  cleared,
  setConfig,
  clearSecret,
}: {
  channel: NotificationChannel
  configured: Set<string>
  cleared: Set<string>
  setConfig: (key: string, value: unknown) => void
  clearSecret: (key: string) => void
}) {
  const { t } = useTranslation()
  const value = (key: string) => String(channel.config[key] ?? '')
  const text = (key: string, label: string, type = 'text') => (
    <label className="field" key={key}>
      <span className="field__label">{label}</span>
      <input
        type={type}
        value={value(key)}
        onChange={(event) =>
          setConfig(key, type === 'number' ? Number(event.target.value) : event.target.value)
        }
      />
    </label>
  )
  const secret = (key: string, label: string) => (
    <div className="field" key={key}>
      <span className="field__label">{label}</span>
      <input
        data-testid={`notification-secret-${key}`}
        type="password"
        value={value(key)}
        placeholder={
          configured.has(key) && !cleared.has(key) ? t('notifications.configuredPlaceholder') : ''
        }
        onChange={(event) => setConfig(key, event.target.value)}
      />
      {configured.has(key) && !cleared.has(key) && (
        <button className="text-link" type="button" onClick={() => clearSecret(key)}>
          {t('notifications.clearSecret')}
        </button>
      )}
    </div>
  )

  switch (channel.provider) {
    case 'telegram':
      return (
        <>
          {secret('bot_token', 'Bot token')}
          {text('chat_id', 'Chat ID')}
          {text('base_url', 'API base URL', 'url')}
        </>
      )
    case 'dingtalk':
      return (
        <>
          {secret('webhook_url', 'Webhook URL')}
          {secret('secret', 'Signing secret')}
        </>
      )
    case 'slack':
      return (
        <>
          {secret('webhook_url', 'Webhook URL')}
          {text('channel', 'Channel')}
        </>
      )
    case 'email':
      return (
        <>
          <div className="form-grid">
            {text('smtp_host', 'SMTP host')}
            {text('smtp_port', 'SMTP port', 'number')}
          </div>
          {text('username', 'Username')}
          {secret('password', 'Password')}
          {text('from', 'From address', 'email')}
        </>
      )
    default:
      return (
        <>
          {secret('url', 'Webhook URL')}
          <label className="field">
            <span className="field__label">{t('notifications.headers')}</span>
            <textarea
              data-testid="notification-secret-headers"
              className="code-input"
              rows={6}
              value={typeof channel.config.headers === 'string' ? channel.config.headers : ''}
              placeholder={
                configured.has('headers') && !cleared.has('headers')
                  ? t('notifications.configuredPlaceholder')
                  : '{\n  "Authorization": "Bearer …"\n}'
              }
              onChange={(event) => setConfig('headers', event.target.value)}
            />
          </label>
        </>
      )
  }
}
