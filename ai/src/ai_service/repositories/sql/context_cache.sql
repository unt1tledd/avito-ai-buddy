-- name: context_cache_create
CREATE TABLE IF NOT EXISTS contexts (key TEXT PRIMARY KEY, context TEXT NOT NULL)
-- name: context_cache_get
SELECT context FROM contexts WHERE key = ?
-- name: context_cache_put
INSERT OR REPLACE INTO contexts VALUES (?, ?)
