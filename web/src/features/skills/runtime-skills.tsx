import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { GitBranch, Sparkles, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useRepositories } from '../../app/providers'
import {
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  JsonView,
  LoadingState,
  Modal,
  PageHeader,
  StatusBadge,
} from '../../shared/components/ui'
import type { Flow } from '../../core/domain/types'
import { useAppStore } from '../../app/store'
import { ManifestImportButton } from '../resources/manifest-import-button'
import { manifestToFlow } from '../resources/manifest'

export function RuntimeSkills() {
  const { t } = useTranslation()
  const namespace = useAppStore((state) => state.namespace)
  const repositories = useRepositories()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Flow | null>(null)
  const query = useQuery({
    queryKey: [namespace, 'runtime-skills'],
    queryFn: ({ signal }) => repositories.runtimeSkills.list(namespace, signal),
  })
  const save = useMutation({
    mutationFn: (skill: Flow) => repositories.runtimeSkills.save(namespace, skill, true),
    onSuccess: () => {
      setDraft(null)
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runtime-skills'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => repositories.runtimeSkills.remove(namespace, id),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: [namespace, 'runtime-skills'] }),
  })

  return (
    <section className="page-stack" data-testid="runtime-skills">
      <PageHeader
        eyebrow={t('skills.runtimeEyebrow')}
        title={t('skills.runtimeTitle')}
        description={t('skills.runtimeSubtitle')}
        actions={
          <ManifestImportButton
            label={t('common.importManifest')}
            onImport={(source) => {
              try {
                setDraft(manifestToFlow(source, 'Skill'))
              } catch (error) {
                window.alert(error instanceof Error ? error.message : String(error))
              }
            }}
          />
        }
      />
      <div className="toolbar">
        <span className="result-count">
          {t('skills.runtimeCount', { count: query.data?.length ?? 0 })}
        </span>
      </div>
      {query.isLoading ? (
        <LoadingState rows={3} />
      ) : query.isError ? (
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : !query.data?.length ? (
        <EmptyState title={t('skills.noRuntimeSkills')} />
      ) : (
        <div className="resource-card-grid">
          {query.data.map((skill) => (
            <article className="resource-card" key={skill.id}>
              <header>
                <span className="resource-card__icon resource-card__icon--gold">
                  <Sparkles size={20} />
                </span>
                <StatusBadge status={skill.status || 'ACTIVE'} />
              </header>
              <div className="resource-card__body">
                <h2>{skill.id}</h2>
                <p>{skill.description || skill.summary || t('skills.runtimeFallback')}</p>
                <dl>
                  <div>
                    <dt>
                      <GitBranch size={13} />
                      {t('flows.nodes')}
                    </dt>
                    <dd>{skill.nodes.length}</dd>
                  </div>
                </dl>
              </div>
              <footer>
                <code>kind=skill</code>
                <IconButton
                  label={t('common.delete')}
                  onClick={() => {
                    if (window.confirm(t('common.confirmDelete'))) remove.mutate(skill.id)
                  }}
                >
                  <Trash2 size={15} />
                </IconButton>
              </footer>
            </article>
          ))}
        </div>
      )}
      <Modal
        open={Boolean(draft)}
        title={draft ? `${t('skills.importRuntime')} · ${draft.id}` : t('skills.importRuntime')}
        onClose={() => setDraft(null)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!draft || save.isPending} onClick={() => draft && save.mutate(draft)}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        {draft && <JsonView value={{ id: draft.id, kind: draft.kind, nodes: draft.nodes }} />}
        {save.isError && <ErrorState error={save.error} />}
      </Modal>
    </section>
  )
}
