-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    email TEXT COLLATE NOCASE,
    password_hash TEXT NOT NULL DEFAULT '',
    is_owner INTEGER NOT NULL DEFAULT 0,
    is_admin INTEGER NOT NULL DEFAULT 0,
    auto_approve INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'pending', 'disabled')),
    must_change_password INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX users_email_unique ON users (email) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX users_single_owner ON users (is_owner) WHERE is_owner = 1;

CREATE TABLE user_identities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE oidc_states (
    state TEXT PRIMARY KEY,
    code_verifier TEXT NOT NULL,
    nonce TEXT NOT NULL,
    link_user_id INTEGER,
    expires_at INTEGER NOT NULL
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE download_clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    protocol TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 0,
    settings TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE path_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host TEXT NOT NULL,
    remote_path TEXT NOT NULL,
    local_path TEXT NOT NULL
);

CREATE TABLE requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    requester_id INTEGER NOT NULL REFERENCES users (id),
    status TEXT NOT NULL,
    release TEXT NOT NULL,
    release_key TEXT NOT NULL,
    original_release TEXT,
    comment TEXT NOT NULL DEFAULT '',
    display_title TEXT NOT NULL DEFAULT '',
    deny_reason TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    client_id INTEGER,
    client_item_id TEXT NOT NULL DEFAULT '',
    progress REAL NOT NULL DEFAULT 0,
    last_progress_at INTEGER,
    approved_by INTEGER,
    library_item_id INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX requests_open_release_key ON requests (release_key)
    WHERE status IN ('pending', 'approved', 'downloading', 'importing', 'scanning');

CREATE TABLE library_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id INTEGER,
    display_title TEXT NOT NULL,
    release_name TEXT NOT NULL,
    release_key TEXT NOT NULL,
    folder TEXT NOT NULL,
    total_size INTEGER NOT NULL DEFAULT 0,
    protocol TEXT NOT NULL DEFAULT '',
    indexer TEXT NOT NULL DEFAULT '',
    import_mode TEXT NOT NULL DEFAULT '',
    retention_days INTEGER,
    status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'expired', 'deleted')),
    imported_at INTEGER NOT NULL,
    last_downloaded_at INTEGER,
    download_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE library_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id INTEGER NOT NULL REFERENCES library_items (id) ON DELETE CASCADE,
    rel_path TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    request_id INTEGER,
    item_id INTEGER,
    read_at INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE webhooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    secret TEXT NOT NULL DEFAULT '',
    events TEXT NOT NULL DEFAULT '[]',
    enabled INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE webhooks;
DROP TABLE notifications;
DROP TABLE library_files;
DROP TABLE library_items;
DROP TABLE requests;
DROP TABLE path_mappings;
DROP TABLE download_clients;
DROP TABLE settings;
DROP TABLE oidc_states;
DROP TABLE sessions;
DROP TABLE user_identities;
DROP TABLE users;
