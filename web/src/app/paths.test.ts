import { describe, expect, it } from 'vitest'
import { matchRoutes } from 'react-router-dom'
import { graphPath, graphRunPath, graphSettingsPath, namespaceSettingsPath } from './paths'
import { router } from './router'

describe('canonical resource paths', () => {
  it('matches catalog and create links without treating them as a foreign namespace', () => {
    for (const path of ['/agent-graphs', '/agent-graphs/new']) {
      const match = matchRoutes(router.routes, path)?.at(-1)
      expect(match?.route.path).toBe(path.slice(1))
      expect(match?.params).toEqual({})
    }
  })

  it('uses GitHub-style namespace/graph paths', () => {
    expect(graphPath('Acme_Team', 'Security-Fixer')).toBe('/Acme_Team/Security-Fixer')
    expect(graphRunPath('Acme_Team', 'Security-Fixer', 'run 1', '/tracking')).toBe(
      '/Acme_Team/Security-Fixer/graph-runs/run%201/tracking',
    )
    expect(graphSettingsPath('Acme_Team', 'Security-Fixer')).toBe(
      '/Acme_Team/Security-Fixer/settings',
    )
    expect(graphSettingsPath('Acme_Team', 'Security-Fixer', 'environment')).toBe(
      '/Acme_Team/Security-Fixer/settings/environment',
    )
  })

  it('keeps namespace runtime settings under the explicit namespace root', () => {
    expect(namespaceSettingsPath('Acme_Team')).toBe('/namespaces/Acme_Team/settings/environment')
  })
})
