// CT story (T-e271): an expanded TaskCard with the description EDITOR open.
//
// Why a story of its own rather than another mode on TaskCardLongTokenStory:
// that guard measures three READ surfaces which all render through `.doc-md`
// and share one inherited overflow rule. The editor is a different layout
// problem — a textarea plus a flex ROW of controls (儲存 / 取消 / 版本紀錄) —
// and folding it in would blur which surface a red is naming.
//
// The description carries the same unbreakable token as the read-mode story,
// because the editor is seeded from the STORED text: whatever a card can
// display, it can also be asked to edit.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, MIRA, NOOP, WORKERS } from "./taskFixtures";

const TOKEN =
  "twin(desired_state/desired_machine_id/refocus_since/bank_balance/created_ts/activated_ts)";

const TASK = mkTask({
  id: "t-e271",
  taskNo: "T-e271",
  title: "任務描述可編輯",
  status: "in_progress",
  description: `更正:這張票要的是 member ${TOKEN} 的描述可編輯,不是步驟備註。`,
  progressDone: 0,
  progressTotal: 1,
});

export function TaskCardDescEditorStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={TASK}
        allTasks={[TASK]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => TASK) as never}
        // The presence of this prop is what makes the 編輯敘述 control render.
        onUpdateDescription={NOOP as never}
      />
    </I18nProvider>
  );
}
