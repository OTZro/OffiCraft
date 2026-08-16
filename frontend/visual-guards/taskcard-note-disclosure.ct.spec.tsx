// HOTSPOT — T-e5b1 的兩件事,量在真的瀏覽器裡。
//
// WHY THIS FILE IS A CT GUARD AND NOT A JSDOM TEST: both halves of this ticket
// are claims about what a HUMAN can see and click. jsdom has no layout engine
// (every box is 0×0 and nothing is ever "visible"), so a jsdom assertion can
// only say "this class/testid is absent" — which stays green if the control
// comes back under a different name. Here the assertions are boxes and hit
// tests: an affordance that returns in ANY shape occupies pixels and answers
// elementFromPoint, and that is what reddens these tests.
//
// Owner's words (2026-08-15): 「UI 不需要提供編輯標題或敘述的功能,而任務備注我希
// 望預設不顯示,多一個展開備注的選項決定要不要開該 step 的備注,不然太長了」.
//
// 🔴 What is DELIBERATELY NOT claimed: nothing here says anything about the
// SERVER. Correcting a task's title or description still exists and still works
// — through `update_task` since T-646a, and through the two original routes,
// which stay on the HTTP surface for this client. This file only measures that
// the cockpit no longer offers a way in.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteDisclosureStory } from "./stories/TaskCardNoteDisclosureStory";

