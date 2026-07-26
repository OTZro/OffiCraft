-- T-66a2 L3: the durable half of the per-warden command FIFO (spec/sse.md §7).
--
-- The queue itself stays in memory (hub.wardenCmds); this table MIRRORS the one
-- verb that cannot be re-derived after a process death — `update`, the owner's
-- upgrade click. START / STOP / UNINSTALL are deliberately NOT stored: the
-- reconcile producer re-decides them from observed presence within one cadence,
-- and a START frame carries a member_token (a short-lived secret that must not
-- be written at rest, and would be stale by the time anything replayed it).
--
-- The natural key is (warden_id, verb, member_id), not a rowid: two identical
-- pending upgrades for the same machine are ONE order, so the queue is bounded
-- by the roster and a flapping warden can never grow a backlog. enqueued_ts is
-- the FIFO order on restore AND the expiry anchor (see wardenCommandTTL) — it is
-- deliberately NOT refreshed by a re-enqueue, so a repeatedly requeued command
-- still ages out instead of living forever.

-- +goose Up
CREATE TABLE warden_command_queue (
    warden_id   TEXT NOT NULL,
    verb        TEXT NOT NULL,
    member_id   TEXT NOT NULL,
    frame       TEXT NOT NULL,
    enqueued_ts REAL NOT NULL,
    PRIMARY KEY (warden_id, verb, member_id)
);

CREATE INDEX idx_warden_command_queue_ts ON warden_command_queue (enqueued_ts);

-- +goose Down
DROP INDEX idx_warden_command_queue_ts;
DROP TABLE warden_command_queue;
