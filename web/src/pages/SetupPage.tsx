import { Alert, Button, Center, Paper, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { api } from '../api/client'
import { FullPageLoader } from '../auth/guards'
import { useAuth } from '../auth/useAuth'

export function SetupPage() {
  const { setupNeeded, loading, user, refresh } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { username: '', password: '' } })

  if (loading) return <FullPageLoader />
  if (!setupNeeded) return <Navigate to={user ? '/' : '/login'} replace />

  const submit = form.onSubmit(async (values) => {
    setError('')
    try {
      await api('/setup', { method: 'POST', json: values })
      await refresh()
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong.')
    }
  })

  return (
    <Center mih="100vh" p="md">
      <Paper withBorder p="lg" w={380}>
        <form onSubmit={submit}>
          <Stack>
            <Title order={3}>Create the owner account</Title>
            <Text size="sm" c="dimmed">
              This is the first account for softwarr. The owner always signs in with a password, even if you set up SSO
              later.
            </Text>
            {error && <Alert color="red">{error}</Alert>}
            <TextInput label="Username" autoComplete="username" required {...form.getInputProps('username')} />
            <PasswordInput label="Password" autoComplete="new-password" required {...form.getInputProps('password')} />
            <Button type="submit" loading={form.submitting}>
              Create account
            </Button>
          </Stack>
        </form>
      </Paper>
    </Center>
  )
}
