import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { defaultAuthGuardUiContext, type AuthGuardUiContext } from './authguard-context'

export type Theme = 'light' | 'dark' | 'system'
export type Locale = 'en' | 'zh'
type NamespaceOrigin = 'build' | 'runtime' | 'user'

interface AppState {
  theme: Theme
  locale: Locale
  namespace: string
  namespaceOrigin: NamespaceOrigin
  authGuard: AuthGuardUiContext
  setTheme: (theme: Theme) => void
  setLocale: (locale: Locale) => void
  setNamespace: (namespace: string) => void
  setRuntimeNamespace: (namespace: string) => void
  setAuthGuard: (authGuard: AuthGuardUiContext) => void
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'system',
      locale: 'en',
      namespace: import.meta.env.VITE_DEFAULT_NAMESPACE ?? 'default',
      namespaceOrigin: 'build',
      authGuard: defaultAuthGuardUiContext,
      setTheme: (theme) => set({ theme }),
      setLocale: (locale) => set({ locale }),
      setNamespace: (namespace) => set({ namespace, namespaceOrigin: 'user' }),
      setRuntimeNamespace: (namespace) =>
        set((state) =>
          state.namespaceOrigin === 'user' ? state : { namespace, namespaceOrigin: 'runtime' },
        ),
      setAuthGuard: (authGuard) => set({ authGuard }),
    }),
    {
      name: 'flowgent-web-preferences',
      partialize: ({ theme, locale, namespace, namespaceOrigin }) => ({
        theme,
        locale,
        namespace,
        namespaceOrigin,
      }),
    },
  ),
)

export function effectiveTheme(theme: Theme): 'light' | 'dark' {
  if (theme !== 'system') return theme
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}