async function mountExpanded(mount: any, page: any, width = 1280) {
  await page.setViewportSize({ width, height: 1200 });
  const cmp = await mount(<TaskCardNoteDisclosureStory />);
  await cmp.locator(".task-card__head").first().click();
  // Non-vacuity for everything below: the card really is expanded, so the
  // surfaces the assertions look for are the ones a reader would be seeing.
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

// The visible, non-degenerate box of a selector, or null when it has none.
async function boxOf(page: any, selector: string) {
  return page.evaluate((s: string) => {
    const el = document.querySelector(s) as HTMLElement | null;
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { w: r.width, h: r.height, x: r.x, y: r.y };
  }, selector);
}

test("no edit affordance for the title or the description occupies any pixels", async ({
  mount,
  page,
}) => {
  const cmp = await mountExpanded(mount, page);

  // (1) POSITIVE CONTROL FIRST. The enumeration below is a "found nothing"
  // assertion, and a broken selector finds nothing too. So prove the same
  // enumeration DOES see the card's other controls: the composer's 送出 button
  // is a real, visible, non-zero box in exactly the tree being searched.
  const control = await page.evaluate(() => {
    const el = document.querySelector(
      "[data-testid='task-msg-send']"
    ) as HTMLElement | null;
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  expect(control, "positive control: 送出 button must be found").not.toBeNull();
  expect(control!.w * control!.h).toBeGreaterThan(0);

  // (2) The invariant, measured as GEOMETRY, not as markup. Every interactive
  // element in the card that a user could actually reach (non-zero box, not
  // display:none / visibility:hidden) is collected with its accessible text.
  // None of them may be an edit-the-title / edit-the-description entry, in
  // either shipped language.
  const reachable = await page.evaluate(() => {
    const card = document.querySelector("[data-testid='task-card']")!;
    const out: string[] = [];
    card
      .querySelectorAll("button, a, input, textarea, select, [role='button']")
      .forEach((n) => {
        const el = n as HTMLElement;
        const r = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        if (r.width * r.height === 0) return;
        if (cs.visibility === "hidden" || cs.display === "none") return;
        out.push(
          [
            el.textContent || "",
            el.getAttribute("aria-label") || "",
            el.getAttribute("title") || "",
            el.getAttribute("data-testid") || "",
          ].join(" ")
        );
      });
    return out;
  });
  // the enumeration is non-empty — see (1); this restates it against THIS list.
  expect(reachable.length).toBeGreaterThan(0);
  for (const label of reachable) {
    expect(
      label,
      `a reachable control still offers title/description editing: ${label}`
    ).not.toMatch(/編輯標題|編輯敘述|Edit title|Edit description/);
    expect(label).not.toMatch(/task-(title|desc)-(edit|editor|input|save)/);
  }

  // (3) No editor surface has any box at all — including one that a stray
  // state could open. Zero elements, hence zero pixels.
  for (const sel of [
    "[data-testid='task-title-editor']",
    "[data-testid='task-desc-editor']",
    "[data-testid='task-title-input']",
    "[data-testid='task-desc-input']",
    "[data-testid='task-title-edit']",
    "[data-testid='task-desc-edit']",
  ]) {
    expect(await boxOf(page, sel), `${sel} must not render`).toBeNull();
  }

  // (4) The two CONTAINERS the affordances used to live in hold no reachable
  // control of ANY shape. A label-based check (2) only catches an entry that
  // announces itself; MEASURED (T-e5b1 mutant M1b): a bare `✎` icon button
  // pushed to the title row's right edge passed (2) and passed a single-point
  // hit test, because a 20px button does not sit under the 97%-of-width point.
  // So the rule is emptiness of the subtree, by geometry:
  //   · .task-card__title-line — nothing interactive at all;
  //   · .task-card__desc-block — nothing interactive EXCEPT links the
  //     description's own markdown may legitimately contain.
  const strays = await page.evaluate(() => {
    const SEL = "button, input, textarea, select, [role='button']";
    function reachable(containerSel: string, extra = "") {
      const root = document.querySelector(containerSel);
      if (!root) return ["MISSING:" + containerSel];
      const out: string[] = [];
      root.querySelectorAll(extra ? `${SEL}, ${extra}` : SEL).forEach((n) => {
        const el = n as HTMLElement;
        const r = el.getBoundingClientRect();
        if (r.width * r.height === 0) return;
        out.push(el.tagName + "." + el.className + "/" + (el.textContent || ""));
      });
      return out;
    }
    return {
      title: reachable(".task-card__title-line", "a[href]"),
      // markdown links inside the rendered description are not an edit entry
      desc: reachable(".task-card__desc-block").filter(
        (x) => !x.startsWith("A.")
      ),
    };
  });
  // A MISSING container is scored as a failure, not a pass — a renamed
  // container must not be able to retire this assertion silently.
  expect(strays.title, "title row must hold no control").toEqual([]);
  expect(strays.desc, "description block must hold no control").toEqual([]);

  // (5) …and the title/description are still THERE to read. Removing the entry
  // must not have removed the content.
  await expect(cmp.locator(".task-card__title")).toContainText(
    "任務卡標題不可就地編輯"
  );
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
});

test("a step note is collapsed until its own control is clicked, and collapses again", async ({
  mount,
  page,
}) => {
  const cmp = await mountExpanded(mount, page);

  // default: nothing of the note is on screen.
  expect(await boxOf(page, "[data-testid='step-note']")).toBeNull();

  const toggle = cmp.locator("[data-testid='step-note-toggle']");
  await expect(toggle).toHaveCount(1);
  await expect(toggle).toBeVisible();

  await toggle.click();
  const open = await boxOf(page, "[data-testid='step-note']");
  expect(open, "the note must render after 展開備註").not.toBeNull();
  expect(
    open!.h,
    "the opened note must occupy real height, not a 0px shell"
  ).toBeGreaterThan(10);
  await expect(cmp.locator("[data-testid='step-note']")).toContainText(
    "handler 已完成"
  );

  await toggle.click();
  expect(await boxOf(page, "[data-testid='step-note']")).toBeNull();
});

test("collapsed, a step WITH a note is visibly taller than one without", async ({
  mount,
  page,
}) => {
  // 🔴 DoD ④ — the owner reads this timeline to find out where a step got to.
  // With notes collapsed, "nobody wrote anything" and "someone wrote something
  // you cannot see" must not look identical. The story's two step names are
  // the same character length, so a height difference can only come from the
  // disclosure row.
  const cmp = await mountExpanded(mount, page);

  const sizes = await page.evaluate(() => {
    const pick = (id: string) => {
      const el = document.querySelector(
        `[data-step-id='${id}']`
      ) as HTMLElement | null;
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { h: r.height, w: r.width };
    };
    return { withNote: pick("s-note"), noNote: pick("s-nonote") };
  });
  expect(sizes.noNote, "fixture step without a note must render").not.toBeNull();
  expect(sizes.withNote, "fixture step with a note must render").not.toBeNull();
  expect(
    sizes.withNote!.h - sizes.noNote!.h,
    "a step carrying a note must be taller than one that carries none, even collapsed"
  ).toBeGreaterThan(8);

  // …and the difference is the CONTROL, not some incidental padding: the
  // toggle exists on exactly the step that has a note, with a real box.
  const which = await page.evaluate(() => {
    const has = (id: string) =>
      !!document
        .querySelector(`[data-step-id='${id}']`)!
        .querySelector("[data-testid='step-note-toggle']");
    const t = document.querySelector(
      "[data-testid='step-note-toggle']"
    ) as HTMLElement;
    const r = t.getBoundingClientRect();
    return { onNote: has("s-note"), onNoNote: has("s-nonote"), w: r.width, h: r.height };
  });
  expect(which.onNote).toBe(true);
  expect(which.onNoNote).toBe(false);
  expect(which.w).toBeGreaterThan(20);
  expect(which.h).toBeGreaterThan(8);
});

test("the note disclosure fits a 390px phone with no page hscroll, open or closed", async ({
  mount,
  page,
}) => {
  // Inherited from the guard this file replaces (taskcard-desc-editor-wrap):
  // the surface that was added to the card must not burst a phone viewport.
  // Both states are measured — a rule that holds only while collapsed is not
  // the rule anyone needs.
  const cmp = await mountExpanded(mount, page, 390);
  const hscroll = () =>
    page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth
    );
  expect(await hscroll(), "collapsed").toBeLessThanOrEqual(1);
  await cmp.locator("[data-testid='step-note-toggle']").click();
  await expect(cmp.locator("[data-testid='step-note']")).toBeVisible();
  expect(await hscroll(), "note open").toBeLessThanOrEqual(1);

  for (const sel of [".task-step__note-toggle", ".task-step__note-md"]) {
    const over = await page.evaluate((s: string) => {
      const el = document.querySelector(s) as HTMLElement | null;
      return el ? el.scrollWidth - el.clientWidth : -2;
    }, sel);
    expect(over, `[390px] ${sel} missing (never rendered)`).not.toBe(-2);
    expect(over, `[390px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }
});
