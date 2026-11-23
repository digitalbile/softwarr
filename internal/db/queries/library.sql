-- name: InsertLibraryItem :one
INSERT INTO library_items (request_id, display_title, release_name, release_key, folder, total_size, protocol, indexer, import_mode, imported_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertLibraryFile :exec
INSERT INTO library_files (item_id, rel_path, size) VALUES (?, ?, ?);

-- name: ListAvailableLibraryItems :many
SELECT li.id, li.display_title, li.release_name, li.folder, li.total_size, li.protocol, li.indexer, li.import_mode,
       li.retention_days, li.imported_at, li.last_downloaded_at, li.download_count,
       CAST(COALESCE(r.requester_id, 0) AS INTEGER) AS requester_id,
       CAST(COALESCE(u.username, '') AS TEXT) AS requester,
       CAST(COALESCE(r.comment, '') AS TEXT) AS comment,
       CAST(COALESCE(a.username, '') AS TEXT) AS approver
FROM library_items li
LEFT JOIN requests r ON r.id = li.request_id
LEFT JOIN users u ON u.id = r.requester_id
LEFT JOIN users a ON a.id = r.approved_by
WHERE li.status = 'available'
ORDER BY li.imported_at DESC, li.id DESC;

-- name: GetAvailableLibraryItem :one
SELECT li.id, li.display_title, li.release_name, li.folder, li.total_size, li.protocol, li.indexer, li.import_mode,
       li.retention_days, li.imported_at, li.last_downloaded_at, li.download_count,
       CAST(COALESCE(r.requester_id, 0) AS INTEGER) AS requester_id,
       CAST(COALESCE(u.username, '') AS TEXT) AS requester,
       CAST(COALESCE(r.comment, '') AS TEXT) AS comment,
       CAST(COALESCE(a.username, '') AS TEXT) AS approver
FROM library_items li
LEFT JOIN requests r ON r.id = li.request_id
LEFT JOIN users u ON u.id = r.requester_id
LEFT JOIN users a ON a.id = r.approved_by
WHERE li.id = ? AND li.status = 'available';

-- name: ListLibraryFiles :many
SELECT item_id, rel_path, size FROM library_files WHERE item_id = ? ORDER BY id;

-- name: ListAllAvailableLibraryFiles :many
SELECT f.item_id, f.rel_path, f.size FROM library_files f
JOIN library_items li ON li.id = f.item_id
WHERE li.status = 'available' ORDER BY f.id;

-- name: UpdateLibraryItemMeta :exec
UPDATE library_items SET display_title = ?, retention_days = ? WHERE id = ?;

-- name: SetLibraryItemStatus :execrows
UPDATE library_items SET status = ? WHERE id = ? AND status = 'available';

-- name: RecordLibraryDownload :exec
UPDATE library_items SET download_count = download_count + 1, last_downloaded_at = ? WHERE id = ?;

-- name: LatestTorrentImportMode :one
SELECT import_mode FROM library_items WHERE protocol = 'torrent' ORDER BY imported_at DESC, id DESC LIMIT 1;
