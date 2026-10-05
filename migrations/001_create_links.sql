CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    original_url TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT,
    visits INTEGER NOT NULL DEFAULT 0 CHECK (visits >= 0)
);

CREATE INDEX IF NOT EXISTS idx_links_expires_at ON links (expires_at);
