-- name: InsertRequest :one
INSERT INTO requests (requester_id, status, release, release_key, comment, display_title, approved_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetRequest :one
SELECT r.*, u.username AS requester FROM requests r
JOIN users u ON u.id = r.requester_id
WHERE r.id = ?;

-- name: ListRequestsByUser :many
SELECT r.*, u.username AS requester FROM requests r
JOIN users u ON u.id = r.requester_id
WHERE r.requester_id = ?
ORDER BY r.id DESC;

-- name: ListAllRequests :many
SELECT r.*, u.username AS requester FROM requests r
JOIN users u ON u.id = r.requester_id
ORDER BY r.id DESC;

-- name: UpdateRequest :execrows
UPDATE requests SET
    status = sqlc.arg(status),
    release = sqlc.arg(release),
    release_key = sqlc.arg(release_key),
    original_release = sqlc.arg(original_release),
    display_title = sqlc.arg(display_title),
    deny_reason = sqlc.arg(deny_reason),
    error = sqlc.arg(error),
    client_id = sqlc.arg(client_id),
    client_item_id = sqlc.arg(client_item_id),
    progress = sqlc.arg(progress),
    last_progress_at = sqlc.arg(last_progress_at),
    approved_by = sqlc.arg(approved_by),
    library_item_id = sqlc.arg(library_item_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status);

-- name: ListWorkerRequests :many
SELECT * FROM requests
WHERE status IN ('approved', 'downloading', 'importing')
ORDER BY id;

-- name: ListActivityRequests :many
SELECT r.*, u.username AS requester FROM requests r
JOIN users u ON u.id = r.requester_id
WHERE r.status IN ('approved', 'downloading', 'importing', 'failed')
ORDER BY r.id DESC;

-- name: UpdateRequestProgress :execrows
UPDATE requests SET progress = ?, last_progress_at = ?, updated_at = ?
WHERE id = ? AND status = 'downloading';
