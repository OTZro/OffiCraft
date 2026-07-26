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
ALTER TABLE task DROP COLUMN outsource_dispatched;
