-- name: InsertLibraryItem :one
INSERT INTO library_items (request_id, display_title, release_name, release_key, folder, total_size, protocol, indexer, import_mode, imported_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertLibraryFile :exec
INSERT INTO library_files (item_id, rel_path, size) VALUES (?, ?, ?);
