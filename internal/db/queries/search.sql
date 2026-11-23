-- name: ListAvailableLibraryKeys :many
SELECT release_key, CAST(MIN(id) AS INTEGER) AS id FROM library_items
WHERE status = 'available' AND release_key IN (sqlc.slice('keys'))
GROUP BY release_key;

-- name: ListOpenRequestKeys :many
SELECT DISTINCT release_key FROM requests
WHERE status IN ('pending', 'approved', 'downloading', 'importing', 'scanning')
  AND release_key IN (sqlc.slice('keys'));

-- name: ListEnabledClientProtocols :many
SELECT DISTINCT protocol FROM download_clients WHERE enabled = 1;
