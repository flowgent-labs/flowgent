import { Activity, GitBranch, Settings } from 'lucide-react'
import { NavLink, Outlet, useParams } from 'react-router-dom'
import { flowPath, flowSettingsPath } from '../../app/paths'

export function FlowRepoLayout() {
  const { namespaceId = '', flowId = '' } = useParams()
  return (
    <div className="flow-repo">
      <header className="flow-repo__header" data-testid="flow-repo-context">
        <nav aria-label="Flow">
          <NavLink to={flowPath(namespaceId, flowId)} end>
            <GitBranch size={15} />
            Definition
          </NavLink>
          <NavLink to={flowPath(namespaceId, flowId, '/runs')}>
            <Activity size={15} />
            Runs
          </NavLink>
          <NavLink to={flowSettingsPath(namespaceId, flowId)}>
            <Settings size={15} />
            Settings
          </NavLink>
        </nav>
      </header>
      <Outlet />
    </div>
  )
}
