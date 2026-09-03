-- +goose Up
-- ⚠️ THE NUMBER 00070 IS PROVISIONAL. Main's highest migration when this was
-- written was 00069, but other branches are queued at the door for the same
-- next numbers — the final number is re-taken at merge time, and nothing here
-- depends on it being 00070.
--
-- T-60 makes a pinned deliverable REPLACEABLE: the same artifact id keeps
-- pointing at the card while its content is swapped. The live row stays in
-- task_artifact; this is the append-only pre-write journal of the versions it
-- replaced, keyed by that stable artifact id — the same shape as
-- document_history (00043) and retained to the same depth
-- (documentHistoryKeepDefault, three), so an artifact's history can never grow
-- without bound.
--
-- No foreign key to task_artifact on purpose: the rows outlive nothing. The
-- remove path (remove_task_artifact) deletes them in the same transaction that
-- deletes the live row, so an owner-less version is never left behind, and the
-- blobs only those versions referenced are collected with them.
CREATE TABLE task_artifact_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id   TEXT NOT NULL,
    kind          TEXT NOT NULL,
    attachment_id TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    created_ts    REAL NOT NULL,
    created_by    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_task_artifact_history_artifact
    ON task_artifact_history (artifact_id, id DESC);

-- +goose Down
DROP TABLE task_artifact_history;
