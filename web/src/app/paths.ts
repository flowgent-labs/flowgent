const segment = (value: string) => encodeURIComponent(value)

export function namespacePathname(namespace: string, suffix = ''): string {
  return `/namespaces/${segment(namespace)}${suffix}`
}

export function namespaceSettingsPath(namespace: string, resource = 'environment'): string {
  return namespacePathname(namespace, `/settings/${segment(resource)}`)
}

export function flowPath(namespace: string, flowId: string, suffix = ''): string {
  return `/${segment(namespace)}/${segment(flowId)}${suffix}`
}

export function flowRunPath(
  namespace: string,
  flowId: string,
  runId?: string,
  suffix = '',
): string {
  const run = runId ? `/${segment(runId)}` : ''
  return flowPath(namespace, flowId, `/runs${run}${suffix}`)
}

export function flowSettingsPath(namespace: string, flowId: string, section = ''): string {
  const suffix = section ? `/settings/${segment(section)}` : '/settings'
  return flowPath(namespace, flowId, suffix)
}
