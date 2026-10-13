import {
  Activity,
  Bot,
  BellRing,
  BrainCircuit,
  ChevronDown,
  CircleGauge,
  Languages,
  LogOut,
  Menu,
  Moon,
  Network,
  Orbit,
  Settings,
  ShieldCheck,
  Sparkles,
  Sun,
  Waypoints,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet } from 'react-router-dom'
import { Modal } from '../shared/components/ui'
import { useAppStore } from './store'
import { namespaceSettingsPath } from './paths'

const globalNav = [
  { to: '/dashboard', label: 'nav.dashboard', icon: CircleGauge },
  { to: '/runs', label: 'nav.runs', icon: Activity },
  { to: '/flows', label: 'nav.flows', icon: Waypoints },
  { to: '/memory/namespace', label: 'nav.memory', icon: BrainCircuit },
  { to: '/agents', label: 'nav.agents', icon: Bot },
  { to: '/skills', label: 'nav.skills', icon: Sparkles },
  { to: '/mcps', label: 'nav.mcps', icon: Network },
  { to: '/llms', label: 'nav.llms', icon: BrainCircuit },
  { to: '/notifications', label: 'nav.notifications', icon: BellRing },
]

function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      <span />
      <Orbit size={22} />
    </span>
  )
}

export function AppShell() {
  const { t } = useTranslation()
  const [mobileOpen, setMobileOpen] = useState(false)
  return (
    <div className="app-shell">
      <aside className="topbar">
        <NavLink to="/dashboard" className="topbar__brand" aria-label={t('app.name')}>
          <BrandMark />
          <div className="brand-copy">
            <strong>{t('app.name')}</strong>
            <span>{t('app.console')}</span>
          </div>
        </NavLink>
        <button
          type="button"
          className="mobile-menu"
          onClick={() => setMobileOpen((value) => !value)}
          aria-label={t('nav.open')}
          aria-expanded={mobileOpen}
        >
          <Menu size={20} />
        </button>
        <nav
          className={`global-nav${mobileOpen ? ' global-nav--open' : ''}`}
          aria-label={t('nav.primary')}
          onClick={() => setMobileOpen(false)}
        >
          {globalNav.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to}>
              <Icon size={16} />
              <span>{t(label)}</span>
            </NavLink>
          ))}
        </nav>
      </aside>
      <section className="workspace-shell">
        <header className="utility-bar">
          <AccountMenu />
        </header>
        <main className="page">
          <Outlet />
        </main>
      </section>
    </div>
  )
}

function AccountMenu() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [securityOpen, setSecurityOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const locale = useAppStore((state) => state.locale)
  const theme = useAppStore((state) => state.theme)
  const setLocale = useAppStore((state) => state.setLocale)
  const setTheme = useAppStore((state) => state.setTheme)
  const namespace = useAppStore((state) => state.namespace)
  const authGuard = useAppStore((state) => state.authGuard)
  const displayName = authGuard.displayName || t('header.signedInUser')
  const secondary = authGuard.applicationName || t('app.name')
  const initials = displayName
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join('')
    .toUpperCase()
    .slice(0, 2)

  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) =>
      !menuRef.current?.contains(event.target as Node) && setOpen(false)
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false)
        buttonRef.current?.focus()
      }
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', key)
    }
  }, [open])

  return (
    <>
      <div className="account" ref={menuRef}>
        <button
          ref={buttonRef}
          type="button"
          className="account__button"
          aria-label={t('header.account')}
          aria-haspopup="menu"
          aria-expanded={open}
          data-testid="account-menu-trigger"
          onClick={() => setOpen((value) => !value)}
        >
          <AccountAvatar logo={authGuard.logo} initials={initials} />
          <span className="account__identity">
            <strong>{displayName}</strong>
            <span data-testid="account-application">{secondary}</span>
          </span>
          <ChevronDown size={15} />
        </button>
        {open && (
          <div className="account-menu" role="menu">
            <div className="account-menu__heading">
              <AccountAvatar logo={authGuard.logo} initials={initials} large />
              <div>
                <strong>{displayName}</strong>
                <span>{secondary}</span>
              </div>
            </div>
            <MenuSetting icon={<Languages size={16} />} label={t('header.language')}>
              <Segmented
                value={locale}
                options={[
                  ['en', t('header.english')],
                  ['zh', t('header.chinese')],
                ]}
                onChange={(value) => setLocale(value as 'en' | 'zh')}
              />
            </MenuSetting>
            <MenuSetting
              icon={theme === 'dark' ? <Moon size={16} /> : <Sun size={16} />}
              label={t('header.appearance')}
            >
              <Segmented
                value={theme}
                options={[
                  ['light', t('header.light')],
                  ['dark', t('header.dark')],
                  ['system', t('header.system')],
                ]}
                onChange={(value) => setTheme(value as 'light' | 'dark' | 'system')}
              />
            </MenuSetting>
            <div className="account-menu__links">
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setOpen(false)
                  setSecurityOpen(true)
                }}
              >
                <ShieldCheck size={16} />
                {t('header.security', { defaultValue: 'Account security' })}
              </button>
              <NavLink
                role="menuitem"
                to={namespaceSettingsPath(namespace)}
                onClick={() => setOpen(false)}
              >
                <Settings size={16} />
                {t('header.settings')}
              </NavLink>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  void fetch('/auth/logout', {
                    method: 'POST',
                    credentials: 'same-origin',
                  }).finally(() => {
                    window.location.assign('/auth/login?return_to=%2Fdashboard')
                  })
                }}
              >
                <LogOut size={16} />
                {t('header.signOut', { defaultValue: 'Sign out' })}
              </button>
            </div>
          </div>
        )}
      </div>
      <Modal
        open={securityOpen}
        wide
        title={t('header.security')}
        onClose={() => setSecurityOpen(false)}
      >
        <iframe
          className="authguard-security-frame"
          data-testid="authguard-security-frame"
          src="/auth/account/security?embedded=1"
          title={t('header.security')}
        />
      </Modal>
    </>
  )
}

function AccountAvatar({
  logo,
  initials,
  large = false,
}: {
  logo: string
  initials: string
  large?: boolean
}) {
  return (
    <span className={`avatar${large ? ' avatar--large' : ''}`}>
      {logo ? <img src={logo} alt="" data-testid="account-brand-logo" /> : initials || 'FG'}
    </span>
  )
}

function MenuSetting({
  icon,
  label,
  children,
}: {
  icon: React.ReactNode
  label: string
  children: React.ReactNode
}) {
  return (
    <section className="menu-setting">
      <div className="menu-setting__label">
        {icon}
        <span>{label}</span>
      </div>
      {children}
    </section>
  )
}

function Segmented({
  value,
  options,
  onChange,
}: {
  value: string
  options: Array<[string, string]>
  onChange: (value: string) => void
}) {
  return (
    <div className="segmented">
      {options.map(([option, label]) => (
        <button
          type="button"
          className={value === option ? 'is-active' : ''}
          aria-pressed={value === option}
          onClick={() => onChange(option)}
          key={option}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
