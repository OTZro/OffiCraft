-- +goose Up
-- T-1f39 split the task-manual history into two independent series
-- (task_manual_sop, task_manual_learnings). The legacy four-field bundle kind
-- 'task_manual' has had no writer since that split, and the owner ruled on
-- 2026-07-31 that the stranded rows are to be DELETED rather than migrated
-- ("不需要管舊歷史，舊歷史資料直接刪除就好，不要留技術債"), with no export
-- taken, after being told the SOP and learnings history goes with them.
-- Fail-closed and surgical: this predicate and nothing broader. Every other
-- document_kind — lessons, global_context, role_definition, and the two
-- replacement manual kinds — is untouched.
DELETE FROM document_history
 WHERE document_kind = 'task_manual';

-- +goose Down
-- NOT REVERSIBLE. The Up deleted rows and kept no copy anywhere; rolling this
-- migration back does not and cannot bring them back. That is the intended,
-- owner-approved outcome (T-1f39, 2026-07-31), not an oversight — writing a
-- Down that reinstated an empty or synthesized bundle would be a lie about
-- what is recoverable. The rollback is therefore an explicit no-op.
SELECT 1;
