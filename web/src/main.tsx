import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import { AppProviders } from './app/providers'
import { AuthenticationBoundary } from './app/authentication-boundary'
import { router } from './app/router'
import './shared/i18n'
import './shared/styles/index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppProviders>
      <AuthenticationBoundary>
        <RouterProvider router={router} />
      </AuthenticationBoundary>
    </AppProviders>
  </StrictMode>,
)
