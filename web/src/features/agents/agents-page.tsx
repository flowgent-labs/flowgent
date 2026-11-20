import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Cpu, Plus, Search, SlidersHorizontal, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
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
import type { Agent } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToAgent } from '../resources/manifest'

const agentSchema = z.object({
  name: z
    .string()
    .min(2)
    .regex(/^[a-zA-Z0-9-_]+$/),
  description: z.string(),
  model: z.string().min(1),
  soul: z.string().min(1),
  instruction: z.string(),
  temperature: z.coerce.number().min(0).max(2),
  max_tokens: z.coerce.number().int().positive(),
  input_schema: z.string(),
  output_schema: z.string(),
})
type AgentForm = z.infer<typeof agentSchema>

export function AgentsPage() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Agent | 'new' | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'agents'],
    queryFn: ({ signal }) => repositories.agents.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: ({
      agent,
      isNew,
      originalName,
    }: {
      agent: Agent
      isNew: boolean
      originalName?: string
    }) => repositories.agents.save(namespace, agent, isNew, originalName),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'agents'] })
    },
  })
  const remove = useMutation({
    mutationFn: (name: string) => repositories.agents.remove(namespace, name),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'agents'] }),
  })
  const rows = useMemo(
    () =>
      query.data?.filter((agent) =>
        `${agent.name} ${agent.description} ${agent.model}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('agents.eyebrow')}
        title={t('agents.title')}
        description={t('agents.subtitle')}
        actions={
          <>
            <ManifestImportButton
              label={t('common.importManifest')}
              onImport={(source) => {
                try {
                  setSelected(manifestToAgent(source))
                } catch (error) {
                  window.alert(error instanceof Error ? error.message : String(error))
                }
              }}
            />
            <Button data-testid="agent-create" onClick={() => setSelected('new')}>
              <Plus size={16} />
              {t('agents.new')}
            </Button>
          </>
        }
      />
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('agents.search')}
          />
        </label>
        <div className="toolbar__end">
          <span className="result-count">{t('agents.agentCount', { count: rows.length })}</span>
        </div>
      </div>
      {query.isLoading ? (
        <LoadingState rows={4} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState title={t('common.noData')} />
      ) : (
        <div className="resource-card-grid">
          {rows.map((agent) => (
            <article
              className="resource-card"
              data-testid={`agent-card-${agent.name}`}
              key={agent.id}
            >
              <header>
                <span className="resource-card__icon">
                  <Bot size={20} />
                </span>
                <StatusBadge status={agent.status} />
              </header>
              <button
                type="button"
                className="resource-card__body"
                onClick={() => setSelected(agent)}
              >
                <h2>{agent.name}</h2>
                <p>{agent.description || agent.soul}</p>
                <dl>
                  <div>
                    <dt>
                      <Cpu size={13} />
                      {t('agents.model')}
                    </dt>
                    <dd>{agent.model}</dd>
                  </div>
                  <div>
                    <dt>
                      <SlidersHorizontal size={13} />
                      {t('agents.temperature')}
                    </dt>
                    <dd>{agent.temperature ?? 'default'}</dd>
                  </div>
                </dl>
              </button>
              <footer>
                <div className="tag-row">
                  <span data-testid={`agent-version-${agent.name}`}>v{agent.revision}</span>
                  {Object.entries(agent.labels ?? {}).map(([key, value]) => (
                    <span key={key}>
                      {key}:{value}
                    </span>
                  ))}
                </div>
                <IconButton
                  data-testid={`agent-delete-${agent.name}`}
                  label={t('common.delete')}
                  onClick={() => {
                    if (window.confirm(t('common.confirmDelete'))) remove.mutate(agent.name)
                  }}
                >
                  <Trash2 size={15} />
                </IconButton>
              </footer>
            </article>
          ))}
        </div>
      )}
      <AgentDrawer
        key={selected === 'new' ? 'new' : (selected?.id ?? 'closed')}
        agent={selected === 'new' ? undefined : (selected ?? undefined)}
        open={Boolean(selected)}
        saving={save.isPending}
        error={save.error}
        onClose={() => setSelected(null)}
        onSave={(agent, isNew, originalName) => save.mutate({ agent, isNew, originalName })}
      />
    </div>
  )
}

function AgentDrawer({
  agent,
  open,
  saving,
  error,
  onClose,
  onSave,
}: {
  agent?: Agent
  open: boolean
  saving: boolean
  error: unknown
  onClose: () => void
  onSave: (agent: Agent, isNew: boolean, originalName?: string) => void
}) {
  const { t } = useTranslation()
  const isNew = !agent?.id
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isDirty },
  } = useForm<AgentForm>({ resolver: zodResolver(agentSchema), defaultValues: toForm(agent) })
  const submit = handleSubmit((values) => {
    let inputSchema: Record<string, unknown>
    let outputSchema: Record<string, unknown>
    try {
      inputSchema = JSON.parse(values.input_schema) as Record<string, unknown>
      outputSchema = JSON.parse(values.output_schema) as Record<string, unknown>
    } catch {
      setError('input_schema', { message: t('errors.invalidJson') })
      setError('output_schema', { message: t('errors.invalidJson') })
      return
    }
    onSave(
      {
        id: agent?.id ?? '',
        namespace_id: agent?.namespace_id,
        status: agent?.status ?? 'ACTIVE',
        created_at: agent?.created_at ?? '',
        updated_at: agent?.updated_at ?? '',
        name: values.name,
        description: values.description,
        model: values.model,
        soul: values.soul,
        instruction: values.instruction,
        input_schema: inputSchema,
        temperature: values.temperature,
        max_tokens: values.max_tokens,
        output_schema: outputSchema,
        revision: agent?.revision ?? 1,
        labels: agent?.labels ?? {},
      },
      isNew,
      agent?.name,
    )
  })
  return (
    <Drawer
      open={open}
      title={isNew ? t('agents.new') : (agent?.name ?? '')}
      onClose={() => {
        if (!isDirty || window.confirm(t('flows.unsaved'))) onClose()
      }}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button data-testid="agent-save" onClick={() => void submit()} disabled={saving}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <form className="form-stack" onSubmit={submit}>
        {error != null && <ErrorState error={error} />}
        <div className="contract-warning contract-warning--info">
          <Bot size={15} />
          <span>{t('agents.partialUpdate')}</span>
        </div>
        <label className="field">
          <span className="field__label">{t('common.name')}</span>
          <input data-testid="agent-name" pattern="[a-zA-Z0-9_-]+" {...register('name')} />
          {errors.name && <span className="field__error">{errors.name.message}</span>}
        </label>
        <label className="field">
          <span className="field__label">{t('common.description')}</span>
          <input data-testid="agent-description" {...register('description')} />
        </label>
        <label className="field">
          <span className="field__label">{t('agents.model')}</span>
          <input data-testid="agent-model" {...register('model')} />
          {errors.model && <span className="field__error">{errors.model.message}</span>}
        </label>
        <label className="field">
          <span className="field__label">{t('agents.soul')}</span>
          <textarea data-testid="agent-soul" rows={4} {...register('soul')} />
          {errors.soul && <span className="field__error">{errors.soul.message}</span>}
        </label>
        <label className="field">
          <span className="field__label">{t('agents.instruction')}</span>
          <textarea data-testid="agent-instruction" rows={9} {...register('instruction')} />
          {errors.instruction && <span className="field__error">{errors.instruction.message}</span>}
        </label>
        <div className="form-grid">
          <label className="field">
            <span className="field__label">{t('agents.temperature')}</span>
            <input type="number" step="0.05" {...register('temperature')} />
          </label>
          <label className="field">
            <span className="field__label">{t('agents.maxTokens')}</span>
            <input type="number" {...register('max_tokens')} />
          </label>
        </div>
        <label className="field">
          <span className="field__label">{t('agents.inputSchema')}</span>
          <textarea
            data-testid="agent-input-schema"
            className="code-input"
            rows={9}
            {...register('input_schema')}
          />
          {errors.input_schema && (
            <span className="field__error">{errors.input_schema.message}</span>
          )}
        </label>
        <label className="field">
          <span className="field__label">{t('agents.schema')}</span>
          <textarea
            data-testid="agent-output-schema"
            className="code-input"
            rows={9}
            {...register('output_schema')}
          />
          {errors.output_schema && (
            <span className="field__error">{errors.output_schema.message}</span>
          )}
        </label>
      </form>
    </Drawer>
  )
}

function toForm(agent?: Agent): AgentForm {
  return {
    name: agent?.name ?? '',
    description: agent?.description ?? '',
    model: agent?.model ?? '',
    soul: agent?.soul ?? '',
    instruction: agent?.instruction ?? '',
    temperature: agent?.temperature ?? 0.2,
    max_tokens: agent?.max_tokens ?? 4096,
    input_schema: JSON.stringify(agent?.input_schema ?? { type: 'object' }, null, 2),
    output_schema: JSON.stringify(agent?.output_schema ?? { type: 'object' }, null, 2),
  }
}
