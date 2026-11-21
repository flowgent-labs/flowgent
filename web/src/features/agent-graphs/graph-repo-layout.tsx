import { Activity, GitBranch, Settings } from 'lucide-react'
import { NavLink, Outlet, useParams } from 'react-router-dom'
import { graphPath, graphSettingsPath } from '../../app/paths'

export function AgentGraphRepoLayout() {
  const { namespaceId = '', graphId = '' } = useParams()
  return (
    <div className="graph-repo">
      <header className="graph-repo__header" data-testid="graph-repo-context">
        <nav aria-label="AgentGraph">
          <NavLink to={graphPath(namespaceId, graphId)} end>
            <GitBranch size={15} />
            Definition
          </NavLink>
          <NavLink to={graphPath(namespaceId, graphId, '/graph-runs')}>
            <Activity size={15} />
            Runs
          </NavLink>
          <NavLink to={graphSettingsPath(namespaceId, graphId)}>
            <Settings size={15} />
            Settings
          </NavLink>
        </nav>
      </header>
      <Outlet />
    </div>
  )
}
