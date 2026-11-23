import type { Release, SearchResult } from './api/types'

// POST /requests rejects unknown fields, so send only the Release part of a search result.
export function toRelease(r: SearchResult): Release {
  return {
    guid: r.guid,
    title: r.title,
    indexer: r.indexer,
    indexerId: r.indexerId,
    protocol: r.protocol,
    size: r.size,
    publishDate: r.publishDate,
    seeders: r.seeders,
    leechers: r.leechers,
    grabs: r.grabs,
    downloadUrl: r.downloadUrl,
    magnetUrl: r.magnetUrl,
    infoHash: r.infoHash,
    categories: r.categories,
  }
}
