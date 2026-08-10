-- +goose Up
-- T-f059 定期訊息: one row = one recurring wall-clock slot bound to a member.
-- When a slot comes due the server delivers `body` to that member down the
-- ORDINARY chat path (synthetic sender `sched:<id>`) — no new delivery
-- semantics are invented, so the message inherits chat's durable offline
-- mailbox, its online SSE push and its on-wake catch-up for free. It is the
-- clock-driven twin of webhook_endpoint (00007): identical shape, the trigger
-- swapped from an inbound call to a clock.
--
-- No FK on member_id (the 00001 decree, same as task_artifact/00022): the id IS
-- the attribution edge and a second FK edge would be a second source of truth.
-- member_id may name an `ow-` outsource worker, which is a row whose lifetime is
-- bound to one task — a cascade would be modelling a lifecycle this table does
-- not own.
--
-- 🔴 last_fired_slot stores the IDENTIFIER OF THE SLOT already delivered
-- (`2026-08-10T09:00+08:00`), NOT a "last run at" timestamp. Every tick
-- recomputes the most recently elapsed slot and fires only when it is STRICTLY
-- LATER than the one this column names. That single choice carries three
-- acceptance conditions:
--   * a server restart does not resend — the recomputed slot compares equal,
--     so the tick skips; nothing lives in memory to be lost on upgrade;
--   * missed slots are not backfilled — only the MOST RECENT slot is ever
--     considered, so three days of downtime delivers one message, not thirty;
--   * a freshly created schedule does not fire immediately — creation seeds this
--     column with the slot most recently elapsed AT creation time.
-- A "last run at" timestamp would express none of them: comparing clocks means
-- re-deriving "did this slot already go out" from two moving quantities, and a
-- resend looks EXACTLY like a normal delivery — nothing would ever alarm.
--
-- ⚠️ "By construction" is what this column ORIGINALLY claimed, and it was too
-- strong: the storage shape guarantees nothing on its own, because the three
-- conditions all rest on the slot computation being monotonic in `now`, which
-- is a property of schedule_slot.go and not of this table. It was in fact false
-- when this feature shipped — in zones that spring forward AT MIDNIGHT
-- (America/Santiago, America/Havana) the computed slot walked backwards and two
-- already-delivered slots went out again — and it was false silently, because a
-- duplicate delivery is indistinguishable from a correct one. The fire test is
-- now an ORDERING one (strictly later, never merely different), so the cursor
-- is a ratchet: the worst a wrong slot computation can now do is skip a
-- delivery. That is a guard, not a proof; the pinning lives in the DST tests in
-- scheduled_message_test.go.
--
-- last_fired_ts is the human-facing companion (when a delivery actually
-- happened); it deliberately takes NO part in the fire/skip decision.
CREATE TABLE scheduled_message (
    id              TEXT PRIMARY KEY,          -- "sch-" + 12 hex (api mint)
    member_id       TEXT NOT NULL,             -- recipient; may be an ow- worker (no FK — see above)
    label           TEXT NOT NULL DEFAULT '',  -- human-facing name; also rides meta.scheduled.label
    body            TEXT NOT NULL,             -- the message text, delivered verbatim
    cadence         TEXT NOT NULL CHECK (cadence IN ('daily', 'weekly', 'monthly')),
    -- weekly reads day_of_week (0=Sunday..6=Saturday); monthly reads
    -- day_of_month (1-31, a month lacking the day is SKIPPED entirely per
    -- iCalendar RFC 5545, never clamped); daily reads neither.
    day_of_week     INTEGER NOT NULL DEFAULT 0,
    day_of_month    INTEGER NOT NULL DEFAULT 1,
    hour            INTEGER NOT NULL DEFAULT 0,
    minute          INTEGER NOT NULL DEFAULT 0,
    -- IANA name; the wall clock is ALWAYS read in this zone, never the host's.
    timezone        TEXT NOT NULL DEFAULT 'Asia/Taipei',
    -- the revocation toggle, NOT a lifecycle: DELETE is the permanent removal.
    status          TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    last_fired_slot TEXT NOT NULL DEFAULT '',   -- the slot IDENTIFIER (see above)
    last_fired_ts   REAL NOT NULL DEFAULT 0.0,  -- human-facing only
    created_ts      REAL NOT NULL DEFAULT 0.0
);
CREATE INDEX idx_scheduled_message_member ON scheduled_message (member_id);

-- +goose Down
DROP INDEX idx_scheduled_message_member;
DROP TABLE scheduled_message;
