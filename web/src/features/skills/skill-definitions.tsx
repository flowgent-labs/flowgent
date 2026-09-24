import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Braces, FileCode2, FileText, Plus, Sparkles, Trash2, Upload } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '../../app/store'
import { useRepositories } from '../../app/providers'
import type { SkillDefinition } from '../../core/domain/types'
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

const blankSkill = (): SkillDefinition => ({
  id: '',
  name: '',
  description: '',
  instruction: '',
  model: '',
  tools: [],
  revision: 1,
  assets: [],
  scripts: [],
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
})

export function SkillDefinitions() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<SkillDefinition | 'new' | null>(null)
  const [search, setSearch] = useState('')
  const query = useQuery({
    queryKey: [namespace, 'skill-definitions'],
    queryFn: ({ signal }) => repositories.skills.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: ({
      item,
      isNew,
      originalName,
    }: {
      item: SkillDefinition
      isNew: boolean
      originalName?: string
    }) => repositories.skills.save(namespace, item, isNew, originalName),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'skill-definitions'] })
    },
  })
  const remove = useMutation({
    mutationFn: (name: string) => repositories.skills.remove(namespace, name),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: [namespace, 'skill-definitions'] }),
  })
  const rows = useMemo(
    () =>
      query.data?.filter((item) =>
        `${item.name} ${item.description} ${item.instruction}`
          .toLowerCase()
          .includes(search.toLowerCase()),
      ) ?? [],
    [query.data, search],
  )
  return (
    <section className="page-stack" data-testid="skill-definitions">
      <PageHeader
        eyebrow={t('skills.eyebrow')}
        title={t('skills.title')}
        description={t('skills.subtitle')}
        actions={
          <Button data-testid="skill-create" onClick={() => setSelected('new')}>
            <Plus size={16} />
            {t('skills.new')}
          </Button>
        }
      />
      <div className="toolbar">
        <label className="search-control">
          <Sparkles size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('skills.search')}
          />
        </label>
        <span className="result-count">{t('skills.count', { count: rows.length })}</span>
      </div>
      {query.isLoading ? (
        <LoadingState rows={3} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState title={t('skills.noDefinitions')} />
      ) : (
        <div className="resource-card-grid">
          {rows.map((skill) => (
            <article
              className="resource-card"
              data-testid={`skill-card-${skill.name}`}
              key={skill.id}
            >
              <header>
                <span className="resource-card__icon resource-card__icon--gold">
                  <Sparkles size={20} />
                </span>
                <StatusBadge status={skill.status || 'ACTIVE'} />
              </header>
              <button
                type="button"
                className="resource-card__body"
                onClick={() => setSelected(skill)}
              >
                <h2>{skill.name}</h2>
                <p>{skill.description || skill.instruction}</p>
                <dl>
                  <div>
                    <dt>
                      <FileText size={13} />
                      {t('skills.assets')}
                    </dt>
                    <dd>{skill.assets?.length ?? 0}</dd>
                  </div>
                  <div>
                    <dt>
                      <FileCode2 size={13} />
                      {t('skills.scripts')}
                    </dt>
                    <dd>{skill.scripts?.length ?? 0}</dd>
                  </div>
                </dl>
              </button>
              <footer>
                <code>v{skill.revision}</code>
                <IconButton
                  data-testid={`skill-delete-${skill.name}`}
                  label={t('common.delete')}
                  onClick={() => {
                    if (window.confirm(t('common.confirmDelete'))) remove.mutate(skill.name)
                  }}
                >
                  <Trash2 size={15} />
                </IconButton>
              </footer>
            </article>
          ))}
        </div>
      )}
      <SkillDrawer
        key={selected === 'new' ? 'new' : (selected?.id ?? 'closed')}
        value={selected === 'new' ? blankSkill() : selected}
        saving={save.isPending}
        error={save.error}
        onClose={() => setSelected(null)}
        onSave={(item, isNew, originalName) => save.mutate({ item, isNew, originalName })}
      />
    </section>
  )
}

