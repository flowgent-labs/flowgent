import { describe, expect, it } from 'vitest'
import { sameResourceName, validNamespaceName, validResourceName } from './resource-name'

describe('public resource names', () => {
  it('accepts URL-safe names starting with a letter', () => {
    expect(validResourceName('Security_Fixer-1')).toBe(true)
    expect(validResourceName('1-fixer')).toBe(false)
    expect(validResourceName('security.fixer')).toBe(false)
    expect(validResourceName('abcdefghijklmnopqrstuvwxyzABCDEFG')).toBe(false)
  })

  it('reserves application roots for namespaces', () => {
    expect(validNamespaceName('Settings')).toBe(false)
    expect(validNamespaceName('Acme_Team')).toBe(true)
  })

  it('compares uniqueness case-insensitively', () => {
    expect(sameResourceName('Acme', 'acme')).toBe(true)
  })
})
