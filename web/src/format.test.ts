import { expect, test } from 'vitest'
import { formatAge, formatBytes } from './format'

test('formatBytes and formatAge', () => {
  expect(formatBytes(0)).toBe('0 B')
  expect(formatBytes(512)).toBe('512 B')
  expect(formatBytes(1024)).toBe('1.0 KiB')
  expect(formatBytes(1536 * 1024)).toBe('1.5 MiB')
  expect(formatBytes(3 * 1024 ** 3)).toBe('3.0 GiB')

  const now = Date.parse('2026-11-23T12:00:00Z')
  expect(formatAge('2026-11-20T12:00:00Z', now)).toBe('3d')
  expect(formatAge('2026-11-23T07:00:00Z', now)).toBe('5h')
  expect(formatAge('2026-11-23T11:20:00Z', now)).toBe('40m')
  expect(formatAge('2026-11-23T12:00:30Z', now)).toBe('0m')
})
