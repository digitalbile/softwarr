import { Alert, Button, Group, Modal, Stack, Text, Textarea } from '@mantine/core'
import { useState } from 'react'
import { isConflict } from '../api/client'
import type { Request } from '../api/types'

interface Props {
  request: Request | null
  onClose(): void
  onDeny(id: number, body: { reason?: string }): Promise<void>
}

export function DenyDialog({ request, onClose, onDeny }: Props) {
  return (
    <Modal opened={request !== null} onClose={onClose} title="Deny this request" centered>
      {request && <Form key={request.id} request={request} onClose={onClose} onDeny={onDeny} />}
    </Modal>
  )
}

function Form({ request, onClose, onDeny }: Props & { request: Request }) {
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await onDeny(request.id, reason.trim() ? { reason: reason.trim() } : {})
      onClose()
    } catch (err) {
      // The page shows conflicts, so close and let it.
      if (isConflict(err)) onClose()
      else setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit}>
      <Stack gap="sm">
        {error && (
          <Alert color="red" role="alert">
            {error}
          </Alert>
        )}
        <Text fw={500}>{request.displayTitle || request.release.title}</Text>
        <Textarea
          label="Reason (optional)"
          value={reason}
          onChange={(e) => setReason(e.currentTarget.value)}
          rows={3}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" color="red" loading={busy}>
            Deny
          </Button>
        </Group>
      </Stack>
    </form>
  )
}
