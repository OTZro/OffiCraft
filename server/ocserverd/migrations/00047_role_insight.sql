-- +goose Up
-- T-3809 角色誌拆三段：Duty / Insight / Learning. One additive table, NO data move.
--
-- WHY A THIRD BLOCK AT ALL (owner, 2026-07-28, verbatim): 「其他人不需要知道
-- Insight，但是 Insight 跟 Learning 也不應該混在一起，後者應該是基於環境學習到
-- 的一些 Q&A」. The three blocks are:
--   * Duty    — what this role is responsible for   → role_def.definition_md (UNTOUCHED)
--   * Insight — the judgement calls and trade-offs  → THIS TABLE (new, starts empty)
--   * Learning— environment Q&A (versions, paths…)  → lessons.text (UNTOUCHED)
--
-- 🔴 ZERO AUTOMATIC SPLIT (owner ruling, 2026-08-01, rc-87e850241ef4 option ②).
-- This migration deliberately contains no UPDATE and no INSERT…SELECT: today's
-- content STAYS where it is and each role moves its own Insight over by hand,
-- if and when it judges that worth doing. The owner accepted the named cost:
-- on the day this ships every role's Insight is EMPTY, so the pain this ticket
-- was opened for (one cap shared by things whose deletion costs differ tenfold)
-- is not one character better on day one. Splitting text by machine would have
-- been the alternative, and a wrong split is worse than a slow one — a judgement
-- call filed as an environment fact gets deleted at the next cap squeeze, and
-- the agent that deletes it will not know what it cost.
--
-- SINGLE KEY. Insight's document_history key is the BARE role_key, so deleting
-- a role's insight history MUST use exact equality (document_key = ?) the way
-- DeleteRoleDef does — a single-key document has no terminator, and a prefix
-- match would take r-abcdef's history out with r-abc's.
--
-- 🔴 CORRECTION, T-2 (2026-08-27). WHEN THIS WAS WRITTEN the paragraph above
-- read as a CONTRAST: "unlike lessons", which was then keyed
-- (role_key, task_type) and whose cascade matched history keys by the
-- "<role>::" prefix, the "::" terminator making r-abc:: safe against
-- r-abcdef::general. T-2 removed the lessons task_type axis
-- (00061 + 00062_drop_lessons_task_type.sql): lessons is keyed role_key ALONE,
-- its history key is the bare role_key too, and DeleteLessonsForRole now
-- matches by exact equality like everything else — see its own comment in
-- dal.go, which says so in as many words. So the contrast is GONE and the rule
-- stated here is simply the house shape, not insight's exception to one.
-- The SQL below is unchanged and was never affected; only the reason was.
-- (Editing an applied migration's COMMENT is safe here and that was measured,
-- not assumed — the same check 00061 records: goose stores only
-- (id, version_id, is_applied, tstamp) and carries no checksum on any
-- migration path, so there is nothing for a comment to invalidate.)
--
-- NO SEED FILE, deliberately. lessons folds overlay ⊕ a shared file seed, so a
-- role that never wrote still reads non-empty. If insight had a seed, text==""
-- would never be true and 「這個角色還沒搬」 would stop being answerable — and
-- that question is the only observable this ticket delivers.
CREATE TABLE role_insight (
  role_key   TEXT PRIMARY KEY,
  text       TEXT NOT NULL DEFAULT '',
  tombstoned INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
-- Drop the table. Nothing to restore: Up moved no data out of anywhere, so an
-- older binary sees precisely the world it left behind — Duty and Learning were
-- never touched. Insight text written while the table existed is lost with it;
-- there is nowhere older to put it, because nowhere older ever held it.
DROP TABLE role_insight;
