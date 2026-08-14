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
//
// T-e5b1 (owner 2026-08-15) put the note behind a per-step disclosure: it is
// COLLAPSED by default and a 展開備註 button opens it. That does NOT weaken the
// paragraph above — it makes one more thing load-bearing. While every note is
// closed, the disclosure BUTTON is the only signal separating a step someone
// wrote a note on from a step nobody did, so its presence/absence is asserted
// here as a first-class contract, not as decoration.

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

// Open every step's note. The tests that assert note CONTENT are about what
// the note renders, not about the disclosure, so they go through this rather
// than each re-deriving the click.
async function openAllNotes(utils: { findAllByTestId: (id: string) => Promise<HTMLElement[]> }) {
  const toggles = await utils.findAllByTestId("step-note-toggle");
  toggles.forEach((b) => fireEvent.click(b));
  return toggles;
}

describe("步驟備註 renders on the task card (T-cc3e)", () => {
  it("shows the note text of the step it belongs to", async () => {
    const { findAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "spec 三份重生一致" }),
      mkStep({ name: "第二步", status: "in_progress", note: "handler 寫完，測試還差負面案例" }),
    ]);
    await openAllNotes({ findAllByTestId });
    const notes = await findAllByTestId("step-note");
    expect(notes).toHaveLength(2);
    expect(notes[0].textContent).toContain("spec 三份重生一致");
    expect(notes[1].textContent).toContain("handler 寫完，測試還差負面案例");
    // Per-step, not smeared across the timeline: the second step's note must
    // not appear inside the first step's block.
    expect(notes[0].textContent).not.toContain("handler 寫完");
  });

  it("renders markdown as elements, and keeps the label out of the parser", async () => {
    const utils = await renderExpanded([
      mkStep({ note: "卡在 **conformance**，下一步 `bin/ci.sh`" }),
    ]);
    const { findByTestId } = utils;
    await openAllNotes(utils);
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
    const utils = await renderExpanded([
      mkStep({ name: "沒備註", status: "pending" }),
      mkStep({ name: "有備註", status: "pending", note: "只有這一步有" }),
    ]);
    const { queryAllByTestId, findAllByTestId } = utils;
    await openAllNotes(utils);
    // positive control first: the one note that exists did render.
    expect((await findAllByTestId("step-note"))).toHaveLength(1);
    expect(queryAllByTestId("step-note")[0].textContent).toContain("只有這一步有");
  });

  it("shows the note whatever the step's status is — the point of the field", async () => {
    // waiting_reason is bound to waiting_external; the note is bound to
    // nothing. If a status condition ever creeps into the render, this reddens.
    const statuses = ["pending", "in_progress", "waiting_external", "done"];
    const utils = await renderExpanded(
      statuses.map((status) =>
        mkStep({ status, note: `note-in-${status}`, waitingReason: "" }),
      ),
    );
    const { findAllByTestId } = utils;
    await openAllNotes(utils);
    const notes = await findAllByTestId("step-note");
    expect(notes).toHaveLength(statuses.length);
    statuses.forEach((status, i) => {
      expect(notes[i].textContent).toContain(`note-in-${status}`);
    });
  });

  it("hides every note until its own 展開備註 is clicked, and re-hides on a second click", async () => {
    const { queryAllByTestId, findAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "第一步的備註" }),
      mkStep({ name: "第二步", status: "in_progress", note: "第二步的備註" }),
    ]);

    // ① default: both toggles are on screen, no note body is.
    const toggles = await findAllByTestId("step-note-toggle");
    expect(toggles).toHaveLength(2);
    expect(queryAllByTestId("step-note")).toHaveLength(0);

    // ② opening ONE step opens only that step's note — the disclosure is per
    // step, which is the whole point of the owner's wording ("該 step 的備注").
    fireEvent.click(toggles[0]);
    let notes = queryAllByTestId("step-note");
    expect(notes).toHaveLength(1);
    expect(notes[0].textContent).toContain("第一步的備註");
    expect(notes[0].textContent).not.toContain("第二步的備註");
    expect(toggles[0].getAttribute("aria-expanded")).toBe("true");
    expect(toggles[1].getAttribute("aria-expanded")).toBe("false");

    // ③ the second step opens independently, leaving the first open.
    fireEvent.click(toggles[1]);
    expect(queryAllByTestId("step-note")).toHaveLength(2);

    // ④ clicking again collapses it back.
    fireEvent.click(toggles[0]);
    notes = queryAllByTestId("step-note");
    expect(notes).toHaveLength(1);
    expect(notes[0].textContent).toContain("第二步的備註");
  });

  it("gives a step WITH a note a control a step without one does not have", async () => {
    // 🔴 The owner reads this timeline to find out where a step got to. Once
    // notes are collapsed, "nobody wrote anything" and "someone wrote
    // something you cannot see" must not look the same. The toggle button is
    // that difference, and it is asserted per step (not just counted) so a
    // future change that renders the button on EVERY step reddens here.
    // The step NAMES deliberately avoid the word 備註 — the assertion below is
    // about what the disclosure control contributes, and a fixture name
    // carrying the same word would make it pass (or fail) for the wrong reason.
    const { findAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "pending" }),
      mkStep({ name: "第二步", status: "pending", note: "只有這一步有" }),
    ]);
    const steps = await findAllByTestId("task-step");
    expect(steps).toHaveLength(2);
    expect(steps[0].querySelectorAll("[data-testid='step-note-toggle']")).toHaveLength(0);
    expect(steps[1].querySelectorAll("[data-testid='step-note-toggle']")).toHaveLength(1);
    // and the control says the word 備註, so what it opens is not a guess.
    expect(steps[1].textContent).toContain("備註");
    expect(steps[0].textContent).not.toContain("備註");
  });

  it("opening a note does not collapse the card it lives in", async () => {
    // The whole card is a toggle surface; a click that lands on a <button> is
    // exempted by the card's closest() filter. If that exemption ever stops
    // covering this button, the note would open and the card would shut in the
    // same click — the note would be unreadable.
    const { findAllByTestId, findByTestId, queryAllByTestId } = await renderExpanded([
      mkStep({ name: "第一步", status: "done", note: "第一步的備註" }),
    ]);
    fireEvent.click((await findAllByTestId("step-note-toggle"))[0]);
    expect(queryAllByTestId("step-note")).toHaveLength(1);
    expect((await findByTestId("task-card")).getAttribute("aria-expanded")).toBe("true");
  });
});
