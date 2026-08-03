// TaskCard — 任務描述可編輯 (T-e271).
//
// Every case drives the REAL card through the REAL api seam (api/index re-exports
// the mock, which mirrors the server rule for rule) rather than a hand-written
// fake. That is deliberate: the cockpit's own mock is the calibrated stand-in,
// and a bespoke fake here would be free to be more permissive than the server —
// which is exactly how a component passes its tests and fails in production.
//
// Selectors are per-card (`within(card)`) and by data-testid. Nothing here picks
// "the last matching node on the page": a second card appearing would silently
// re-point such a probe, and the failure would surface somewhere unrelated.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "t-desc-1",
    taskNo: "T-de5c",
    title: "描述可編輯",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

/** Renders one card whose hydrated detail is `detail`, with a spy standing in
 * for the page's write. Returns the card element so every query is scoped. */
function renderCard(
  detail: TaskView,
  onUpdateDescription?: (id: string, description: string) => Promise<void>
) {
  const onHydrate = vi.fn(async () => detail);
  const utils = render(
    <I18nProvider>
      <TaskCard
        task={mkTask({ id: detail.id, status: detail.status })}
        allTasks={[detail]}
        members={[MIRA]}
        workers={workers}
        nowTs={3000}
        onTerminate={noop as never}
        onMarkDuplicate={noop as never}
        onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={onHydrate}
        onUpdateDescription={onUpdateDescription}
      />
    </I18nProvider>
  );
  return utils;
}

async function expand(utils: ReturnType<typeof renderCard>) {
  const card = await utils.findByTestId("task-card");
  fireEvent.click(card);
  await act(async () => {});
  return card;
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("T-e271 描述就地編輯", () => {
  it("opens the editor seeded with the CURRENT text and saves the correction", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(
      mkTask({ description: "原本的敘述" }),
      save
    );
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    const input = within(card).getByTestId("task-desc-input") as HTMLTextAreaElement;
    // Seeded with what is stored — an editor that opens EMPTY would turn every
    // correction into an accidental wipe of the rest of the text.
    expect(input.value).toBe("原本的敘述");

    fireEvent.change(input, { target: { value: "更正後的敘述" } });
    fireEvent.click(within(card).getByTestId("task-desc-save"));
    await act(async () => {});

    expect(save).toHaveBeenCalledWith("t-desc-1", "更正後的敘述");
    // Closed on success — the editor is not left open pretending to still hold
    // an unsaved draft.
    expect(within(card).queryByTestId("task-desc-editor")).toBeNull();
  });

  it("offers the editor on a task that has NO description yet", async () => {
    // Without this the only way to give a task a description would be to have
    // named one at create time — the exact gap this ticket exists to close, one
    // layer up in the UI.
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({ description: "" }), save);
    const card = await expand(utils);

    expect(within(card).getByTestId("task-desc-edit")).toBeTruthy();
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    fireEvent.change(within(card).getByTestId("task-desc-input"), {
      target: { value: "第一版敘述" },
    });
    fireEvent.click(within(card).getByTestId("task-desc-save"));
    await act(async () => {});
    expect(save).toHaveBeenCalledWith("t-desc-1", "第一版敘述");
  });

  // 🔴 Owner ruling ②. The priority chip one block up renders a closed task's
  // value as a frozen plain span; copying that shape here is the single most
  // likely "tidy-up" a future reader would make, and it would silently remove
  // the capability. This is the test that goes red when they do.
  it("stays editable on a CLOSED task, and says why", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(
      mkTask({
        description: "結案後才發現寫錯",
        status: "done",
        closedTs: 2500,
        progressDone: 1,
      }),
      save
    );
    const card = await expand(utils);

    // The affordance exists at all…
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    // …and the editor explains that this is a decision, not a missing guard.
    expect(within(card).getByTestId("task-desc-closed-note")).toBeTruthy();

    fireEvent.change(within(card).getByTestId("task-desc-input"), {
      target: { value: "更正:其實是另一件事" },
    });
    fireEvent.click(within(card).getByTestId("task-desc-save"));
    await act(async () => {});
    // The write really goes out — a card that showed the controls but refused
    // the save would pass an existence-only assertion.
    expect(save).toHaveBeenCalledWith("t-desc-1", "更正:其實是另一件事");
  });

  it("shows the closed-task note ONLY on a closed card", async () => {
    // The negative half: without it the assertion above is satisfied by a note
    // that is always rendered, which says nothing about the ruling.
    const utils = renderCard(mkTask({ description: "進行中" }), async () => {});
    const card = await expand(utils);
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    expect(within(card).queryByTestId("task-desc-closed-note")).toBeNull();
  });

  it("keeps the draft and stays open when the save fails", async () => {
    const save = vi.fn(async () => {
      throw new Error("boom");
    });
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const utils = renderCard(mkTask({ description: "舊的" }), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    fireEvent.change(within(card).getByTestId("task-desc-input"), {
      target: { value: "沒存成功的更正" },
    });
    fireEvent.click(within(card).getByTestId("task-desc-save"));
    await act(async () => {});

    expect(within(card).getByTestId("task-desc-error")).toBeTruthy();
    // The typed text is the ONLY copy of that correction; closing the editor on
    // failure would destroy exactly what failed to store.
    expect(
      (within(card).getByTestId("task-desc-input") as HTMLTextAreaElement).value
    ).toBe("沒存成功的更正");
  });

  it("cancel discards the draft without writing", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({ description: "原文" }), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    fireEvent.change(within(card).getByTestId("task-desc-input"), {
      target: { value: "反悔了" },
    });
    fireEvent.click(within(card).getByTestId("task-desc-cancel"));
    await act(async () => {});

    expect(save).not.toHaveBeenCalled();
    expect(within(card).queryByTestId("task-desc-editor")).toBeNull();
    // Re-opening seeds from the STORED text, not the abandoned draft.
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    expect(
      (within(card).getByTestId("task-desc-input") as HTMLTextAreaElement).value
    ).toBe("原文");
  });

  // The priority chip's outside-click shape, with the one deviation this
  // editor needs: a menu loses nothing when it closes, a textarea loses text.
  it("an outside click closes a CLEAN editor but never a dirty one", async () => {
    const utils = renderCard(mkTask({ description: "原文" }), async () => {});
    const card = await expand(utils);

    // Clean: opened and untouched → an outside click closes it, exactly like
    // the priority menu.
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    fireEvent.mouseDown(document.body);
    await act(async () => {});
    expect(within(card).queryByTestId("task-desc-editor")).toBeNull();

    // Dirty: a stray click must NOT discard a half-written correction.
    fireEvent.click(within(card).getByTestId("task-desc-edit"));
    fireEvent.change(within(card).getByTestId("task-desc-input"), {
      target: { value: "寫到一半" },
    });
    fireEvent.mouseDown(document.body);
    await act(async () => {});
    expect(within(card).getByTestId("task-desc-editor")).toBeTruthy();
    expect(
      (within(card).getByTestId("task-desc-input") as HTMLTextAreaElement).value
    ).toBe("寫到一半");
  });

  it("renders no edit affordance when the host passes no writer", async () => {
    // TaskCard is rendered in read-only contexts too; the control must not
    // appear where there is nothing behind it.
    const utils = renderCard(mkTask({ description: "唯讀" }), undefined);
    const card = await expand(utils);
    expect(within(card).queryByTestId("task-desc-edit")).toBeNull();
    expect(card.textContent).toContain("唯讀");
  });
});
