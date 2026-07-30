-- +goose Up
-- T-7d33 keeps the three most recent pre-write snapshots for every editable
-- long-form document. The live document remains in its existing table; this
-- is an append-only rollback journal keyed by its stable document identity.
CREATE TABLE document_history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    document_kind TEXT NOT NULL,
    document_key  TEXT NOT NULL,
    content_json  TEXT NOT NULL,
    created_ts    REAL NOT NULL,
    actor_id      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_document_history_document
    ON document_history (document_kind, document_key, id DESC);

-- +goose Down
DROP TABLE document_history;
