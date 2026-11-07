import { formatDistanceStrict } from 'date-fns'

export function duration(start?: string | null, end?: string | null): string {
  if (!start) return '—'
  try {
    return formatDistanceStrict(new Date(start), end ? new Date(end) : new Date())
  } catch {
    return '—'
  }
}

export function formatDate(value?: string | null): string {
  if (!value) return '—'
  try {
    return new Intl.DateTimeFormat(undefined, {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(new Date(value))
  } catch {
    return '—'
  }
}
