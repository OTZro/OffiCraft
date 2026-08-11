// TaskCard — 任務標題可編輯 (T-2ebe).
//
// Shaped after TaskCard.desc-edit.test.tsx, and for the same reasons: every case
// drives the REAL card and scopes its queries per-card. What is NOT copied is
// the blank rule — a blank description clears the field, a blank title is a 400
// — so the cases that pin it are the point of this file.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import { ApiError } from "../api/errors";
import type { Member } from "../types";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "t-title-1",
    taskNo: "T-2ebe",
    title: "原本的標題",
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

function renderCard(
  detail: TaskView,
  onUpdateTitle?: (id: string, title: string) => Promise<void>
) {
  const onHydrate = vi.fn(async () => detail);
  return render(
    <I18nProvider>
      <TaskCard
        task={detail}
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
        onUpdateTitle={onUpdateTitle}
      />
    </I18nProvider>
  );
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

describe("T-2ebe 標題就地編輯", () => {
  it("opens the editor seeded with the CURRENT title and saves the correction", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    const input = within(card).getByTestId(
      "task-title-input"
    ) as HTMLInputElement;
    // Seeded with what is stored — an editor that opened EMPTY would turn every
    // correction into a retype from scratch.
    expect(input.value).toBe("原本的標題");

    fireEvent.change(input, { target: { value: "更正後的標題" } });
    fireEvent.click(within(card).getByTestId("task-title-save"));
    await act(async () => {});

    expect(save).toHaveBeenCalledWith("t-title-1", "更正後的標題");
    expect(within(card).queryByTestId("task-title-editor")).toBeNull();
  });

  // 🔴 The rule that differs from the description editor. Two halves, because
  // either alone is satisfiable by the wrong implementation: the control must be
  // disabled (so the owner is not invited to post a blank), AND the click must
  // not write (so a keyboard/Enter path cannot slip one through).
  it("refuses a blank title without sending it", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "   " },
    });
    const saveBtn = within(card).getByTestId(
      "task-title-save"
    ) as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);

    fireEvent.click(saveBtn);
    fireEvent.keyDown(within(card).getByTestId("task-title-input"), {
      key: "Enter",
    });
    await act(async () => {});
    expect(save).not.toHaveBeenCalled();
    // Said out loud, in the same slot a server refusal would land in — a
    // silently inert button reads as a broken screen.
    expect(within(card).getByTestId("task-title-error")).toBeTruthy();
    // And nothing was destroyed: the editor still stands, still holding the
    // draft.
    expect(within(card).getByTestId("task-title-editor")).toBeTruthy();
  });

  it("surfaces the server's 400 when the write is refused", async () => {
    // The FE guard is not the only door — the server owns the rule, and a
    // rejection from it must read the same as the local refusal rather than the
    // generic "try again", which would be false for a blank.
    const save = vi.fn(async () => {
      throw new ApiError(
        "http 400 for POST /api/tasks/t-title-1/title",
        400,
        "bad_request",
        "title must not be blank"
      );
    });
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "伺服器會拒絕的標題" },
    });
    fireEvent.click(within(card).getByTestId("task-title-save"));
    await act(async () => {});

    expect(save).toHaveBeenCalled();
    expect(within(card).getByTestId("task-title-error").textContent).toContain(
      "標題不能空白"
    );
    // Stays open with the draft intact — the typed text is its only copy.
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("伺服器會拒絕的標題");
  });

  it("keeps the draft and stays open when the save fails for any other reason", async () => {
    const save = vi.fn(async () => {
      throw new Error("boom");
    });
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "沒存成功的標題" },
    });
    fireEvent.click(within(card).getByTestId("task-title-save"));
    await act(async () => {});

    expect(within(card).getByTestId("task-title-error").textContent).toContain(
      "儲存失敗"
    );
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("沒存成功的標題");
  });

  // 🔴 Owner ruling inherited from T-e271 ②: a closed ticket's text is exactly
  // what tends to be found wrong AFTER it closed.
  it("stays editable on a CLOSED task, and says why", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(
      mkTask({ status: "done", closedTs: 2500, progressDone: 1 }),
      save
    );
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    expect(within(card).getByTestId("task-title-closed-note")).toBeTruthy();

    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "更正:其實是另一件事" },
    });
    fireEvent.click(within(card).getByTestId("task-title-save"));
    await act(async () => {});
    // The write really goes out — a card that showed the controls but refused
    // the save would pass an existence-only assertion.
    expect(save).toHaveBeenCalledWith("t-title-1", "更正:其實是另一件事");
  });

  it("shows the closed-task note ONLY on a closed card", async () => {
    const utils = renderCard(mkTask({}), async () => {});
    const card = await expand(utils);
    fireEvent.click(within(card).getByTestId("task-title-edit"));
    expect(within(card).queryByTestId("task-title-closed-note")).toBeNull();
  });

  it("cancel discards the draft without writing", async () => {
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "反悔了" },
    });
    fireEvent.click(within(card).getByTestId("task-title-cancel"));
    await act(async () => {});

    expect(save).not.toHaveBeenCalled();
    expect(within(card).queryByTestId("task-title-editor")).toBeNull();
    // Re-opening seeds from the STORED title, not the abandoned draft.
    fireEvent.click(within(card).getByTestId("task-title-edit"));
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("原本的標題");
  });

  it("an outside click closes a CLEAN editor but never a dirty one", async () => {
    const utils = renderCard(mkTask({}), async () => {});
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.mouseDown(document.body);
    await act(async () => {});
    expect(within(card).queryByTestId("task-title-editor")).toBeNull();

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "寫到一半" },
    });
    fireEvent.mouseDown(document.body);
    await act(async () => {});
    expect(within(card).getByTestId("task-title-editor")).toBeTruthy();
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("寫到一半");
  });

  it("renders no edit affordance when the host passes no writer", async () => {
    // TaskCard is rendered in read-only contexts too; the control must not
    // appear where there is nothing behind it.
    const utils = renderCard(mkTask({}), undefined);
    const card = await expand(utils);
    expect(within(card).queryByTestId("task-title-edit")).toBeNull();
    expect(card.textContent).toContain("原本的標題");
  });

  it("keeps the collapsed card free of the affordance", async () => {
    // The collapsed card is a scanning row; an edit button on every one of them
    // is noise. Same rule the description block already follows.
    const utils = renderCard(mkTask({}), async () => {});
    const card = await utils.findByTestId("task-card");
    expect(within(card).queryByTestId("task-title-edit")).toBeNull();
    expect(card.textContent).toContain("原本的標題");
  });
});
