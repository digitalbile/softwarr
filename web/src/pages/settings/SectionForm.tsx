import { Alert, Button, Group, Stack, Text, Title } from '@mantine/core'
import { type FormEvent, type ReactNode, useState } from 'react'
import { messageOf } from '../../errors'

interface Props {
  title: string
  description?: string
  onSave: () => Promise<unknown>
  actions?: ReactNode
  children: ReactNode
}

// A settings section that saves on its own and reports the result inline.
export function SectionForm({ title, description, onSave, actions, children }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    setSaved(false)
    try {
      await onSave()
      setSaved(true)
    } catch (err) {
      setError(messageOf(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit}>
      <Stack gap="sm" maw={560}>
        <Title order={4}>{title}</Title>
        {description && (
          <Text size="sm" c="dimmed">
            {description}
          </Text>
        )}
        {children}
        {error && (
          <Alert color="red" role="alert">
            {error}
          </Alert>
        )}
        <Group>
          <Button type="submit" loading={busy}>
            Save
          </Button>
          {actions}
          {saved && (
            <Text size="sm" c="green" role="status">
              Saved.
            </Text>
          )}
        </Group>
      </Stack>
    </form>
  )
}
