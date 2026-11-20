export interface AuthGuardUiContext {
  principalId: string
  displayName: string
  applicationName: string
  logo: string
}

export const defaultAuthGuardUiContext: AuthGuardUiContext = {
  principalId: '',
  displayName: '',
  applicationName: 'Flowgent',
  logo: '',
}

interface BrowserSessionPayload {
  principal?: {
    principalId?: unknown
    trustedClaims?: Record<string, unknown>
  }
}

interface AuthenticationMetadata {
  application?: {
    displayName?: unknown
    logo?: unknown
  } | null
}

export type AuthGuardRequest = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

function text(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

export function authGuardUiContext(
  session: BrowserSessionPayload,
  metadata: AuthenticationMetadata,
): AuthGuardUiContext {
  const principal = session.principal
  const claims = principal?.trustedClaims ?? {}
  const principalId = text(principal?.principalId)
  const displayName =
    text(claims.display_name) ||
    text(claims.displayName) ||
    text(claims.name) ||
    text(claims.preferred_username) ||
    text(claims.login) ||
    text(claims.email) ||
    principalId

  return {
    principalId,
    displayName,
    applicationName:
      text(metadata.application?.displayName) || defaultAuthGuardUiContext.applicationName,
    logo: text(metadata.application?.logo),
  }
}

export async function loadAuthGuardUiContext(
  sessionResponse: Response,
  request: AuthGuardRequest,
): Promise<AuthGuardUiContext> {
  const session = (await sessionResponse.json().catch(() => ({}))) as BrowserSessionPayload
  let metadata: AuthenticationMetadata = {}
  try {
    const response = await request('/.well-known/authn.json', {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
    if (response.ok) metadata = (await response.json()) as AuthenticationMetadata
  } catch {
    // Authentication remains authoritative. A transient branding endpoint
    // failure must not turn an established browser session into an outage.
  }
  return authGuardUiContext(session, metadata)
}
