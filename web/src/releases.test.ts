import { expect, test } from 'vitest'
import type { SearchResult } from './api/types'
import { toRelease } from './releases'

test('toRelease keeps only Release fields', () => {
  const result: SearchResult = {
    guid: 'g', title: 't', indexer: 'i', indexerId: 1, protocol: 'torrent', size: 5,
    publishDate: '2026-01-01T00:00:00Z', seeders: 3, leechers: null, grabs: null,
    downloadUrl: 'd', magnetUrl: '', infoHash: 'h', categories: [4000],
    key: 'k', status: 'requestable', libraryItemId: null,
  }
  const rel = toRelease(result)
  expect(Object.keys(rel).sort()).toEqual(
    ['categories', 'downloadUrl', 'grabs', 'guid', 'indexer', 'indexerId', 'infoHash', 'leechers', 'magnetUrl', 'protocol', 'publishDate', 'seeders', 'size', 'title'],
  )
  expect(rel.title).toBe('t')
})
