import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { expect, test } from 'vitest'
import { makeUser, server } from '../../test/server'
import { renderApp, useApiServer } from '../../test/utils'

useApiServer()

const row = (t: string) => screen.getAllByText(t).map((e) => e.closest('tr')).find(Boolean)!

function setup(extra: Parameters<typeof server.use> = []) {
  server.use(
    ...extra,
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ id: 1, username: 'root', isAdmin: true, isOwner: true }))),
    http.get('*/api/v1/users', () =>
      HttpResponse.json([
        makeUser({ id: 1, username: 'root', isOwner: true, isAdmin: true }),
        makeUser({ id: 2, username: 'sam' }),
        makeUser({ id: 3, username: 'sso-sam', hasPassword: false, hasOidc: true, status: 'pending' }),
        makeUser({ id: 4, username: 'both', hasOidc: true }),
      ]),
    ),
  )
  renderApp('/settings/users')
}

test('owner row has no destructive controls', async () => {
  setup()
  await screen.findByText('sam')
  const owner = within(row('root'))
  expect(owner.getByText('Owner')).toBeInTheDocument()
  expect(owner.queryByRole('button', { name: /Disable|Delete/ })).not.toBeInTheDocument()
  expect(within(row('sam')).getByRole('button', { name: 'Delete sam' })).toBeInTheDocument()
  expect(within(row('sam')).getByRole('button', { name: 'Disable sam' })).toBeInTheDocument()
})

test('sign-in type and pending approval', async () => {
  setup()
  await screen.findByText('sam')
  expect(within(row('sam')).getByText('Local')).toBeInTheDocument()
  expect(within(row('sso-sam')).getByText('SSO')).toBeInTheDocument()
  expect(within(row('both')).getByText('Local + SSO')).toBeInTheDocument()
  expect(within(row('sso-sam')).getByRole('button', { name: 'Approve sso-sam' })).toBeInTheDocument()
})

test('delete conflict shows the server message', async () => {
  setup([http.delete('*/api/v1/users/2', () => HttpResponse.json({ error: 'This user has requests. Disable the account instead.' }, { status: 409 }))])
  await userEvent.click(await screen.findByRole('button', { name: 'Delete sam' }))
  await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
  expect(await screen.findByText('This user has requests. Disable the account instead.')).toBeInTheDocument()
})
