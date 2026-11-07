import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Braces, KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { Button, ErrorState, LoadingState, PageHeader } from '../../shared/components/ui'
import type { RuntimeConfigView } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

type RuntimeConfigScope = 'namespace' | 'flow'
type RuntimeConfigSection = 'environment' | 'secrets'
type EnvironmentRow = { id: string; key: string; value: string }

const environmentKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/

export function RuntimeConfigPage({
  scope,
  section,
}: {
  scope: RuntimeConfigScope
  section: RuntimeConfigSection
}) {
  const { namespaceId = '', flowId = '' } = useParams()
  const locale = useAppStore((state) => state.locale)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const queryKey = [namespaceId, 'runtime-config', scope, scope === 'flow' ? flowId : namespaceId]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      scope === 'flow'
        ? repositories.runtimeConfig.getFlow(namespaceId, flowId, signal)
        : repositories.runtimeConfig.getNamespace(namespaceId, signal),
  })
  const updateCached = (view: RuntimeConfigView) => queryClient.setQueryData(queryKey, view)

  if (query.isLoading) return <LoadingState rows={5} />
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  if (!query.data) return null

  return section === 'environment' ? (
    <EnvironmentSettings
      key={JSON.stringify(query.data.local.environment)}
      scope={scope}
      namespace={namespaceId}
      flowId={flowId}
      view={query.data}
      locale={locale}
      onSaved={updateCached}
    />
  ) : (
    <SecretSettings
      scope={scope}
      namespace={namespaceId}
      flowId={flowId}
      view={query.data}
      locale={locale}
      onSaved={updateCached}
    />
  )
}

function EnvironmentSettings({
  scope,
  namespace,
  flowId,
  view,
  locale,
  onSaved,
}: RuntimeConfigContentProps) {
  const repositories = useRepositories()
  const [rows, setRows] = useState<EnvironmentRow[]>(() =>
    Object.entries(view.local.environment).map(([key, value]) => ({
      id: crypto.randomUUID(),
      key,
      value,
    })),
  )
  const [validation, setValidation] = useState('')
  const save = useMutation({
    mutationFn: (environment: Record<string, string>) =>
      scope === 'flow'
        ? repositories.runtimeConfig.updateFlowEnvironment(namespace, flowId, environment)
        : repositories.runtimeConfig.updateNamespaceEnvironment(namespace, environment),
    onSuccess: onSaved,
  })
  const submit = () => {
    const environment: Record<string, string> = {}
    for (const row of rows) {
      const key = row.key.trim()
      if (!environmentKeyPattern.test(key) || key.length > 128) {
        setValidation(
          locale === 'zh' ? `环境变量名无效：${key}` : `Invalid environment key: ${key}`,
        )
        return
      }
      if (key in environment) {
        setValidation(
          locale === 'zh' ? `环境变量名重复：${key}` : `Duplicate environment key: ${key}`,
        )
        return
      }
      environment[key] = row.value
    }
    setValidation('')
    save.mutate(environment)
  }
  return (
    <div className="page-stack" data-testid={`${scope}-environment-settings`}>
      <PageHeader
        eyebrow={scope === 'flow' ? 'FLOW SETTINGS' : 'NAMESPACE SETTINGS'}
        title={locale === 'zh' ? '环境变量' : 'Environment variables'}
        description={
          scope === 'flow'
            ? locale === 'zh'
              ? 'Flow 自动继承 namespace 公共变量；同名 Flow 值优先。'
              : 'This Flow inherits namespace values automatically; local values win on conflicts.'
            : locale === 'zh'
              ? '这些公共变量会被 namespace 中的 Flow 自动继承。'
              : 'These common values are inherited by every Flow in this namespace.'
        }
      />
      {scope === 'flow' && <InheritanceNotice locale={locale} />}
      <section className="surface runtime-config-card">
        <header className="surface__header">
          <div>
            <span className="surface__eyebrow">{locale === 'zh' ? '本地值' : 'LOCAL VALUES'}</span>
            <h2>{locale === 'zh' ? '环境变量' : 'Environment'}</h2>
          </div>
          <Braces size={20} />
        </header>
        <div className="runtime-config-rows">
          {rows.map((row, index) => (
            <div className="runtime-config-row" key={row.id}>
              <input
                aria-label={`Environment key ${index + 1}`}
                data-testid={`runtime-env-key-${index}`}
                placeholder="VARIABLE_NAME"
                value={row.key}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item) =>
                      item.id === row.id ? { ...item, key: event.target.value } : item,
                    ),
                  )
                }
              />
              <input
                aria-label={`Environment value ${index + 1}`}
                data-testid={`runtime-env-value-${index}`}
                placeholder={locale === 'zh' ? '值' : 'Value'}
                value={row.value}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item) =>
                      item.id === row.id ? { ...item, value: event.target.value } : item,
                    ),
                  )
                }
              />
              <Button
                variant="ghost"
                size="sm"
                aria-label={locale === 'zh' ? '删除变量' : 'Remove variable'}
                onClick={() => setRows((current) => current.filter((item) => item.id !== row.id))}
              >
                <Trash2 size={15} />
              </Button>
            </div>
          ))}
        </div>
        <div className="form-actions form-actions--spread">
          <Button
            variant="secondary"
            onClick={() =>
              setRows((current) => [...current, { id: crypto.randomUUID(), key: '', value: '' }])
            }
          >
            <Plus size={15} />
            {locale === 'zh' ? '添加变量' : 'Add variable'}
          </Button>
          <Button data-testid="runtime-env-save" onClick={submit} disabled={save.isPending}>
            {locale === 'zh' ? '保存变量' : 'Save variables'}
          </Button>
        </div>
        {(validation || save.isError) && (
          <p className="inline-error">{validation || String(save.error)}</p>
        )}
      </section>
      <EffectiveConfiguration view={view} section="environment" scope={scope} locale={locale} />
    </div>
  )
}