function SkillDrawer({
  value,
  saving,
  error,
  onClose,
  onSave,
}: {
  value: SkillDefinition | null
  saving: boolean
  error: unknown
  onClose: () => void
  onSave: (item: SkillDefinition, isNew: boolean, originalName?: string) => void
}) {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<SkillDefinition | null>(() =>
    value ? structuredClone(value) : null,
  )
  const [tools, setTools] = useState(() => JSON.stringify(value?.tools ?? [], null, 2))
  const upload = useMutation({
    mutationFn: ({ kind, file }: { kind: 'assets' | 'scripts'; file: File }) => {
      if (!draft) throw new Error('skill is not selected')
      return repositories.skills.upload(namespace, draft.name, kind, file)
    },
    onSuccess: (item) => {
      setDraft(item)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'skill-definitions'] })
    },
  })
  const current = draft
  const update = (changes: Partial<SkillDefinition>) =>
    current && setDraft({ ...current, ...changes })
  const submit = () => {
    if (!current?.name || !current.instruction) return
    try {
      onSave({ ...current, tools: JSON.parse(tools) as string[] }, !current.id, value?.name)
    } catch {
      window.alert(t('errors.invalidJson'))
    }
  }
  const selectFile = (kind: 'assets' | 'scripts', file?: File) => {
    if (file) upload.mutate({ kind, file })
  }
  return (
    <Drawer
      open={Boolean(value)}
      title={current?.id ? `${current.name} · v${current.revision}` : t('skills.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            data-testid="skill-save"
            disabled={saving || !current?.name || !current.instruction}
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
          <Braces size={15} />
          <span>{t('skills.revisionNotice')}</span>
          {current?.id && <code data-testid="skill-current-revision">v{current.revision}</code>}
        </div>
        <label className="field">
          <span className="field__label">{t('common.name')}</span>
          <input
            data-testid="skill-name"
            pattern="[a-zA-Z0-9_-]+"
            value={current?.name ?? ''}
            onChange={(event) => update({ name: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('common.description')}</span>
          <input
            data-testid="skill-description"
            value={current?.description ?? ''}
            onChange={(event) => update({ description: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('skills.instruction')}</span>
          <textarea
            data-testid="skill-instruction"
            rows={9}
            value={current?.instruction ?? ''}
            onChange={(event) => update({ instruction: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('agents.model')}</span>
          <input
            data-testid="skill-model"
            value={current?.model ?? ''}
            onChange={(event) => update({ model: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('skills.tools')} (JSON)</span>
          <textarea
            data-testid="skill-tools"
            className="code-input"
            rows={5}
            value={tools}
            onChange={(event) => setTools(event.target.value)}
          />
        </label>
        {current?.id && (
          <>
            <FileUploadGroup
              title={t('skills.assets')}
              hint={t('skills.assetHint')}
              testId="skill-asset-file"
              files={current.assets}
              disabled={upload.isPending}
              onSelect={(file) => selectFile('assets', file)}
            />
            <FileUploadGroup
              title={t('skills.scripts')}
              hint={t('skills.scriptHint')}
              testId="skill-script-file"
              files={current.scripts}
              disabled={upload.isPending}
              onSelect={(file) => selectFile('scripts', file)}
            />
            {upload.isError && <ErrorState error={upload.error} />}
          </>
        )}
      </div>
    </Drawer>
  )
}

function FileUploadGroup({
  title,
  hint,
  files,
  testId,
  disabled,
  onSelect,
}: {
  title: string
  hint: string
  files: SkillDefinition['assets']
  testId: string
  disabled: boolean
  onSelect: (file?: File) => void
}) {
  return (
    <section className="field">
      <span className="field__label">{title}</span>
      <input
        data-testid={testId}
        type="file"
        disabled={disabled}
        onChange={(event) => onSelect(event.target.files?.[0])}
      />
      <span className="field__hint">{hint}</span>
      {files.length > 0 && (
        <div className="tag-row">
          {files.map((file) => (
            <span key={file.id} title={`${file.media_type} · ${file.content_hash}`}>
              <Upload size={12} /> {file.relative_path.split('/').at(-1)}
            </span>
          ))}
        </div>
      )}
    </section>
  )
}
