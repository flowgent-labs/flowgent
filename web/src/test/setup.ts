import '@testing-library/jest-dom/vitest'
import '../shared/i18n'

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
})

// Node's optional Web Storage global is not the browser Storage implementation
// used by jsdom. Keep preference persistence deterministic in this test realm.
const browserStorage = new Map<string, string>()
const storage: Storage = {
  get length() {
    return browserStorage.size
  },
  clear: () => browserStorage.clear(),
  getItem: (key) => browserStorage.get(key) ?? null,
  key: (index) => [...browserStorage.keys()][index] ?? null,
  removeItem: (key) => {
    browserStorage.delete(key)
  },
  setItem: (key, value) => {
    browserStorage.set(key, String(value))
  },
}
Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage })
