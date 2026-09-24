import { AlertTriangle } from 'lucide-react'
import { lazy, Suspense, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { createBrowserRouter, Navigate, Outlet, useParams, useRouteError } from 'react-router-dom'
import { Button, LoadingState } from '../shared/components/ui'
import { useAppStore } from './store'
import { AppShell } from './shell'
import { FlowRepoLayout } from '../features/flows/flow-repo-layout'
import { FlowSettingsLayout, NamespaceSettingsLayout } from '../features/settings/settings-layouts'
import { RuntimeConfigPage } from '../features/settings/runtime-config-page'

const DashboardPage = lazy(() =>
  import('../features/dashboard/dashboard-page').then((module) => ({
    default: module.DashboardPage,
  })),
)
const FlowsPage = lazy(() =>
  import('../features/flows/flows-page').then((module) => ({ default: module.FlowsPage })),
)
const FlowEditorPage = lazy(() =>
  import('../features/flows/flow-editor-page').then((module) => ({
    default: module.FlowEditorPage,
  })),
)
const RunsPage = lazy(() =>
  import('../features/runs/runs-page').then((module) => ({ default: module.RunsPage })),
)
const RunDetailPage = lazy(() =>
  import('../features/runs/run-detail-page').then((module) => ({
    default: module.RunDetailPage,
  })),
)
const TrackingPage = lazy(() =>
  import('../features/runs/tracking-page').then((module) => ({ default: module.TrackingPage })),
)
const MemoryPage = lazy(() =>
  import('../features/memory/memory-page').then((module) => ({ default: module.MemoryPage })),
)
const AgentsPage = lazy(() =>
  import('../features/agents/agents-page').then((module) => ({ default: module.AgentsPage })),
)
const SkillsPage = lazy(() =>
  import('../features/skills/skills-page').then((module) => ({ default: module.SkillsPage })),
)
const McpsPage = lazy(() =>
  import('../features/integrations/mcps-page').then((module) => ({ default: module.McpsPage })),
)
const LlmsPage = lazy(() =>
  import('../features/integrations/llms-page').then((module) => ({ default: module.LlmsPage })),
)
const NotificationsPage = lazy(() =>
  import('../features/integrations/notifications-page').then((module) => ({
    default: module.NotificationsPage,
  })),
)
const page = (element: React.ReactNode) => (
  <Suspense fallback={<LoadingState rows={6} />}>{element}</Suspense>
)

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    errorElement: <RouteError />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'dashboard', element: page(<DashboardPage />) },
      { path: 'flows', element: page(<FlowsPage />) },
      { path: 'flows/new', element: page(<FlowEditorPage />) },
      { path: 'runs', element: page(<RunsPage />) },
      { path: 'memory', element: <Navigate to="/memory/namespace" replace /> },
      { path: 'memory/:scope', element: page(<MemoryPage />) },
      { path: 'agents', element: page(<AgentsPage />) },
      { path: 'skills', element: page(<SkillsPage />) },
      { path: 'mcps', element: page(<McpsPage />) },
      { path: 'llms', element: page(<LlmsPage />) },
      { path: 'notifications', element: page(<NotificationsPage />) },
      {
        path: 'namespaces/:namespaceId',
        element: <NamespaceRouteScope />,
        children: [
          { index: true, element: <Navigate to="settings/environment" replace /> },
          {
            path: 'settings',
            element: <NamespaceSettingsLayout />,
            children: [
              { index: true, element: <Navigate to="environment" replace /> },
              {
                path: 'environment',
                element: page(<RuntimeConfigPage scope="namespace" section="environment" />),
              },
              {
                path: 'secrets',
                element: page(<RuntimeConfigPage scope="namespace" section="secrets" />),
              },
            ],
          },
        ],
      },
      {
        path: ':namespaceId/:flowId',
        element: <NamespaceRouteScope />,
        children: [
          {
            element: <FlowRepoLayout />,
            children: [
              { index: true, element: page(<FlowEditorPage />) },
              { path: 'runs', element: page(<RunsPage />) },
              { path: 'runs/:runId', element: page(<RunDetailPage />) },
              { path: 'runs/:runId/tracking', element: page(<TrackingPage />) },
              {
                path: 'settings',
                element: <FlowSettingsLayout />,
                children: [
                  { index: true, element: <Navigate to="environment" replace /> },
                  {
                    path: 'environment',
                    element: page(<RuntimeConfigPage scope="flow" section="environment" />),
                  },
                  {
                    path: 'secrets',
                    element: page(<RuntimeConfigPage scope="flow" section="secrets" />),
                  },
                ],
              },
            ],
          },
        ],
      },
      { path: '*', element: <NotFound /> },
    ],
  },
])

function NamespaceRouteScope() {
  const { namespaceId = '' } = useParams()
  const namespace = useAppStore((state) => state.namespace)
  const setNamespace = useAppStore((state) => state.setNamespace)
  useEffect(() => {
    if (namespaceId && namespaceId !== namespace) setNamespace(namespaceId)
  }, [namespace, namespaceId, setNamespace])
  if (!namespaceId || namespace !== namespaceId) return <LoadingState rows={4} />
  return <Outlet />
}

function RouteError() {
  const { t } = useTranslation()
  const error = useRouteError()
  return (
    <div className="fatal-state">
      <span>
        <AlertTriangle size={24} />
      </span>
      <h1>{t('errors.routeFailed')}</h1>
      <p>{error instanceof Error ? error.message : String(error)}</p>
      <Button onClick={() => window.location.assign('/dashboard')}>
        {t('errors.returnDashboard')}
      </Button>
    </div>
  )
}

function NotFound() {
  const { t } = useTranslation()
  return (
    <div className="fatal-state">
      <span>404</span>
      <h1>{t('errors.resourceNotFound')}</h1>
      <p>{t('errors.notFoundDescription')}</p>
      <Button onClick={() => window.location.assign('/dashboard')}>
        {t('errors.returnDashboard')}
      </Button>
    </div>
  )
}
