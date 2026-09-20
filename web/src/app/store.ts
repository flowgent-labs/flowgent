import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type Theme = 'light' | 'dark' | 'system'
export type Locale = 'en' | 'zh'

interface AppState {
  theme: Theme
  locale: Locale
  namespace: string
  setTheme: (theme: Theme) => void
  setLocale: (locale: Locale) => void
  setNamespace: (namespace: string) => void
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'system',
      locale: 'en',
      namespace: import.meta.env.VITE_DEFAULT_NAMESPACE ?? 'default',
      setTheme: (theme) => set({ theme }),
      setLocale: (locale) => set({ locale }),
      setNamespace: (namespace) => set({ namespace }),
    }),
    {
      name: 'flowgent-web-preferences',
      partialize: ({ theme, locale, namespace }) => ({ theme, locale, namespace }),
    },
  ),
)

export function effectiveTheme(theme: Theme): 'light' | 'dark' {
  if (theme !== 'system') return theme
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}
