const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return i === 0 ? `${Math.round(v)} B` : `${v.toFixed(1)} ${units[i]}`
}

export function formatAge(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const mins = Math.max(0, Math.floor((now - t) / 60000))
  if (mins >= 1440) return `${Math.floor(mins / 1440)}d`
  if (mins >= 60) return `${Math.floor(mins / 60)}h`
  return `${mins}m`
}

// Newznab standard PC categories, plus the other groups an indexer is likely
// to tag software with. Anything else shows as its id.
const categoryNames: Record<number, string> = {
  1000: 'Console',
  4000: 'PC',
  4010: 'PC/0day',
  4020: 'PC/ISO',
  4030: 'PC/Mac',
  4040: 'PC/Mobile-Other',
  4050: 'PC/Games',
  4060: 'PC/Mobile-iOS',
  4070: 'PC/Mobile-Android',
  8000: 'Other',
}

export function formatCategories(ids: number[]): string {
  return ids.map((id) => categoryNames[id] ?? String(id)).join(', ')
}

// Newznab reserves ids from 100000 for each indexer's own categories.
const firstCustomCategory = 100000

const group = (id: number) => Math.floor(id / 1000) * 1000

// shortCategories names a release's categories in a few words. It leaves out
// indexer-specific ids when there are standard ones, and a group such as PC
// when a subcategory of it is listed. When more than two remain, it names
// only their groups.
export function shortCategories(ids: number[]): string {
  const standard = ids.filter((id) => id < firstCustomCategory)
  if (standard.length === 0) return formatCategories(ids)
  const specific = standard.filter((id) => id !== group(id) || !standard.some((o) => o !== id && group(o) === id))
  if (specific.length <= 2) return formatCategories(specific)
  return formatCategories([...new Set(specific.map(group))])
}
