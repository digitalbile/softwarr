import { Alert, Loader, NumberInput, Switch } from '@mantine/core'
import { useState } from 'react'
import type { Settings } from '../../api/types'
import { SectionForm } from './SectionForm'
import { useSaveSettings, useSettings } from './useSettings'

function RequestForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const [commentRequired, setCommentRequired] = useState(settings.commentRequired)
  const [stall, setStall] = useState<number | string>(settings.stallHours)
  return (
    <SectionForm
      title="Requests"
      onSave={() => save.mutateAsync((s) => ({ ...s, commentRequired, stallHours: Number(stall) || 0 }))}
    >
      <Switch
        label="Require a reason with every request"
        checked={commentRequired}
        onChange={(e) => setCommentRequired(e.currentTarget.checked)}
      />
      <NumberInput
        label="Stall threshold hours"
        description="Downloads with no progress for this long are flagged as stalled."
        min={0}
        allowDecimal={false}
        value={stall}
        onChange={setStall}
      />
    </SectionForm>
  )
}

export function RequestSettings() {
  const q = useSettings()
  if (q.error) return <Alert color="red">{q.error.message}</Alert>
  if (!q.data) return <Loader size="sm" />
  return <RequestForm settings={q.data} />
}
