import { Alert, Button, Paper, PasswordInput, Stack, Text, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { notifications } from '@mantine/notifications'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { api } from '../api/client'
import { useAuth } from '../auth/useAuth'
import type { OIDCConfig } from './LoginPage'

export function ProfilePage() {
  const { user, refresh } = useAuth()
  const [params] = useSearchParams()
  const [error, setError] = useState('')
  const [linkError, setLinkError] = useState('')
  const form = useForm({ initialValues: { current: '', next: '' } })
  const oidc = useQuery({
    queryKey: ['oidc-config'],
    queryFn: () => api<OIDCConfig>('/auth/oidc/config'),
  })

  if (!user) return null

  const canLink = !!oidc.data?.enabled && user.hasPassword && !user.isOwner && !user.hasOidc

  const submit = form.onSubmit(async (values) => {
    setError('')
    try {
      await api('/auth/password', { method: 'POST', json: { current: values.current, new: values.next } })
      form.reset()
      notifications.show({ message: 'Password changed.', color: 'green' })
      await refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong.')
    }
  })

  const connect = async () => {
    setLinkError('')
    try {
      const res = await api<{ redirect: string }>('/auth/oidc/link', { method: 'POST' })
      window.location.assign(res.redirect)
    } catch (err) {
      setLinkError(err instanceof Error ? err.message : 'Something went wrong.')
    }
  }

  const urlError = params.get('error')

  return (
    <Stack maw={420}>
      <Title order={3}>Profile</Title>
      <Text size="sm">Signed in as {user.username}.</Text>
      {params.get('linked') === '1' && <Alert color="green">Your SSO account is connected.</Alert>}
      {urlError && <Alert color="red">{urlError}</Alert>}
      {user.hasPassword && (
        <Paper withBorder p="md">
          <form onSubmit={submit}>
            <Stack>
              <Title order={5}>Change password</Title>
              {error && <Alert color="red">{error}</Alert>}
              <PasswordInput label="Current password" autoComplete="current-password" required {...form.getInputProps('current')} />
              <PasswordInput label="New password" autoComplete="new-password" required {...form.getInputProps('next')} />
              <Button type="submit" loading={form.submitting}>
                Change password
              </Button>
            </Stack>
          </form>
        </Paper>
      )}
      {canLink && (
        <Paper withBorder p="md">
          <Stack>
            <Title order={5}>Single sign-on</Title>
            {linkError && <Alert color="red">{linkError}</Alert>}
            <Button variant="default" onClick={connect}>
              Connect SSO account
            </Button>
          </Stack>
        </Paper>
      )}
    </Stack>
  )
}
