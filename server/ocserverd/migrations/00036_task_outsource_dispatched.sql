-- +goose Up
-- T-8a67. Until now "was this an EXPLICIT 發包?" was INFERRED from the very
-- columns that carry the spec (outsource_model/effort/machine non-empty). That
-- inference is why a task whose MANUAL routes it to outsource could never carry
-- a resolved spec of its own: writing one would have made it impersonate a
-- dispatch, skipping the scheduler's spawn gate (outsource_sched.go arm ④,
-- authorization by the task's CREATOR) — so it carried nothing at all, and a
-- manual that named no machine left the minted worker with no placement and no
-- way to ever boot.
--
-- Make the discriminator EXPLICIT instead, so the spec columns are free to hold
-- a creator SNAPSHOT for a manual-driven task:
--   dispatched=1 → an explicit 發包 (create/reassign target.kind=outsource); the
--                  columns are the AUTHORITATIVE resolved target and the sched
--                  gate is skipped (already authorized at the handler).
--   dispatched=0 → no explicit target; the columns are a FALLBACK snapshot of
--                  the creator's own runtime/model/effort/machine, consulted
--                  only for what the live type manual leaves unset. The gate
--                  still runs.
--
-- Backfill reproduces the OLD inference exactly, so every in-flight row keeps
-- the classification it had a moment ago (a dispatch always floors effort to
-- 'medium', so a real dispatch is never missed; outsource_runtime is deliberately
-- NOT part of it — PutTask normalizes it on every write, so it is never blank
-- and would mark every task as dispatched).
ALTER TABLE task ADD COLUMN outsource_dispatched INTEGER NOT NULL DEFAULT 0;

UPDATE task
   SET outsource_dispatched = 1
 WHERE outsource_model <> '' OR outsource_effort <> '' OR outsource_machine <> '';

-- +goose Down
--
-- ⚠️ THE ROLLBACK IS ASYMMETRIC — read this before running it. Dropping the column
-- is only safe while no row created by the NEW code exists. After this lands,
-- every manual-driven outsource task is written with outsource_effort='medium'
-- (the type manual's assignee schema default) and dispatched=0. Roll the DB back
-- WITHOUT rolling the code back and the retired inference — "any of
-- model/effort/machine non-empty ⇒ explicit dispatch" — re-reads that whole
-- population as explicit dispatches, which SKIPS the scheduler's by-creator spawn
-- gate (outsource_sched.go arm ④). That is the exact impersonation bug this
-- migration exists to make impossible, reintroduced in bulk rather than one row
-- at a time.
--
-- So the recovery direction is FORWARD, and it is cheap: fix the code and, if a
-- rollback already happened, re-add the column and re-derive it
-- (`UPDATE task SET outsource_dispatched = …`) from what those rows actually are.
-- Going down is not the safe default here just because it is the reversible one.
ALTER TABLE task DROP COLUMN outsource_dispatched;
