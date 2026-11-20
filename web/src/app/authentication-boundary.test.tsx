import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  AuthenticationBoundary,
  currentReturnPath,
  hostedLoginUrl,
} from './authentication-boundary'
import { useAppStore } from './store'

afterEach(() => {
  useAppStore.setState({
    namespace: 'default',
    namespaceOrigin: 'build',
    authGuard: {
      principalId: '',
      displayName: '',
      applicationName: 'Flowgent',
      logo: '',
    },
  })
  window.localStorage.clear()
  window.history.replaceState({}, '', '/')
})

describe('AuthenticationBoundary', () => {
  it('renders the console only after AuthGuard confirms the browser session', async () => {
    const request = vi.fn().mockImplementation((input: RequestInfo | URL) =>
      Promise.resolve(
        new Response(
          input === '/runtime-config.json'
            ? JSON.stringify({ defaultNamespace: 'security-fixer' })
            : input === '/auth/session'
              ? JSON.stringify({
                  principal: {
                    principalId: 'P_admin',
                    trustedClaims: { display_name: 'Flowgent Admin' },
                  },
                })
              : JSON.stringify({
                  application: {
                    displayName: 'Flowgent Security Autonomy Fixer',
                    logo: '/auth/assets/themes/custom/flowgent.svg',
                  },
                }),
          { status: 200 },
        ),
      ),
    )

    render(
      <AuthenticationBoundary request={request}>
        <div>protected console</div>
      </AuthenticationBoundary>,
    )

    expect(await screen.findByText('protected console')).toBeVisible()
    expect(request).toHaveBeenNthCalledWith(1, '/auth/session', {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    expect(request).toHaveBeenNthCalledWith(2, '/.well-known/authn.json', {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    expect(request).toHaveBeenNthCalledWith(3, '/runtime-config.json', {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    expect(useAppStore.getState().namespace).toBe('security-fixer')
    expect(useAppStore.getState().authGuard).toMatchObject({
      principalId: 'P_admin',
      displayName: 'Flowgent Admin',
      applicationName: 'Flowgent Security Autonomy Fixer',
    })
  })

  it('preserves a namespace explicitly selected by the user', async () => {
    useAppStore.getState().setNamespace('user-selected')
    const request = vi
      .fn()
      .mockImplementation((input: RequestInfo | URL) =>
        Promise.resolve(
          new Response(
            input === '/runtime-config.json'
              ? JSON.stringify({ defaultNamespace: 'security-fixer' })
              : '{}',
            { status: 200 },
          ),
        ),
      )

    render(
      <AuthenticationBoundary request={request}>
        <div>protected console</div>
      </AuthenticationBoundary>,
    )

    expect(await screen.findByText('protected console')).toBeVisible()
    expect(useAppStore.getState().namespace).toBe('user-selected')
  })

  it('redirects an anonymous browser to Hosted Login with the full return path', async () => {
    window.history.replaceState({}, '', '/flows?status=active#recent')
    const request = vi.fn().mockResolvedValue(new Response('{}', { status: 401 }))
    const redirect = vi.fn()

    render(
      <AuthenticationBoundary request={request} redirect={redirect}>
        <div>must stay hidden</div>
      </AuthenticationBoundary>,
    )

    await waitFor(() =>
      expect(redirect).toHaveBeenCalledWith(
        '/auth/login?return_to=%2Fflows%3Fstatus%3Dactive%23recent',
      ),
    )
    expect(screen.queryByText('must stay hidden')).not.toBeInTheDocument()
  })

  it('fails closed when the AuthGuard session endpoint is unhealthy', async () => {
    const request = vi.fn().mockResolvedValue(new Response('{}', { status: 503 }))

    render(
      <AuthenticationBoundary request={request}>
        <div>must stay hidden</div>
      </AuthenticationBoundary>,
    )

    expect(await screen.findByText('AuthGuard session check failed: HTTP 503')).toBeVisible()
    expect(screen.queryByText('must stay hidden')).not.toBeInTheDocument()
  })
})

describe('Hosted Login location', () => {
  it('preserves path, query, and fragment using one encoded return_to value', () => {
    expect(
      hostedLoginUrl(
        currentReturnPath({ pathname: '/dashboard', search: '?range=7d', hash: '#failures' }),
      ),
    ).toBe('/auth/login?return_to=%2Fdashboard%3Frange%3D7d%23failures')
  })
})
