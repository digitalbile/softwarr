import { screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { expect, test } from 'vitest'
import { makeSettings, makeUser, server } from '../../test/server'
import { renderApp, setupApiServer } from '../../test/utils'

setupApiServer()

test('the general page leaves request and library settings to their own pages', async () => {
  server.use(
    http.get('*/api/v1/auth/me', () => HttpResponse.json(makeUser({ isAdmin: true }))),
    http.get('*/api/v1/settings', () => HttpResponse.json(makeSettings())),
  )
  renderApp('/settings/general')
  expect(await screen.findByLabelText('External URL')).toBeInTheDocument()
  expect(screen.queryByText('Require a reason with every request')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Stall threshold hours')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Default retention days')).not.toBeInTheDocument()
})
