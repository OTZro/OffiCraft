-- +goose Up
-- T-6020 「誰凍的」— attribution for the frozen priority.
--
-- Until now `frozen` was, by gate, the owner's knob alone (api_tasks.go
-- HandleSetTaskPriority… answered 403 "frozen is the owner's knob" to everyone
-- else), so a frozen ticket needed no attribution: there was exactly one actor
-- it could have been. The owner's 2026-07-26 governance ruling opens frozen to
-- the admin_agent class AND to the task's own executor, which deletes that
-- inference — and with it the owner's ability to tell "I stopped this" from "an
-- agent stopped this", which is the whole meaning of a freeze (老闆喊停).
--
-- So the freezer becomes a persisted field rather than an assumption:
--
--   frozen_by  '' = the task is not frozen (and every pre-column row, which is
--              the honest state — nothing recorded who froze those). Otherwise
--              the verified token sub of the write that set priority='frozen':
--              the wireOwnerID literal for owner scope, else the member /
--              outsource-worker id. Cleared by the write that moves the task
--              off frozen, so it is never a stale name on a running task.
--
-- A constant-DEFAULT ADD COLUMN (cheap metadata op, no table rebuild).
ALTER TABLE task ADD COLUMN frozen_by TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task DROP COLUMN frozen_by;
