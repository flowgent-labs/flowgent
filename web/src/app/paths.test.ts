import { describe, expect, it } from 'vitest'
import { graphPath, graphRunPath, graphSettingsPath, namespaceSettingsPath } from './paths'

describe('canonical resource paths', () => {
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
