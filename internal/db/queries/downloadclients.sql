-- name: ListDownloadClients :many
SELECT * FROM download_clients ORDER BY priority, id;

-- name: GetDownloadClient :one
SELECT * FROM download_clients WHERE id = ?;

-- name: CreateDownloadClient :one
INSERT INTO download_clients (name, type, protocol, enabled, priority, settings)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateDownloadClient :one
UPDATE download_clients
SET name = ?, type = ?, protocol = ?, enabled = ?, priority = ?, settings = ?
WHERE id = ?
RETURNING *;

-- name: DeleteDownloadClient :execrows
DELETE FROM download_clients WHERE id = ?;

-- name: PickDownloadClient :one
SELECT * FROM download_clients
WHERE enabled = 1 AND protocol = ?
ORDER BY priority, id
LIMIT 1;
