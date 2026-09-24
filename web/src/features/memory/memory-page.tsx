import { useQuery } from '@tanstack/react-query'
import { BookOpen, FileText, Search, ShieldCheck, Tag } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { useRepositories } from '../../app/providers'
import { useAppStore } from '../../app/store'
import type { MemoryScope } from '../../core/domain/types'
import { EmptyState, ErrorState, LoadingState, PageHeader } from '../../shared/components/ui'

export function MemoryPage() {
  const { scope = 'namespace' } = useParams()
  const memoryScope = (
    ['namespace', 'flow', 'run'].includes(scope) ? scope : 'namespace'
  ) as MemoryScope
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const [search, setSearch] = useState('')
  const [tag, setTag] = useState('')
  const query = useQuery({
    queryKey: [namespace, 'knowledge', memoryScope],
    queryFn: ({ signal }) => repositories.knowledge.list(namespace, memoryScope, signal),
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

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t('memory.scopeEyebrow', { scope: memoryScope.toUpperCase() })}
        title={t('memory.title')}
        description={t(`memory.${memoryScope}Subtitle`)}
      />
      <div className="contract-warning contract-warning--info" data-testid="memory-published-only">
        <ShieldCheck size={16} />
        <span>{t('memory.publishedOnlyNotice')}</span>
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
        <EmptyState title={t('common.noData')} />
      ) : (
        <div className="knowledge-grid">
          {rows.map((entry) => (
            <article
              className="knowledge-card"
              data-testid={`memory-card-${entry.title}`}
              key={entry.id}
            >
              <div className="knowledge-card__main">
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
              </div>
              <footer className="knowledge-card__footer">
                <BookOpen size={14} />
                <span>{t('memory.immutablePublished')}</span>
              </footer>
            </article>
          ))}
        </div>
      )}
    </div>
  )
}
