import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BookOpen, FileText, Plus, Search, Tag, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import {
  Button,
  Drawer,
  EmptyState,
  ErrorState,
  IconButton,
  LoadingState,
  PageHeader,
} from '../../shared/components/ui'
import type { KnowledgeEntry, MemoryScope } from '../../core/domain/types'
import { useAppStore } from '../../app/store'

const blankEntry = (): KnowledgeEntry => ({
  id: '',
  title: '',
  content: '',
  content_type: 'text/markdown',
  source: 'operator',
  source_ref: '',
  tags: [],
  metadata: {},
  status: 'ACTIVE',
  created_at: '',
  updated_at: '',
})

export function MemoryPage() {
  const { scope = 'run' } = useParams()
  const memoryScope = (['run', 'flow', 'share'].includes(scope) ? scope : 'run') as MemoryScope
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [tag, setTag] = useState('')
  const [draft, setDraft] = useState<KnowledgeEntry | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'knowledge', memoryScope],
    queryFn: ({ signal }) => repositories.knowledge.list(namespace, memoryScope, signal),
  })
  const save = useMutation({
    mutationFn: (entry: KnowledgeEntry) =>
      repositories.knowledge.save(namespace, memoryScope, entry, !entry.id),
    onSuccess: () => {
      setDraft(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'knowledge'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.knowledge.remove(namespace, id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: [namespace, 'knowledge'] }),
  })
  const rows = useMemo(
    () =>
      query.data?.filter(
        (entry) =>
          (!search ||
            `${entry.title} ${entry.content} ${entry.source_ref}`
              .toLowerCase()
              .includes(search.toLowerCase())) &&
          (!tag || entry.tags.includes(tag)),
      ) ?? [],
    [query.data, search, tag],
  )
  const tags = [...new Set(query.data?.flatMap((entry) => entry.tags) ?? [])]
  const subtitle = t(`memory.${memoryScope}Subtitle`)
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('memory.scopeEyebrow', { scope: memoryScope.toUpperCase() })}
        title={t('memory.title')}
        description={subtitle}
        actions={
          <Button data-testid="memory-create" onClick={() => setDraft(blankEntry())}>
            <Plus size={16} />
            {t('memory.new')}
          </Button>
        }
      />
      <div className="contract-warning contract-warning--info">
        <BookOpen size={16} />
        <span>{t('memory.scopeNotice')}</span>
      </div>
      <div className="toolbar">
        <label className="search-control">
          <Search size={16} />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('memory.search')}
          />
        </label>
        <label className="filter-control">
          <Tag size={15} />
          <select value={tag} onChange={(event) => setTag(event.target.value)}>
            <option value="">{t('memory.allTags')}</option>
            {tags.map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <span className="result-count">{t('memory.entryCount', { count: rows.length })}</span>
      </div>
      {query.isLoading ? (
        <LoadingState rows={5} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !rows.length ? (
        <EmptyState
          title={t('common.noData')}
          action={<Button onClick={() => setDraft(blankEntry())}>{t('memory.new')}</Button>}
        />
      ) : (
        <div className="knowledge-grid">
          {rows.map((entry) => (
            <article
              className="knowledge-card"
              data-testid={`memory-card-${entry.title}`}
              key={entry.id}
            >
              <button
                type="button"
                className="knowledge-card__main"
                onClick={() => setDraft(structuredClone(entry))}
              >
                <div className="knowledge-card__icon">
                  <FileText size={18} />
                </div>
                <div>
                  <div className="knowledge-card__meta">
                    <span>{entry.source}</span>
                    <span>{entry.content_type}</span>
                  </div>
                  <h2>{entry.title || t('memory.untitled')}</h2>
                  <p>{entry.content}</p>
                  <div className="tag-row">
                    {entry.tags.map((value) => (
                      <span key={value}>#{value}</span>
                    ))}
                  </div>
                </div>
              </button>
              <IconButton
                data-testid={`memory-delete-${entry.title}`}
                label={t('common.delete')}
                onClick={() => {
                  if (window.confirm(t('common.confirmDelete'))) remove.mutate(entry.id)
                }}
              >
                <Trash2 size={16} />
              </IconButton>
            </article>
          ))}
        </div>
      )}
      <KnowledgeDrawer
        key={draft ? draft.id || 'new' : 'closed'}
        draft={draft}
        scope={memoryScope}
        saving={save.isPending}
        onClose={() => setDraft(null)}
        onSave={(entry) => save.mutate(entry)}
      />
    </div>
  )
}

