-- +goose Up
-- T-30f1 任務手冊的 SOP 與學習經驗各有自己的字數上限. Splits the one manual cap
-- into the two suffixed keys the two documents now answer to, and retires the
-- key that named the whole artefact.
--
-- WHY THE VALUE IS COPIED TO BOTH AND NOT INHERITED BY ONE: the stored value is
-- whatever the owner last raised the manual cap to, and BOTH of the manual's
-- long documents were sized against it — an SOP that is legal today was written
-- under that number. Letting either half fall back to the code default would
-- LOWER an effective cap, which is the one thing the floor=shipped-default rule
-- (api_settings.go, owner 2026-07-31) exists to prevent: every document already
-- over the smaller number would enter shrink-only mode without anyone asking
-- for it. An install that never raised the cap has no row here at all, and both
-- new keys are then absent and read their code defaults (settings.go: absent =
-- default) — the copy is a no-op exactly when there is nothing to preserve.
--
-- WHY THE OLD KEY IS DELETED RATHER THAN LEFT AS ONE OF THE TWO: `get_settings`
-- shows an agent key NAMES with no descriptions. `doc.cap_chars.manual` sitting
-- beside `.manual_sop` and `.manual_learnings` reads as "the manual's default",
-- and someone raising it would believe they had moved both halves. This is the
-- same ruling migration 00048 applied to the unsuffixed `doc.cap_chars`.
INSERT INTO setting (key, value, updated_at)
SELECT 'doc.cap_chars.manual_sop', value, updated_at
FROM setting WHERE key = 'doc.cap_chars.manual';
INSERT INTO setting (key, value, updated_at)
SELECT 'doc.cap_chars.manual_learnings', value, updated_at
FROM setting WHERE key = 'doc.cap_chars.manual';
DELETE FROM setting WHERE key = 'doc.cap_chars.manual';

-- +goose Down
-- Fold the two back into the single manual key. The LARGER of the two wins:
-- after the split the owner may have raised one half only, and an older binary
-- reads one number for both documents — picking the smaller would leave every
-- document written against the larger cap in shrink-only mode, which is the
-- degradation the cap-only-goes-up rule forbids. Picking the larger can only
-- widen a budget, never strand a document that is legal now.
--
-- MAX over the two rows rather than a comparison of two subqueries so that one
-- key being absent (only one half was ever raised) still yields the other's
-- value instead of NULL. CAST to INTEGER because the column is TEXT and a
-- lexical MAX would rank '9000' above '15000'.
INSERT INTO setting (key, value, updated_at)
SELECT 'doc.cap_chars.manual',
       CAST(MAX(CAST(value AS INTEGER)) AS TEXT),
       MAX(updated_at)
FROM setting
WHERE key IN ('doc.cap_chars.manual_sop', 'doc.cap_chars.manual_learnings')
HAVING COUNT(*) > 0;
DELETE FROM setting
WHERE key IN ('doc.cap_chars.manual_sop', 'doc.cap_chars.manual_learnings');
