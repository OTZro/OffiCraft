-- +goose Up
-- "auto" was persisted as a machine placement but was never a warden id: the
-- dispatch path handed the literal string to IsOnline, which is always false,
-- so an "auto" placement was an unreachable destination that reconcile never
-- healed. Normalize every stored occurrence to the empty representation, the
-- only honest "no machine chosen on this row" value.
UPDATE member
   SET desired_machine_id = ''
 WHERE desired_machine_id = 'auto';

UPDATE task
   SET outsource_machine = ''
 WHERE outsource_machine = 'auto';

UPDATE task_manual
   SET assignee = json_set(assignee, '$.machine', '')
 WHERE json_extract(assignee, '$.machine') = 'auto';

-- +goose Down
-- Restoring 'auto' would reinstate an unreachable placement. The empty value is
-- the honest state in both directions, so the rollback is a no-op.
SELECT 1;
