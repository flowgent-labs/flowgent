import {
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
import { useAppStore } from './store'
import { namespaceSettingsPath } from './paths'

const globalNav = [
  { to: '/dashboard', label: 'nav.dashboard', icon: CircleGauge },
  { to: '/flows', label: 'nav.flows', icon: Waypoints },
  { to: '/memory/run', label: 'nav.memory', icon: BrainCircuit },
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
      <header className="topbar">
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
        <AccountMenu />
      </header>
      <main className="page">
        <Outlet />
      </main>
    </div>
  )
}

function AccountMenu() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const locale = useAppStore((state) => state.locale)
  const theme = useAppStore((state) => state.theme)
  const setLocale = useAppStore((state) => state.setLocale)
  const setTheme = useAppStore((state) => state.setTheme)
  const namespace = useAppStore((state) => state.namespace)
  const displayName = t('header.account')
  const secondary = 'AuthGuard'
  const initials = 'AG'

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
    <div
      className="account"
      ref={menuRef}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
    >
      <button
        ref={buttonRef}
        type="button"
        className="account__button"
        aria-label={t('header.account')}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <span className="avatar">{initials}</span>
        <span className="account__identity">
          <strong>{displayName}</strong>
          <span>{secondary}</span>
        </span>
        <ChevronDown size={15} />
      </button>
      {open && (
        <div className="account-menu" role="menu">
          <div className="account-menu__heading">
            <span className="avatar avatar--large">{initials}</span>
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
            <a role="menuitem" href="/auth/account/security">
              <ShieldCheck size={16} />
              {t('header.security', { defaultValue: 'Account security' })}
            </a>
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
