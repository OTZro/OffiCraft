// 步驟備註 (T-cc3e) — the cockpit face of the step working note.
//
// The owner picked a step-level note over folding progress into the task
// description for ONE stated reason: he wants the granularity of "第 4 步做到
// 哪" on the card. So "the server stores it" is not the deliverable — being
// able to READ it on the task card is. These tests assert the rendered text,
// per step, on the real card; they are what stops the field from shipping
// invisible.
//
// The note is agent-authored free text (the handover SOP asks for "做到哪、下
// 一步接什麼", routinely with markdown), so it takes the same treatment as the
// DoD and the waiting reason: rendered through the shared, XSS-safe `Markdown`
// component, with the i18n label kept OUTSIDE that container.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}

function mkTask(steps: TaskStepView[]): TaskView {
  return {
    id: "T-cc3e", taskNo: "T-cc3e", title: "步驟備註任務", typeKey: "",
    description: "", status: "in_progress", priority: "mid",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: steps.length, steps,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

// The step timeline only renders while the card is expanded.
async function renderExpanded(steps: TaskStepView[]) {
  const task = mkTask(steps);
  const utils = render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={workers} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={vi.fn(async () => task)}
      />
    </I18nProvider>
  );
  fireEvent.click(await utils.findByTestId("task-card"));
  return utils;
}

describe("步驟備註 renders on the task card (T-cc3e)", () => {
  it("shows the note text of the step it belongs to", async () => {
    const { findAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "spec 三份重生一致" }),
      mkStep({ name: "第二步", status: "in_progress", note: "handler 寫完，測試還差負面案例" }),
    ]);
    const notes = await findAllByTestId("step-note");
    expect(notes).toHaveLength(2);
    expect(notes[0].textContent).toContain("spec 三份重生一致");
    expect(notes[1].textContent).toContain("handler 寫完，測試還差負面案例");
    // Per-step, not smeared across the timeline: the second step's note must
    // not appear inside the first step's block.
    expect(notes[0].textContent).not.toContain("handler 寫完");
  });

  it("renders markdown as elements, and keeps the label out of the parser", async () => {
    const { findByTestId } = await renderExpanded([
      mkStep({ note: "卡在 **conformance**，下一步 `bin/ci.sh`" }),
    ]);
    const block = await findByTestId("step-note");

    // positive control: the label is present in this block, so the negative
    // assertions below cannot pass by selecting nothing.
    const label = block.querySelector(".task-step__note-label");
    expect(label?.textContent).toBeTruthy();

    const md = block.querySelector(".task-step__note-md")!;
    expect(md.querySelector("strong")?.textContent).toBe("conformance");
    expect(md.querySelector("code")?.textContent).toBe("bin/ci.sh");
    expect(md.textContent).not.toContain("**conformance**");
    // The i18n label must live OUTSIDE the markdown container — feeding label
    // and note to the parser together would parse the label too.
    expect(md.textContent).not.toContain(label!.textContent!);
  });

  it("renders nothing at all when a step has no note — no empty shell", async () => {
    const { queryAllByTestId, findAllByTestId } = await renderExpanded([
      mkStep({ name: "沒備註", status: "pending" }),
      mkStep({ name: "有備註", status: "pending", note: "只有這一步有" }),
    ]);
    // positive control first: the one note that exists did render.
    expect((await findAllByTestId("step-note"))).toHaveLength(1);
    expect(queryAllByTestId("step-note")[0].textContent).toContain("只有這一步有");
  });

  it("shows the note whatever the step's status is — the point of the field", async () => {
    // waiting_reason is bound to waiting_external; the note is bound to
    // nothing. If a status condition ever creeps into the render, this reddens.
    const statuses = ["pending", "in_progress", "waiting_external", "done"];
    const { findAllByTestId } = await renderExpanded(
      statuses.map((status) =>
        mkStep({ status, note: `note-in-${status}`, waitingReason: "" }),
      ),
    );
    const notes = await findAllByTestId("step-note");
    expect(notes).toHaveLength(statuses.length);
    statuses.forEach((status, i) => {
      expect(notes[i].textContent).toContain(`note-in-${status}`);
    });
  });
});
