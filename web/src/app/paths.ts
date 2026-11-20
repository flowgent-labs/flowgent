const segment = (value: string) => encodeURIComponent(value)

export function namespacePathname(namespace: string, suffix = ''): string {
  return `/namespaces/${segment(namespace)}${suffix}`
}

export function namespaceSettingsPath(namespace: string, resource = 'environment'): string {
  return namespacePathname(namespace, `/settings/${segment(resource)}`)
}

export function graphPath(namespace: string, graphId: string, suffix = ''): string {
  return `/${segment(namespace)}/${segment(graphId)}${suffix}`
}

export function graphRunPath(
  namespace: string,
  graphId: string,
  graphRunId?: string,
  suffix = '',
): string {
  const run = graphRunId ? `/${segment(graphRunId)}` : ''
  return graphPath(namespace, graphId, `/graph-runs${run}${suffix}`)
}

export function graphSettingsPath(namespace: string, graphId: string, section = ''): string {
  const suffix = section ? `/settings/${segment(section)}` : '/settings'
  return graphPath(namespace, graphId, suffix)
}
