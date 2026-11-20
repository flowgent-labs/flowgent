import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { AppShell } from './shell'
import { useAppStore } from './store'

afterEach(() => {
  useAppStore.setState({
    authGuard: {
      principalId: '',
      displayName: '',
      applicationName: 'Flowgent',
      logo: '',
    },
  })
})

describe('AppShell account experience', () => {
  it('renders AuthGuard application branding and opens security settings in place', async () => {
    useAppStore.setState({
      authGuard: {
        principalId: 'P_admin',
        displayName: 'Flowgent Admin',
        applicationName: 'Security Autonomy Fixer',
        logo: '/auth/assets/themes/custom/flowgent.svg',
      },
    })
    const router = createMemoryRouter([
      {
        path: '/',
        element: <AppShell />,
        children: [{ index: true, element: <div>console page</div> }],
      },
    ])
    const user = userEvent.setup()
    render(<RouterProvider router={router} />)

    await user.click(screen.getByRole('button', { name: 'Account menu' }))
    expect(screen.getAllByText('Flowgent Admin')).not.toHaveLength(0)
    expect(screen.getAllByText('Security Autonomy Fixer')).not.toHaveLength(0)
    expect(screen.getAllByTestId('account-brand-logo')[0]).toHaveAttribute(
      'src',
      '/auth/assets/themes/custom/flowgent.svg',
    )

    await user.click(screen.getByRole('menuitem', { name: 'Account security' }))
    expect(screen.getByRole('dialog', { name: 'Account security' })).toBeVisible()
    expect(screen.getByTestId('authguard-security-frame')).toHaveAttribute(
      'src',
      '/auth/account/security?embedded=1',
    )
    expect(router.state.location.pathname).toBe('/')
  })
})