function SecretSettings({
  scope,
  namespace,
  flowId,
  view,
  locale,
  onSaved,
}: RuntimeConfigContentProps) {
  const repositories = useRepositories()
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [validation, setValidation] = useState('')
  const update = useMutation({
    mutationFn: (payload: { secrets?: Record<string, string>; clear_secret_keys?: string[] }) =>
      scope === 'flow'
        ? repositories.runtimeConfig.updateFlowSecrets(namespace, flowId, payload)
        : repositories.runtimeConfig.updateNamespaceSecrets(namespace, payload),
    onSuccess: (saved) => {
      setKey('')
      setValue('')
      onSaved(saved)
    },
  })
  const add = () => {
    const trimmed = key.trim()
    if (!environmentKeyPattern.test(trimmed) || trimmed.length > 128 || !value) {
      setValidation(
        locale === 'zh'
          ? '请输入合法密钥名和非空密钥值。'
          : 'Enter a valid key and a non-empty secret value.',
      )
      return
    }
    setValidation('')
    update.mutate({ secrets: { [trimmed]: value } })
  }
  return (
    <div className="page-stack" data-testid={`${scope}-secret-settings`}>
      <PageHeader
        eyebrow={scope === 'flow' ? 'FLOW SETTINGS' : 'NAMESPACE SETTINGS'}
        title={locale === 'zh' ? '密钥' : 'Secrets'}
        description={
          scope === 'flow'
            ? locale === 'zh'
              ? 'Flow 继承 namespace 密钥；本地同名密钥覆盖继承值。'
              : 'This Flow inherits namespace secrets; a local key overrides the inherited value.'
            : locale === 'zh'
              ? '密钥使用认证加密存储，只能写入或替换，无法从 UI 读回。'
              : 'Secrets are authenticated-encrypted at rest and are write-only in the UI.'
        }
      />
      <section className="contract-warning">
        <ShieldCheck size={18} />
        <span>
          {locale === 'zh'
            ? 'API 只返回已配置的密钥名称，永不返回明文或加密信封。'
            : 'The API returns configured key names only—never plaintext or encrypted envelopes.'}
        </span>
      </section>
      {scope === 'flow' && <InheritanceNotice locale={locale} />}
      <section className="surface runtime-config-card">
        <header className="surface__header">
          <div>
            <span className="surface__eyebrow">
              {locale === 'zh' ? '写入型字段' : 'WRITE ONLY'}
            </span>
            <h2>{locale === 'zh' ? '添加或替换密钥' : 'Add or replace a secret'}</h2>
          </div>
          <KeyRound size={20} />
        </header>
        <div className="runtime-secret-form">
          <label>
            <span>{locale === 'zh' ? '名称' : 'Name'}</span>
            <input
              data-testid="runtime-secret-key"
              placeholder="SECRET_NAME"
              value={key}
              onChange={(event) => setKey(event.target.value)}
            />
          </label>
          <label>
            <span>{locale === 'zh' ? '密钥值' : 'Secret value'}</span>
            <input
              data-testid="runtime-secret-value"
              type="password"
              autoComplete="new-password"
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          </label>
          <Button data-testid="runtime-secret-save" onClick={add} disabled={update.isPending}>
            {locale === 'zh' ? '保存密钥' : 'Save secret'}
          </Button>
        </div>
        {(validation || update.isError) && (
          <p className="inline-error">{validation || String(update.error)}</p>
        )}
        <div className="runtime-secret-list">
          {view.local.secret_keys.map((secretKey) => (
            <div key={secretKey} data-testid={`local-secret-${secretKey}`}>
              <code>{secretKey}</code>
              <span>{locale === 'zh' ? '本地覆盖' : 'Local override'}</span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => update.mutate({ clear_secret_keys: [secretKey] })}
                disabled={update.isPending}
              >
                {scope === 'flow'
                  ? locale === 'zh'
                    ? '移除覆盖'
                    : 'Remove override'
                  : locale === 'zh'
                    ? '删除'
                    : 'Remove'}
              </Button>
            </div>
          ))}
        </div>
      </section>
      <EffectiveConfiguration view={view} section="secrets" scope={scope} locale={locale} />
    </div>
  )
}

