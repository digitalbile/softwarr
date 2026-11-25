import { Alert, Button, Center, Paper, PasswordInput, Stack, Text, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { api } from '../api/client'
import { FullPageLoader } from '../auth/guards'
import { useAuth } from '../auth/useAuth'

export function ChangePasswordPage() {
  const { user, loading, refresh } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { current: '', next: '' } })

  if (loading) return <FullPageLoader />
  if (!user) return <Navigate to="/login" replace />

  const submit = form.onSubmit(async (values) => {
    setError('')
    try {
      await api('/auth/password', { method: 'POST', json: { current: values.current, new: values.next } })
      await refresh()
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong.')
    }
  })

  return (
    <Center mih="100vh" p="md">
      <Paper withBorder p="lg" w={340}>
        <form onSubmit={submit}>
          <Stack>
            <Title order={3}>Choose a new password</Title>
            <Text size="sm" c="dimmed">
              An admin set your current password. Pick one of your own to continue.
            </Text>
            {error && <Alert color="red">{error}</Alert>}
            <PasswordInput
              label="Current password"
              autoComplete="current-password"
              required
              {...form.getInputProps('current')}
            />
            <PasswordInput label="New password" autoComplete="new-password" required {...form.getInputProps('next')} />
            <Button type="submit" loading={form.submitting}>
              Change password
            </Button>
          </Stack>
        </form>
      </Paper>
    </Center>
  )
}
