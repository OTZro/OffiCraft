-- +goose Up
-- T-14 項目 7 — the SECOND intent, split out of desired_state.
--
-- desired_state carried two different owner intents at once: HOW HARD this
-- member is being taken down (停止 / 加速停止 / 強制停止) and WHETHER it should
-- be running afterwards. One column cannot hold both, so a 重啟 verb arriving
-- on a member the owner had already stopped had nowhere to record "and bring it
-- back up" — the server either refused (refocus: 409) or stored the new
-- machine / model and did nothing (relocate / 換 model: a clean 200).
--
--   0  nothing is waiting to come back up (every pre-column row starts here).
--   1  the owner's LAST action on this member was a 重啟 intent while it was on
--      its way down: honour the stronger stop, then bring it up.
--
-- Owner 2026-08-30 (rc-bc1b029a3aa2): 「一個重啟的 intention 遇上一個更強硬的
-- 下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」.
--
-- 「要不要起來」 is LAST-WRITER-WINS (a down verb clears this, a 重啟 verb sets
-- it); 「下線用多強」 stays a RATCHET on the existing ladder. Splitting them is
-- the whole change — the ladder is untouched.
--
-- Deliberately NOT on the wire (no DTO field, no spec change), like
-- session_boot_ts and handover_noticed_ts: it is consumed by the reconcile tick
-- at the converged-offline edge, and what the cockpit shows is the receipt the
-- verb already stamps in last_op_reason.
ALTER TABLE member ADD COLUMN restart_after_stop INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE member DROP COLUMN restart_after_stop;
