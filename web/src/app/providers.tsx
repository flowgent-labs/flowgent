import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { createRepositories } from '../core/api/repositories'
import type { Repositories } from '../core/domain/repositories'
import { effectiveTheme, useAppStore } from './store'

const RepositoryContext = createContext<Repositories | null>(null)

export function useRepositories(): Repositories {
  const repositories = useContext(RepositoryContext)
  if (!repositories) throw new Error('RepositoryProvider is missing')
  return repositories
}

export function AppProviders({
  children,
  repositories,
}: {
  children: React.ReactNode
  repositories?: Repositories
}) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 20_000, retry: 1, refetchOnWindowFocus: false },
          mutations: { retry: 0 },
        },
      }),
  )
  const value = useMemo(() => repositories ?? createRepositories(), [repositories])
  return (
    <RepositoryContext.Provider value={value}>
      <QueryClientProvider client={queryClient}>
        <PreferenceSync>{children}</PreferenceSync>
      </QueryClientProvider>
    </RepositoryContext.Provider>
  )
}

function PreferenceSync({ children }: { children: React.ReactNode }) {
  const { i18n } = useTranslation()
  const theme = useAppStore((state) => state.theme)
  const locale = useAppStore((state) => state.locale)

  useEffect(() => {
    void i18n.changeLanguage(locale)
    document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'
  }, [i18n, locale])
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = () => {
      document.documentElement.dataset.theme = effectiveTheme(theme)
      document.documentElement.style.colorScheme = effectiveTheme(theme)
    }
    apply()
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [theme])
  return children
}
