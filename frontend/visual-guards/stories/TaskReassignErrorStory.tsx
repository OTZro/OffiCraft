// Story (T-b9f6) — the 轉派 dialog showing the SERVER's refusal reason.
//
// The reason line replaced a 10-character fixed string (「轉派失敗，請稍後重試」)
// with whatever the server said, and the longest of those sentences is ~110
// characters including 發包. That is a layout change in everything but name, and
// jsdom cannot see it — hence a real-browser guard. The story hands the dialog
// the LONGEST refusal the reassign handler can produce, so the CT spec measures
// the worst case rather than a comfortable one.
import { I18nProvider } from "../../src/i18n";
import { TaskReassignDialog } from "../../src/components/TaskReassignDialog";
import { ApiError } from "../../src/api/errors";
import type { TaskView } from "../../src/api/adapter";
import { LONGEST_REFUSAL } from "./reassignRefusals";


const TASK: TaskView = {
  id: "task-1",
  taskNo: "T-1001",
  title: "任務",
  typeKey: "",
  description: "",
  status: "in_progress",
  priority: "mid",
  executorKind: "outsource",
  executorId: "",
  creatorId: "",
  dedupeKey: "",
  deps: [],
  waitingReason: "",
  duplicateOf: "",
  createdTs: 1_780_000_000,
  updatedTs: 1_780_000_100,
  closedTs: null,
  progressDone: 0,
  progressTotal: 0,
  steps: [],
};

export function TaskReassignErrorStory() {
  return (
    <I18nProvider>
      <TaskReassignDialog
        task={TASK}
        members={[]}
        onReassign={() =>
          Promise.reject(
            new ApiError(
              "http 403 for POST /api/tasks/task-1/reassign",
              403,
              "forbidden",
              LONGEST_REFUSAL
            )
          )
        }
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
