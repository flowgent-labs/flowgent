import clsx from 'clsx'
import { FileCode2, Radio } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TaskStatus } from '../../core/domain/types'
import { buildLineDiff, diffStats, type TaskFileChange } from './file-changes'

export function FileChangesPreview({
  changes,
  status,
}: {
  changes: TaskFileChange[]
  status: TaskStatus
}) {
  const { t } = useTranslation()
  const [selectedPath, setSelectedPath] = useState(changes[0]?.path ?? '')
  const selected = changes.find((change) => change.path === selectedPath) ?? changes[0]
  const lines = useMemo(
    () => (selected ? buildLineDiff(selected.before, selected.after) : []),
    [selected],
  )
  const stats = useMemo(() => diffStats(lines), [lines])
  if (!selected) return null

  return (
    <section className="file-change-preview" data-testid="task-file-preview">
      <header className="file-change-preview__header">
        <span className="file-change-preview__icon">
          <FileCode2 size={15} />
        </span>
        <div>
          <strong>{t('runs.fileChanges')}</strong>
          <small>{t('runs.fileCount', { count: changes.length })}</small>
        </div>
        {status === 'RUNNING' && (
          <span className="file-change-preview__live">
            <Radio size={11} /> {t('runs.live')}
          </span>
        )}
      </header>
      <nav className="file-change-tabs" aria-label={t('runs.changedFiles')}>
        {changes.map((change) => (
          <button
            type="button"
            className={change.path === selected.path ? 'is-active' : ''}
            onClick={() => setSelectedPath(change.path)}
            key={change.path}
          >
            <FileCode2 size={12} />
            <span>{change.path}</span>
          </button>
        ))}
      </nav>
      <div className="file-change-meta">
        <code title={selected.path}>{selected.path}</code>
        <span className={`file-change-kind file-change-kind--${selected.kind}`}>
          {t(`runs.fileChangeKind.${selected.kind}`)}
        </span>
        <span className="diff-stat diff-stat--added">+{stats.additions}</span>
        <span className="diff-stat diff-stat--deleted">−{stats.deletions}</span>
      </div>
      {(selected.rule || selected.description) && (
        <div className="file-change-description">
          {selected.rule && <code>{selected.rule}</code>}
          {selected.description && <span>{selected.description}</span>}
        </div>
      )}
      <div className="file-diff" role="table" aria-label={t('runs.fileDiff')}>
        {lines.map((line, index) => (
          <div
            className={clsx('file-diff__line', `file-diff__line--${line.kind}`)}
            role="row"
            key={`${index}:${line.kind}:${line.oldLine ?? ''}:${line.newLine ?? ''}`}
          >
            {line.kind === 'omitted' ? (
              <span className="file-diff__omitted">
                {t('runs.linesOmitted', { count: line.omitted ?? 0 })}
              </span>
            ) : (
              <>
                <span className="file-diff__number">{line.oldLine ?? ''}</span>
                <span className="file-diff__number">{line.newLine ?? ''}</span>
                <span className="file-diff__marker">
                  {line.kind === 'addition' ? '+' : line.kind === 'deletion' ? '−' : ' '}
                </span>
                <code>{line.text || ' '}</code>
              </>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
