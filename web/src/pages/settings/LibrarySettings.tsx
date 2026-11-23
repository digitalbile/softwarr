import { useState } from 'react'
import { Alert, Loader, TextInput } from '@mantine/core'
import type { Settings } from '../../api/types'
import { SectionForm } from './SectionForm'
import { useSaveSettings, useSettings } from './useSettings'

function LibraryForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const [root, setRoot] = useState(settings.libraryRoot)
  return (
    <SectionForm title="Library" onSave={() => save.mutateAsync((s) => ({ ...s, libraryRoot: root.trim() }))}>
      <TextInput
        label="Library root path"
        description="Finished downloads are imported into folders under this path."
        value={root}
        onChange={(e) => setRoot(e.currentTarget.value)}
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
