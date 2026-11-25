import { Alert, Loader, NumberInput, TextInput } from '@mantine/core'
import { useState } from 'react'
import type { Settings } from '../../api/types'
import { SectionForm } from './SectionForm'
import { useSaveSettings, useSettings } from './useSettings'

function LibraryForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const [root, setRoot] = useState(settings.libraryRoot)
  const [retention, setRetention] = useState<number | string>(settings.defaultRetentionDays)
  return (
    <SectionForm
      title="Library"
      onSave={() =>
        save.mutateAsync((s) => ({ ...s, libraryRoot: root.trim(), defaultRetentionDays: Number(retention) || 0 }))
      }
    >
      <TextInput
        label="Library root path"
        description="Finished downloads are imported into folders under this path."
        value={root}
        onChange={(e) => setRoot(e.currentTarget.value)}
      />
      <NumberInput
        label="Default retention days"
        description="How long imported items stay in the library. 0 keeps them forever."
        min={0}
        allowDecimal={false}
        value={retention}
        onChange={setRetention}
      />
    </SectionForm>
  )
}

export function LibrarySettings() {
  const q = useSettings()
  if (q.error) return <Alert color="red">{q.error.message}</Alert>
  if (!q.data) return <Loader size="sm" />
  return <LibraryForm settings={q.data} />
}
