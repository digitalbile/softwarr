import { Badge, type BadgeProps } from '@mantine/core'

const styles: Record<string, { label: string; color: string }> = {
  // Search statuses
  available: { label: 'Available', color: 'green' },
  requested: { label: 'Requested', color: 'blue' },
  no_client: { label: 'No compatible download client', color: 'gray' },
  requestable: { label: 'Requestable', color: 'teal' },
  // Request statuses
  pending: { label: 'Pending', color: 'yellow' },
  approved: { label: 'Approved', color: 'cyan' },
  downloading: { label: 'Downloading', color: 'blue' },
  importing: { label: 'Importing', color: 'indigo' },
  scanning: { label: 'Scanning', color: 'indigo' },
  denied: { label: 'Denied', color: 'red' },
  cancelled: { label: 'Cancelled', color: 'gray' },
  failed: { label: 'Failed', color: 'red' },
  abandoned: { label: 'Abandoned', color: 'orange' },
}

export function StatusBadge({ status, ...props }: { status: string } & BadgeProps) {
  const s = styles[status] ?? { label: status, color: 'gray' }
  return (
    <Badge size="sm" variant="light" color={s.color} {...props}>
      {s.label}
    </Badge>
  )
}
