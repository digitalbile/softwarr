import { useState } from 'react'
import { Alert, Button, Group, Modal, Text } from '@mantine/core'
import { messageOf } from '../errors'

interface Props {
  opened: boolean
  title: string
  message: string
  confirmLabel: string
  onConfirm: () => Promise<unknown>
  onClose: () => void
}

// Asks before a destructive action and shows the server's message on failure.
export function ConfirmDialog({ opened, title, message, confirmLabel, onConfirm, onClose }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const close = () => {
    setError('')
    onClose()
  }
  const confirm = async () => {
    setBusy(true)
    setError('')
    try {
      await onConfirm()
      close()
    } catch (err) {
      setError(messageOf(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal opened={opened} onClose={close} title={title} size="sm">
      <Text size="sm">{message}</Text>
      {error && (
        <Alert color="red" mt="sm" role="alert">
          {error}
        </Alert>
      )}
      <Group justify="flex-end" mt="md">
        <Button variant="default" onClick={close}>
          Cancel
        </Button>
        <Button color="red" loading={busy} onClick={confirm}>
          {confirmLabel}
        </Button>
      </Group>
    </Modal>
  )
}
