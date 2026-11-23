import { useState } from 'react'
import { Alert, Loader, MultiSelect, PasswordInput, TextInput } from '@mantine/core'
import { api } from '../../api/client'
import type { Settings } from '../../api/types'
import { SectionForm } from './SectionForm'
import { TestButton } from './TestButton'
import { useSaveSettings, useSettings } from './useSettings'

const standardCategories = [
  { value: '4000', label: '4000 PC' },
  { value: '4010', label: '4010 PC/0day' },
  { value: '4020', label: '4020 PC/ISO' },
  { value: '4030', label: '4030 PC/Mac' },
  { value: '4040', label: '4040 PC/Mobile-Other' },
  { value: '4050', label: '4050 PC/Games' },
  { value: '4060', label: '4060 PC/Mobile-iOS' },
  { value: '4070', label: '4070 PC/Mobile-Android' },
]

function IndexerForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const [url, setUrl] = useState(settings.prowlarr.url)
  const [apiKey, setApiKey] = useState(settings.prowlarr.apiKey)
  const [cats, setCats] = useState(settings.prowlarr.categories.map(String))
  const [search, setSearch] = useState('')

  // Typed numeric ids that aren't in the standard list become options too.
  const extra = new Set(cats)
  if (/^\d+$/.test(search)) extra.add(search)
  const data = [
    ...standardCategories,
    ...[...extra].filter((v) => !standardCategories.some((c) => c.value === v)).map((v) => ({ value: v, label: v })),
  ]

  return (
    <SectionForm
      title="Indexers"
      description="softwarr searches through Prowlarr."
      onSave={() =>
        save.mutateAsync((s) => ({
          ...s,
          prowlarr: { url: url.trim(), apiKey: apiKey.trim(), categories: cats.map(Number).sort((a, b) => a - b) },
        }))
      }
      actions={<TestButton okText="Prowlarr responded." run={() => api('/settings/prowlarr/test', { method: 'POST', json: { url: url.trim(), apiKey: apiKey.trim() } })} />}
    >
      <TextInput label="Prowlarr URL" placeholder="http://prowlarr:9696" value={url} onChange={(e) => setUrl(e.currentTarget.value)} />
      <PasswordInput label="API key" value={apiKey} onChange={(e) => setApiKey(e.currentTarget.value)} />
      <MultiSelect
        label="Categories"
        description="Pick from the list or type a numeric category id."
        searchable
        data={data}
        value={cats}
        searchValue={search}
        onSearchChange={setSearch}
        onChange={(v) => {
          setCats(v)
          setSearch('')
        }}
        nothingFoundMessage="Type a numeric id"
      />
    </SectionForm>
  )
}

export function IndexerSettings() {
  const q = useSettings()
  if (q.error) return <Alert color="red">{q.error.message}</Alert>
  if (!q.data) return <Loader size="sm" />
  return <IndexerForm settings={q.data} />
}
