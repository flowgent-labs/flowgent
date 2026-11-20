import { describe, expect, it, vi } from 'vitest'
import { authGuardUiContext, loadAuthGuardUiContext } from './authguard-context'

describe('AuthGuard UI context', () => {
  it('derives the principal and application brand from canonical AuthGuard payloads', () => {
    expect(
      authGuardUiContext(
        {
          principal: {
            principalId: 'P_012345',
            trustedClaims: { display_name: 'Flowgent Operator' },
          },
        },
        {
          application: {
            displayName: 'Security Autonomy Fixer',
            logo: '/auth/assets/themes/custom/flowgent.svg',
          },
        },
      ),
    ).toEqual({
      principalId: 'P_012345',
      displayName: 'Flowgent Operator',
      applicationName: 'Security Autonomy Fixer',
      logo: '/auth/assets/themes/custom/flowgent.svg',
    })
  })

  it('keeps an authenticated session usable when optional brand metadata is unavailable', async () => {
    const request = vi.fn().mockRejectedValue(new Error('metadata unavailable'))
    await expect(
      loadAuthGuardUiContext(
        new Response(
          JSON.stringify({
            principal: { principalId: 'P_fallback', trustedClaims: {} },
          }),
        ),
        request,
      ),
    ).resolves.toMatchObject({
      principalId: 'P_fallback',
      displayName: 'P_fallback',
      applicationName: 'Flowgent',
    })
  })
})
