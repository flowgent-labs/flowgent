import clsx from 'clsx'
import { AlertTriangle, Check, Database, LoaderCircle, X } from 'lucide-react'
import { useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import type { RunStatus, TaskStatus } from '../../core/domain/types'

export function Button({
  variant = 'primary',
  size = 'md',
  className,
  type = 'button',
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
}) {
  return (
    <button
      type={type}
      className={clsx('button', `button--${variant}`, `button--${size}`, className)}
      {...props}
    />
  )
}

export function IconButton({
  label,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return (
    <button type="button" className="icon-button" aria-label={label} title={label} {...props} />
  )
}

export function Field({
  label,
  hint,
  error,
  children,
  className,
}: {
  label: string
  hint?: string
  error?: string
  children: React.ReactNode
  className?: string
}) {
  const id = useId()
  return (
    <label className={clsx('field', className)} htmlFor={id}>
      <span className="field__label">{label}</span>
      {children}
      {hint && <span className="field__hint">{hint}</span>}
      {error && (
        <span className="field__error" role="alert">
          {error}
        </span>
      )}
    </label>
  )
}

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string
  title: string
  description?: string
  actions?: React.ReactNode
}) {
  return (
    <header className="page-header">
      <div>
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {description && <p className="page-header__description">{description}</p>}
      </div>
      {actions && <div className="page-header__actions">{actions}</div>}
    </header>
  )
}

export function StatusBadge({ status }: { status: RunStatus | TaskStatus | string }) {
  const { t } = useTranslation()
  const normalized = status.toLowerCase()
  return (
    <span className={clsx('status', `status--${normalized.replace('_', '-')}`)}>
      <span className="status__dot" aria-hidden="true" />
      {t(`status.${normalized}`, { defaultValue: status.replace('_', ' ') })}
    </span>
  )
}

export function LoadingState({ rows = 4 }: { rows?: number }) {
  const { t } = useTranslation()
  return (
    <div className="skeleton-stack" aria-busy="true" aria-label={t('common.loading')}>
      {Array.from({ length: rows }, (_, index) => (
        <div className="skeleton" key={index} />
      ))}
    </div>
  )
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <div className="empty-state">
      <div className="empty-state__orb">
        <Database size={22} />
      </div>
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {action}
    </div>
  )
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="error-state" role="alert">
      <AlertTriangle size={22} />
      <div>
        <strong>{t('errors.generic')}</strong>
        <p>{error instanceof Error ? error.message : String(error)}</p>
      </div>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          {t('common.retry')}
        </Button>
      )}
    </div>
  )
}

export function Modal({
  open,
  title,
  children,
  onClose,
  footer,
  wide = false,
}: {
  open: boolean
  title: string
  children: React.ReactNode
  onClose: () => void
  footer?: React.ReactNode
  wide?: boolean
}) {
  const dialog = useRef<HTMLDivElement>(null)
  const { t } = useTranslation()
  useEffect(() => {
    if (!open) return
    const prior = document.activeElement as HTMLElement | null
    const onKey = (event: KeyboardEvent) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    requestAnimationFrame(() => dialog.current?.focus())
    return () => {
      document.removeEventListener('keydown', onKey)
      prior?.focus()
    }
  }, [onClose, open])
  if (!open) return null
  return createPortal(
    <div
      className="modal-backdrop"
      role="presentation"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <div
        className={clsx('modal', wide && 'modal--wide')}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        ref={dialog}
      >
        <header className="modal__header">
          <h2>{title}</h2>
          <IconButton label={t('common.close')} onClick={onClose}>
            <X size={18} />
          </IconButton>
        </header>
        <div className="modal__body">{children}</div>
        {footer && <footer className="modal__footer">{footer}</footer>}
      </div>
    </div>,
    document.body,
  )
}

export function Drawer({
  open,
  title,
  children,
  onClose,
  footer,
}: {
  open: boolean
  title: string
  children: React.ReactNode
  onClose: () => void
  footer?: React.ReactNode
}) {
  const { t } = useTranslation()
  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => event.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose, open])
  return (
    <div className={clsx('drawer-wrap', open && 'is-open')} aria-hidden={!open}>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" aria-label={title}>
        <header className="drawer__header">
          <h2>{title}</h2>
          <IconButton label={t('common.close')} onClick={onClose}>
            <X size={18} />
          </IconButton>
        </header>
        <div className="drawer__body">{children}</div>
        {footer && <footer className="drawer__footer">{footer}</footer>}
      </aside>
    </div>
  )
}

export function JsonView({ value, label }: { value: unknown; label?: string }) {
  const { t } = useTranslation()
  const text = JSON.stringify(value ?? {}, null, 2)
  const copy = async () => navigator.clipboard?.writeText(text)
  return (
    <section className="json-view">
      {label && (
        <header>
          <span>{label}</span>
          <button type="button" onClick={copy}>
            {t('common.copy')}
          </button>
        </header>
      )}
      <pre>{text}</pre>
    </section>
  )
}

export function SavingLabel({ saving, children }: { saving: boolean; children: React.ReactNode }) {
  return (
    <>
      {saving && <LoaderCircle size={15} className="spin" />}
      {!saving && <Check size={15} />}
      {children}
    </>
  )
}
