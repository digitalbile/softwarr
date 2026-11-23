import { useState } from 'react'
import { Alert, Button, CopyButton, Group, Loader, PasswordInput, Switch, Text, TextInput } from '@mantine/core'
import type { Settings } from '../../api/types'
import { SectionForm } from './SectionForm'
import { useSaveSettings, useSettings } from './useSettings'

export function redirectUri(externalUrl: string) {
  return `${externalUrl.trim().replace(/\/+$/, '')}/api/v1/auth/oidc/callback`
}

function AuthForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const [o, setO] = useState({ ...settings.oidc, scopes: settings.oidc.scopes.join(' ') })
  const set = <K extends keyof typeof o>(k: K, v: (typeof o)[K]) => setO((prev) => ({ ...prev, [k]: v }))
  const hasUrl = settings.externalUrl.trim() !== ''
  const uri = redirectUri(settings.externalUrl)

  return (
    <SectionForm
      title="Authentication"
      description="Let people sign in with an OpenID Connect provider. Local accounts keep working."
      onSave={() =>
        save.mutateAsync((s) => ({
          ...s,
          oidc: { ...o, scopes: o.scopes.split(/[\s,]+/).filter(Boolean) },
        }))
      }
    >
      <Switch
        label="Enable single sign-on"
        checked={o.enabled}
        disabled={!hasUrl && !o.enabled}
        onChange={(e) => set('enabled', e.currentTarget.checked)}
      />
      {!hasUrl && (
        <Text size="sm" c="dimmed">
          Set the external URL in General settings before enabling single sign-on.
        </Text>
      )}
      {hasUrl && (
        <div>
          <Text size="sm" fw={500}>
            Redirect URI
          </Text>
          <Text size="xs" c="dimmed">
            Add this to your provider as an allowed redirect.
          </Text>
          <Group gap="xs" mt={4}>
            <Text size="sm" ff="monospace">
              {uri}
            </Text>
            <CopyButton value={uri}>
              {({ copied, copy }) => (
                <Button size="compact-xs" variant="default" onClick={copy}>
                  {copied ? 'Copied' : 'Copy'}
                </Button>
              )}
            </CopyButton>
          </Group>
        </div>
      )}
      <TextInput label="Issuer URL" value={o.issuer} onChange={(e) => set('issuer', e.currentTarget.value)} />
      <TextInput label="Client ID" value={o.clientId} onChange={(e) => set('clientId', e.currentTarget.value)} />
      <PasswordInput label="Client secret" value={o.clientSecret} onChange={(e) => set('clientSecret', e.currentTarget.value)} />
      <TextInput label="Scopes" description="Separate with spaces." value={o.scopes} onChange={(e) => set('scopes', e.currentTarget.value)} />
      <TextInput label="Button label" placeholder="Sign in with SSO" value={o.buttonLabel} onChange={(e) => set('buttonLabel', e.currentTarget.value)} />
      <Switch label="Create accounts on first sign-in" checked={o.autoProvision} onChange={(e) => set('autoProvision', e.currentTarget.checked)} />
      <Switch
        label="Link to existing accounts by verified email"
        checked={o.linkVerifiedEmail}
        onChange={(e) => set('linkVerifiedEmail', e.currentTarget.checked)}
      />
      {o.linkVerifiedEmail && (
        <Alert color="yellow">
          Anyone who can verify an email address with your provider can sign in as the local account with that email. Only turn this on if you trust the provider.
        </Alert>
      )}
      <Group grow align="flex-start">
        <TextInput label="Admin claim" description="Claim that marks admins." value={o.adminClaim} onChange={(e) => set('adminClaim', e.currentTarget.value)} />
        <TextInput label="Admin value" description="Value that grants admin." value={o.adminValue} onChange={(e) => set('adminValue', e.currentTarget.value)} />
      </Group>
    </SectionForm>
  )
}

export function AuthSettings() {
  const q = useSettings()
  if (q.error) return <Alert color="red">{q.error.message}</Alert>
  if (!q.data) return <Loader size="sm" />
  return <AuthForm settings={q.data} />
}
