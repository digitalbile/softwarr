import { Alert, Button, CopyButton, Group, Loader, NumberInput, Switch, TextInput } from '@mantine/core'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../../api/client'
import type { Settings } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { SectionForm } from './SectionForm'
import { useSaveSettings, useSettings } from './useSettings'

function GeneralForm({ settings }: { settings: Settings }) {
  const save = useSaveSettings()
  const queryClient = useQueryClient()
  const [externalUrl, setExternalUrl] = useState(settings.externalUrl)
  const [commentRequired, setCommentRequired] = useState(settings.commentRequired)
  const [retention, setRetention] = useState<number | string>(settings.defaultRetentionDays)
  const [stall, setStall] = useState<number | string>(settings.stallHours)
  const [confirming, setConfirming] = useState(false)

  return (
    <>
      <SectionForm
        title="General"
        onSave={() =>
          save.mutateAsync((s) => ({
            ...s,
            externalUrl: externalUrl.trim(),
            commentRequired,
            defaultRetentionDays: Number(retention) || 0,
            stallHours: Number(stall) || 0,
          }))
        }
      >
        <TextInput
          label="External URL"
          description="The address people use to reach softwarr. Used in links and the single sign-on redirect."
          placeholder="https://softwarr.example.com"
          value={externalUrl}
          onChange={(e) => setExternalUrl(e.currentTarget.value)}
        />
        <TextInput
          label="API key"
          readOnly
          value={settings.apiKey}
          rightSectionWidth={150}
          rightSection={
            <Group gap={4} wrap="nowrap">
              <CopyButton value={settings.apiKey}>
                {({ copied, copy }) => (
                  <Button size="compact-xs" variant="default" onClick={copy}>
                    {copied ? 'Copied' : 'Copy'}
                  </Button>
                )}
              </CopyButton>
              <Button size="compact-xs" variant="default" color="red" onClick={() => setConfirming(true)}>
                Regenerate
              </Button>
            </Group>
          }
        />
        <Switch
          label="Require a reason with every request"
          checked={commentRequired}
          onChange={(e) => setCommentRequired(e.currentTarget.checked)}
        />
        <NumberInput
          label="Default retention days"
          description="How long imported items stay in the library. 0 keeps them forever."
          min={0}
          allowDecimal={false}
          value={retention}
          onChange={setRetention}
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
      <ConfirmDialog
        opened={confirming}
        title="Regenerate API key"
        message="Anything using the current key will stop working until you update it."
        confirmLabel="Regenerate"
        onClose={() => setConfirming(false)}
        onConfirm={async () => {
          await api('/settings/apikey', { method: 'POST' })
          await queryClient.invalidateQueries({ queryKey: ['settings'] })
        }}
      />
    </>
  )
}

export function GeneralSettings() {
  const q = useSettings()
  if (q.error) return <Alert color="red">{q.error.message}</Alert>
  if (!q.data) return <Loader size="sm" />
  return <GeneralForm settings={q.data} />
}
