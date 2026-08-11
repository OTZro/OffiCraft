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

  it("Enter saves the correction from the input", async () => {
    // The keydown handler's Enter branch is the fast path anyone editing a
    // one-line field will actually use, and it is the ONLY path the blank case
    // below really exercises — without this positive case, a handler that did
    // nothing at all on Enter would satisfy that one and leave the save path
    // entirely unguarded.
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    const input = within(card).getByTestId("task-title-input");
    fireEvent.change(input, { target: { value: "用 Enter 存的標題" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await act(async () => {});

    expect(save).toHaveBeenCalledWith("t-title-1", "用 Enter 存的標題");
    expect(within(card).queryByTestId("task-title-editor")).toBeNull();
  });

  it("Escape closes the editor without writing, and does not collapse the card", async () => {
    // The Escape branch of the input's onKeyDown. Deleting it leaves the editor
    // stuck open on a draft the owner has decided against — every other way out
    // (cancel, outside click) refuses to close a DIRTY editor, so Escape is the
    // only key that discards.
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    const input = within(card).getByTestId("task-title-input");
    fireEvent.change(input, { target: { value: "打到一半就反悔" } });

    // Control: an unrelated key must NOT close it, otherwise "the editor is
    // gone" would say nothing about Escape in particular.
    fireEvent.keyDown(input, { key: "a" });
    await act(async () => {});
    expect(within(card).getByTestId("task-title-editor")).toBeTruthy();

    fireEvent.keyDown(input, { key: "Escape" });
    await act(async () => {});

    expect(within(card).queryByTestId("task-title-editor")).toBeNull();
    expect(save).not.toHaveBeenCalled();
    // Escape dismisses the editor, not the card underneath it — and it
    // preventDefaults so the app-wide escape layer does not take a second bite.
    expect(card.getAttribute("aria-expanded")).toBe("true");
    // Re-opening seeds from the STORED title: the draft really was discarded.
    fireEvent.click(within(card).getByTestId("task-title-edit"));
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("原本的標題");
  });

  it("a click on the editor's own chrome never collapses the card", async () => {
    // The title editor stands INSIDE the card head, which is the whole card's
    // expand/collapse toggle. The toggle's closest() filter exempts the input
    // and the buttons, but not the editor's padding or its hint line — so
    // without the container's onClick stopPropagation, a stray click while
    // composing a correction collapses the card and takes the half-written
    // draft with it.
    const save = vi.fn(async () => {});
    const utils = renderCard(mkTask({}), save);
    const card = await expand(utils);
    expect(card.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(within(card).getByTestId("task-title-edit"));
    fireEvent.change(within(card).getByTestId("task-title-input"), {
      target: { value: "寫到一半的更正" },
    });

    // The container's own padding, then the hint line inside it — neither is an
    // interactive element, so either one reaches the card toggle if nothing
    // stops it. 🔴 Asserted one click at a time ON PURPOSE: the toggle flips,
    // so two unguarded clicks in a row land back on "expanded" and the case
    // would pass against the very defect it exists to catch.
    const editor = within(card).getByTestId("task-title-editor");
    fireEvent.click(editor);
    await act(async () => {});
    expect(card.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(editor.querySelector(".task-card__desc-hint") as Element);
    await act(async () => {});
    expect(card.getAttribute("aria-expanded")).toBe("true");

    expect(within(card).getByTestId("task-title-editor")).toBeTruthy();
    expect(
      (within(card).getByTestId("task-title-input") as HTMLInputElement).value
    ).toBe("寫到一半的更正");
    expect(save).not.toHaveBeenCalled();

    // Control: the card really is collapsible by a click that is NOT stopped —
    // otherwise the assertions above would hold on a card that cannot toggle at
    // all, and the guard they claim to prove would be untested.
    fireEvent.click(card);
    await act(async () => {});
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  // 🔴 The rule that differs from the description editor, pinned by TWO
  // assertions that prove DIFFERENT things — and the difference matters, because
  // the obvious reading of this case is wrong.
  //
  //   • `saveBtn.disabled` is the affordance: the owner is not invited to post a
  //     blank. The click on it that follows is INERT in jsdom (a disabled button
  //     dispatches nothing), so it proves nothing on its own — it is there only
  //     so the two lines read as one gesture.
  //   • The Enter keydown is the assertion with teeth: it is the ONE path in
  //     this case that actually reaches the blank guard inside doSaveTitle. Take
  //     that guard out and this is the line that goes red.
  //
  // The positive Enter case above ("Enter saves…") is what stops this one from
  // passing vacuously against a keydown handler that simply does nothing.
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
