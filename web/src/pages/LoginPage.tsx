import { Alert, Button, Center, Paper, PasswordInput, Stack, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router'
import { api, urlBase } from '../api/client'
import { FullPageLoader } from '../auth/guards'
import { useAuth } from '../auth/useAuth'

export interface OIDCConfig {
  enabled: boolean
  buttonLabel: string
}

export function LoginPage() {
  const { user, loading, setupNeeded, refresh } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const urlError = params.get('error') ?? ''
  const [error, setError] = useState('')
  const form = useForm({ initialValues: { username: '', password: '' } })
  const oidc = useQuery({
    queryKey: ['oidc-config'],
    queryFn: () => api<OIDCConfig>('/auth/oidc/config'),
  })

  if (loading) return <FullPageLoader />
  if (setupNeeded) return <Navigate to="/setup" replace />
  if (user) {
    // A failed SSO link bounces here while still signed in.
    return <Navigate to={urlError ? `/profile?error=${encodeURIComponent(urlError)}` : '/'} replace />
  }

  const submit = form.onSubmit(async (values) => {
    setError('')
    try {
      await api('/auth/login', { method: 'POST', json: values })
      await refresh()
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong.')
    }
  })

  const shown = error || urlError

  return (
    <Center mih="100vh" p="md">
      <Paper withBorder p="lg" w={340}>
        <form onSubmit={submit}>
          <Stack>
            <Title order={3}>Sign in to softwarr</Title>
            {shown && <Alert color="red">{shown}</Alert>}
            <TextInput label="Username" autoComplete="username" required {...form.getInputProps('username')} />
            <PasswordInput label="Password" autoComplete="current-password" required {...form.getInputProps('password')} />
            <Button type="submit" loading={form.submitting}>
              Sign in
            </Button>
            {oidc.data?.enabled && (
              <Button component="a" href={`${urlBase}/api/v1/auth/oidc/login`} variant="default">
                {oidc.data.buttonLabel || 'Sign in with SSO'}
              </Button>
            )}
          </Stack>
        </form>
      </Paper>
    </Center>
  )
}
