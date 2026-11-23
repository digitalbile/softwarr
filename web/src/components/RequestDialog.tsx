import { useState } from 'react'
import { Alert, Button, Group, Modal, Stack, Text, Textarea } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Request, SearchResult } from '../api/types'
import { formatBytes } from '../format'

interface Props {
  result: SearchResult | null
  commentRequired: boolean
  onClose(): void
  onRequested(result: SearchResult): void
}

export function RequestDialog({ result, ...rest }: Props) {
  // Keyed on the release so the form starts fresh for each one.
  return (
    <Modal opened={result !== null} onClose={rest.onClose} title="Request this release" centered>
      {result && <Form key={result.key} result={result} {...rest} />}
    </Modal>
  )
}

function Form({ result, commentRequired, onClose, onRequested }: Props & { result: SearchResult }) {
  const [comment, setComment] = useState('')
  const [error, setError] = useState('')
  const create = useMutation({
    mutationFn: () => api<Request>('/requests', { method: 'POST', json: { token: result.token, comment: comment.trim() } }),
    onSuccess: () => {
      notifications.show({ message: 'Request sent.', color: 'green' })
      onRequested(result)
      onClose()
    },
    onError: (err: Error) => setError(err.message),
  })

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (commentRequired && comment.trim() === '') {
      setError('Tell us why you need this software.')
      return
    }
    setError('')
    create.mutate()
  }

  return (
    <form onSubmit={submit} noValidate>
      <Stack gap="sm">
        {error && (
          <Alert color="red" role="alert">
            {error}
          </Alert>
        )}
        <div>
          <Text fw={500}>{result.title}</Text>
          <Text size="sm" c="dimmed">
            {formatBytes(result.size)} from {result.indexer}
          </Text>
        </div>
        <Textarea
          label="Why do you need this software?"
          withAsterisk={commentRequired}
          value={comment}
          onChange={(e) => setComment(e.currentTarget.value)}
          rows={3}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Send request
          </Button>
        </Group>
      </Stack>
    </form>
  )
}