interface RuntimeConfigContentProps {
  scope: RuntimeConfigScope
  namespace: string
  flowId: string
  view: RuntimeConfigView
  locale: 'en' | 'zh'
  onSaved: (view: RuntimeConfigView) => void
}

function InheritanceNotice({ locale }: { locale: 'en' | 'zh' }) {
  return (
    <section className="contract-warning contract-warning--info">
      <ShieldCheck size={17} />
      <span>
        {locale === 'zh'
          ? '继承是动态解析的；namespace 更新会作用于未覆盖该键的所有 Flow。'
          : 'Inheritance is resolved dynamically; namespace changes reach every Flow that has not overridden the key.'}
      </span>
    </section>
  )
}

function EffectiveConfiguration({
  view,
  section,
  scope,
  locale,
}: {
  view: RuntimeConfigView
  section: RuntimeConfigSection
  scope: RuntimeConfigScope
  locale: 'en' | 'zh'
}) {
  const localKeys = useMemo(
    () =>
      new Set(
        section === 'environment' ? Object.keys(view.local.environment) : view.local.secret_keys,
      ),
    [section, view.local.environment, view.local.secret_keys],
  )
  const values =
    section === 'environment'
      ? Object.entries(view.effective.environment)
      : view.effective.secret_keys.map((key) => [key, '••••••••'] as const)
  return (
    <section className="surface runtime-effective" data-testid={`effective-${section}`}>
      <header className="surface__header">
        <div>
          <span className="surface__eyebrow">
            {locale === 'zh' ? '运行时解析结果' : 'EFFECTIVE AT RUNTIME'}
          </span>
          <h2>{locale === 'zh' ? '有效配置' : 'Effective configuration'}</h2>
        </div>
      </header>
      {!values.length ? (
        <p className="muted-copy">{locale === 'zh' ? '尚未配置。' : 'Nothing configured yet.'}</p>
      ) : (
        <div className="runtime-effective__list">
          {values.map(([key, value]) => (
            <div key={key}>
              <code>{key}</code>
              <span>{value}</span>
              <small>
                {scope === 'flow' && !localKeys.has(key)
                  ? locale === 'zh'
                    ? '继承自 namespace'
                    : 'Inherited from namespace'
                  : locale === 'zh'
                    ? '本地值'
                    : 'Local value'}
              </small>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
