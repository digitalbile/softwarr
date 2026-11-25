import { Button, Text } from '@mantine/core'
import { useState } from 'react'
import { messageOf } from '../../errors'

export interface TestOutcome {
  ok: boolean
  error?: string
}

interface Props {
  run: () => Promise<TestOutcome>
  okText?: string
}

// Runs a connection test and shows the result next to the button.
export function TestButton({ run, okText = 'Connection works.' }: Props) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<TestOutcome | null>(null)

  const click = async () => {
    setBusy(true)
    setResult(null)
    try {
      setResult(await run())
    } catch (err) {
      setResult({ ok: false, error: messageOf(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Button variant="default" loading={busy} onClick={click}>
        Test
      </Button>
      {result && (
        <Text size="sm" c={result.ok ? 'green' : 'red'} role="status">
          {result.ok ? okText : result.error || 'The test failed.'}
        </Text>
      )}
    </>
  )
}
