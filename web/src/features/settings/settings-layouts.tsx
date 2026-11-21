import { Braces, KeyRound } from 'lucide-react'
import { NavLink, Outlet, useParams } from 'react-router-dom'
import { graphSettingsPath, namespaceSettingsPath } from '../../app/paths'
import { useAppStore } from '../../app/store'

export function NamespaceSettingsLayout() {
  const { namespaceId = '' } = useParams()
  const locale = useAppStore((state) => state.locale)
  return (
    <div className="settings-layout">
      <aside className="settings-sidebar" aria-label="Namespace settings">
        <strong>{locale === 'zh' ? 'Namespace 设置' : 'Namespace settings'}</strong>
        <span>{locale === 'zh' ? '运行时配置' : 'Runtime configuration'}</span>
        <nav>
          <NavLink to={namespaceSettingsPath(namespaceId, 'environment')}>
            <Braces size={16} />
            {locale === 'zh' ? '环境变量' : 'Environment'}
          </NavLink>
          <NavLink to={namespaceSettingsPath(namespaceId, 'secrets')}>
            <KeyRound size={16} />
            {locale === 'zh' ? '密钥' : 'Secrets'}
          </NavLink>
        </nav>
      </aside>
      <div className="settings-layout__content">
        <Outlet />
      </div>
    </div>
  )
}

export function AgentGraphSettingsLayout() {
  const { namespaceId = '', graphId = '' } = useParams()
  const locale = useAppStore((state) => state.locale)
  return (
    <div className="settings-layout">
      <aside className="settings-sidebar" aria-label="AgentGraph settings">
        <strong>{locale === 'zh' ? 'AgentGraph 设置' : 'AgentGraph settings'}</strong>
        <nav>
          <NavLink to={graphSettingsPath(namespaceId, graphId, 'environment')}>
            <Braces size={16} />
            {locale === 'zh' ? '环境变量' : 'Environment'}
          </NavLink>
          <NavLink to={graphSettingsPath(namespaceId, graphId, 'secrets')}>
            <KeyRound size={16} />
            {locale === 'zh' ? '密钥' : 'Secrets'}
          </NavLink>
        </nav>
      </aside>
      <div className="settings-layout__content">
        <Outlet />
      </div>
    </div>
  )
}
