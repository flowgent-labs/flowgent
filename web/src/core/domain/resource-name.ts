export const RESOURCE_NAME_MAX_LENGTH = 32
export const RESOURCE_NAME_PATTERN_SOURCE = '^[A-Za-z][A-Za-z0-9_-]{0,31}$'
export const RESOURCE_NAME_PATTERN = new RegExp(RESOURCE_NAME_PATTERN_SOURCE)

const reservedNamespaces = new Set([
  'agents',
  'dashboard',
  'flows',
  'llms',
  'mcps',
  'memory',
  'namespaces',
  'notifications',
  'runs',
  'settings',
  'skills',
])

export function validResourceName(name: string): boolean {
  return RESOURCE_NAME_PATTERN.test(name)
}

export function validNamespaceName(name: string): boolean {
  return validResourceName(name) && !reservedNamespaces.has(name.toLowerCase())
}

export function sameResourceName(left: string, right: string): boolean {
  return left.localeCompare(right, undefined, { sensitivity: 'base' }) === 0
}
