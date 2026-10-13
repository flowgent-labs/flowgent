import { useAppStore } from './store'

export const RUNTIME_CONFIG_URL = '/runtime-config.json'

export type RuntimeConfigRequest = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>

interface RuntimeConfig {
  defaultNamespace: string
}

function parseRuntimeConfig(value: unknown): RuntimeConfig {
  const defaultNamespace =
    typeof value === 'object' && value !== null && 'defaultNamespace' in value
      ? value.defaultNamespace
      : undefined
  if (typeof defaultNamespace !== 'string' || !defaultNamespace.trim()) {
    throw new Error('Flowgent runtime config has no defaultNamespace')
  }
  return { defaultNamespace: defaultNamespace.trim() }
}

export async function applyRuntimeConfig(request: RuntimeConfigRequest) {
  const response = await request(RUNTIME_CONFIG_URL, {
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) {
    throw new Error(`Flowgent runtime config failed: HTTP ${response.status}`)
  }
  const config = parseRuntimeConfig(await response.json())
  useAppStore.getState().setRuntimeNamespace(config.defaultNamespace)
}
