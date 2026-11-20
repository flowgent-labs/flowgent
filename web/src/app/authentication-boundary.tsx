import { useEffect, useState, type ReactNode } from 'react'
import { ErrorState, LoadingState } from '../shared/components/ui'
import { loadAuthGuardUiContext } from './authguard-context'
import { applyRuntimeConfig, type RuntimeConfigRequest } from './runtime-config'
import { useAppStore } from './store'

type SessionRequest = RuntimeConfigRequest

const requestSession: SessionRequest = (input, init) => fetch(input, init)
const redirectToLogin = (url: string) => window.location.replace(url)

export function currentReturnPath(location: Pick<Location, 'pathname' | 'search' | 'hash'>) {
  return `${location.pathname}${location.search}${location.hash}` || '/dashboard'
}

export function hostedLoginUrl(returnPath: string) {
  return `/auth/login?return_to=${encodeURIComponent(returnPath)}`
}

export function AuthenticationBoundary({
  children,
  request = requestSession,
  redirect = redirectToLogin,
}: {
  children: ReactNode
  request?: SessionRequest
  redirect?: (url: string) => void
}) {
  const [authenticated, setAuthenticated] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [retry, setRetry] = useState(0)

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const response = await request('/auth/session', {
          cache: 'no-store',
          credentials: 'same-origin',
          headers: { Accept: 'application/json' },
        })
        if (!active) return
        if (response.ok) {
          const authGuard = await loadAuthGuardUiContext(response, request)
          await applyRuntimeConfig(request)
          if (active) {
            useAppStore.getState().setAuthGuard(authGuard)
            setAuthenticated(true)
          }
          return
        }
        if (response.status === 401 || response.status === 403) {
          redirect(hostedLoginUrl(currentReturnPath(window.location)))
          return
        }
        throw new Error(`AuthGuard session check failed: HTTP ${response.status}`)
      } catch (cause: unknown) {
        if (active) setError(cause)
      }
    })()
    return () => {
      active = false
    }
  }, [redirect, request, retry])

  if (error) {
    return (
      <div className="fatal-state">
        <ErrorState
          error={error}
          onRetry={() => {
            setError(null)
            setRetry((value) => value + 1)
          }}
        />
      </div>
    )
  }
  if (!authenticated) return <LoadingState rows={6} />
  return children
}