function KnowledgeDrawer({
  draft,
  scope,
  saving,
  onClose,
  onSave,
}: {
  draft: KnowledgeEntry | null
  scope: MemoryScope
  saving: boolean
  onClose: () => void
  onSave: (entry: KnowledgeEntry) => void
}) {
  const { t } = useTranslation()
  const [metadataText, setMetadataText] = useState(() =>
    JSON.stringify(draft?.metadata ?? {}, null, 2),
  )
  const [metadataError, setMetadataError] = useState('')
  const [value, setValue] = useState<KnowledgeEntry | null>(() =>
    draft ? structuredClone(draft) : null,
  )
  const current = value
  const update = (updates: Partial<KnowledgeEntry>) =>
    current && setValue({ ...current, ...updates })
  const submit = () => {
    if (!current?.title && !current?.content) return
    try {
      const metadata = JSON.parse(metadataText) as Record<string, unknown>
      setMetadataError('')
      onSave({ ...current, metadata: { ...metadata, scope } })
    } catch {
      setMetadataError(t('errors.invalidJson'))
    }
  }
  return (
    <Drawer
      open={Boolean(draft)}
      title={draft?.id ? draft.title : t('memory.new')}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            data-testid="memory-save"
            disabled={saving || (!current?.title && !current?.content)}
            onClick={submit}
          >
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="form-stack">
        <label className="field">
          <span className="field__label">{t('memory.entryTitle')}</span>
          <input
            data-testid="memory-title"
            value={current?.title ?? ''}
            onChange={(event) => update({ title: event.target.value })}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('memory.content')}</span>
          <textarea
            data-testid="memory-content"
            rows={13}
            value={current?.content ?? ''}
            onChange={(event) => update({ content: event.target.value })}
          />
        </label>
        <div className="form-grid">
          <label className="field">
            <span className="field__label">{t('memory.contentType')}</span>
            <select
              data-testid="memory-content-type"
              value={current?.content_type ?? 'text/markdown'}
              onChange={(event) => update({ content_type: event.target.value })}
            >
              <option>text/markdown</option>
              <option>text/plain</option>
              <option>application/json</option>
            </select>
          </label>
          <label className="field">
            <span className="field__label">{t('common.source')}</span>
            <input
              data-testid="memory-source-ref"
              value={current?.source_ref ?? ''}
              onChange={(event) => update({ source_ref: event.target.value })}
              placeholder={
                scope === 'run'
                  ? t('memory.runId')
                  : scope === 'flow'
                    ? t('memory.flowId')
                    : t('memory.documentId')
              }
            />
          </label>
        </div>
        <label className="field">
          <span className="field__label">{t('memory.tags')}</span>
          <input
            data-testid="memory-tags"
            value={current?.tags.join(', ') ?? ''}
            onChange={(event) =>
              update({
                tags: event.target.value
                  .split(',')
                  .map((item) => item.trim())
                  .filter(Boolean),
              })
            }
            placeholder={t('memory.tagsPlaceholder')}
          />
        </label>
        <label className="field">
          <span className="field__label">{t('memory.metadata')}</span>
          <textarea
            data-testid="memory-metadata"
            className="code-input"
            rows={8}
            value={metadataText}
            onChange={(event) => setMetadataText(event.target.value)}
          />
          {metadataError && <span className="field__error">{metadataError}</span>}
        </label>
      </div>
    </Drawer>
  )
}
