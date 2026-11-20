import { describe, expect, it } from 'vitest'
import { flowPath, flowRunPath, flowSettingsPath, namespaceSettingsPath } from './paths'

describe('canonical resource paths', () => {
  it('uses GitHub-style namespace/flow paths', () => {
    expect(flowPath('Acme_Team', 'Security-Fixer')).toBe('/Acme_Team/Security-Fixer')
    expect(flowRunPath('Acme_Team', 'Security-Fixer', 'run 1', '/tracking')).toBe(
      '/Acme_Team/Security-Fixer/runs/run%201/tracking',
    )
    expect(flowSettingsPath('Acme_Team', 'Security-Fixer')).toBe(
      '/Acme_Team/Security-Fixer/settings',
    )
    expect(flowSettingsPath('Acme_Team', 'Security-Fixer', 'environment')).toBe(
      '/Acme_Team/Security-Fixer/settings/environment',
    )
  })

  it('keeps namespace runtime settings under the explicit namespace root', () => {
    expect(namespaceSettingsPath('Acme_Team')).toBe('/namespaces/Acme_Team/settings/environment')
  })
})
