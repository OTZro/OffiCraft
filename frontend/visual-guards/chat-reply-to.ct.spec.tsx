// GUARD (T-4e95) — 「回覆這則」的版面契約。
//
// jsdom 已經證過行為（ChatArea.reply-to.test.tsx：入口在每一列、橫幅顯示對象、
// x 不清字、送出帶對象、引用列點得回去）。這裡補的是 jsdom **量不到**的三件，
// 每一件都對應一個真的會壞掉的方式：
//
//  ① 回覆入口是 hover 才顯形的，但**永遠占著版面**。用 display:none 做隱藏會讓
//     氣泡在滑過去的當下橫向跳一格 —— 那是使用者感覺得到、jsdom 完全看不到的。
//  ② 引用列與「正在回覆」橫幅都必須裁成一行。它們攜帶的是別人訊息的原文，長度
//     不受這一則控制；一旦允許折行，一句長訊息就會把版面撐開，或把輸入框往下推。
//  ③ 引用列必須留在氣泡那一欄裡，不得溢出訊息串的可視寬度（窄視窗尤其）。
//
// 兩個寬度都量：手機寬與桌面寬在這個元件上是不同的失敗面。
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatReplyToStory } from "./stories/ChatReplyToStory";

for (const width of [390, 1280]) {
  test(`width ${width}: the reply entry is hover-revealed but never re-flows the row`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const entry = cmp.getByTestId("reply-entry-incoming");
    // The bubble's TEXT is what a reflow would move now that the controls live
    // inside the bubble: measuring the bubble itself would miss a slot that
    // grew and pushed the words sideways.
    const bubble = cmp.getByTestId("row-incoming").locator(".chat__msg-text");

    // Hidden by OPACITY, not by display/visibility: it must still occupy space.
    await expect(entry).toHaveCSS("opacity", "0");
    const box = await entry.boundingBox();
    expect(box, "an opacity-hidden control still has a box").not.toBeNull();
    expect(box!.width).toBeGreaterThan(0);
    expect(box!.height).toBeGreaterThan(0);

    // The decisive measurement: hovering reveals it WITHOUT moving the bubble.
    const before = await bubble.boundingBox();
    await cmp.getByTestId("row-incoming").hover();
    await expect(entry).toHaveCSS("opacity", "1");
    const after = await bubble.boundingBox();
    expect(Math.abs(after!.x - before!.x)).toBeLessThanOrEqual(0.5);
    expect(Math.abs(after!.width - before!.width)).toBeLessThanOrEqual(0.5);
  });

  test(`width ${width}: the quote row is clipped to one line and stays inside the thread`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const quote = cmp.getByTestId("quote-row");
    const bubble = cmp.getByTestId("row-mine").locator(".chat__msg-bubble");
    const quoteBox = (await quote.boundingBox())!;
    const bubbleBox = (await bubble.boundingBox())!;

    // ONE LINE. The quoted text is long enough to wrap to three or four lines
    // if anything let it, so a tall quote is the failure.
    expect(quoteBox.height).toBeLessThan(30);

    // INSIDE the bubble, not floating beside it (owner 2026-08-20). Measured
    // rather than asserted on the DOM: a quote nested in the markup but pulled
    // out visually would still read as a separate strip, which is the actual
    // complaint.
    expect(quoteBox.x).toBeGreaterThanOrEqual(bubbleBox.x - 0.5);
    expect(quoteBox.x + quoteBox.width).toBeLessThanOrEqual(
      bubbleBox.x + bubbleBox.width + 0.5,
    );
    expect(quoteBox.y).toBeGreaterThanOrEqual(bubbleBox.y - 0.5);

    // The jump control keeps its whole width — a cut 跳到原訊息 helps nobody;
    // it is the quoted TEXT (and, when it has to, the sender name) that gives
    // way. The story deliberately uses an UNRESOLVED sender — a raw member id —
    // because that is the long name this row really meets.
    const jump = (await cmp.getByTestId("quote-jump").boundingBox())!;
    expect(jump.x + jump.width).toBeLessThanOrEqual(
      bubbleBox.x + bubbleBox.width + 0.5,
    );
    expect(jump.width).toBeGreaterThan(40);

    // 🔴 AND IT MUST NOT REACH THE CORNER CONTROLS. Measured, not inferred: a
    // row whose min-content width exceeded the bubble pushed the jump ON TOP of
    // the reply button — the affordance covering the affordance. Nothing about
    // the DOM says that; only geometry does.
    const acts = (await cmp
      .getByTestId("row-mine")
      .locator(".chat__msg-actions")
      .boundingBox())!;
    expect(jump.x + jump.width).toBeLessThanOrEqual(acts.x + 0.5);
  });

  test(`width ${width}: a short sender name in the quote row is never ellipsised`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const who = cmp.getByTestId("quote-who-short");
    const body = cmp.getByTestId("quote-body-short");

    // POSITIVE CONTROL first: the row really is tight enough to force a cut.
    // Without this the assertion below would also pass on a row with acres of
    // spare width — i.e. it would guard nothing.
    const bodyCut = await body.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(bodyCut).toBe(true);

    // 🔴 AND THE NAME SURVIVES IT. Two shrinkable items shrink in proportion,
    // so the quoted text taking a cut used to drag the name down with it —
    // 「Mira」 rendered as 「M…」 with room to spare. The name answers WHO;
    // the quoted text is the part that may be trimmed.
    const whoCut = await who.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(whoCut).toBe(false);
    expect((await who.textContent())?.trim()).toBe("Mira");
  });

  test(`width ${width}: a name is never cut while the excerpt still fits`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const who = cmp.getByTestId("quote-who-tight");
    const body = cmp.getByTestId("quote-body-tight");

    // POSITIVE CONTROL, the other way round from the test above: this row has
    // room to spare, so NOTHING on it should be cut. If the excerpt were being
    // trimmed the row would be genuinely tight and the name's fate would say
    // nothing about the rule.
    const bodyCut = await body.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(bodyCut).toBe(false);

    // 🔴 SO THE NAME MUST BE WHOLE. A `max-width` in PERCENT does not survive
    // shrink-to-fit: the bubble is sized from intrinsic width, where a
    // percentage cap is ignored, and the cap then clips the name inside a
    // bubble that was measured to hold it. The result was a name losing
    // characters at 1600px next to a one-word excerpt — a shortage the layout
    // invented. Whatever keeps long names inside the bubble, it must not fire
    // when the row is not full.
    const whoCut = await who.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 0.5,
    );
    expect(whoCut).toBe(false);
    expect((await who.textContent())?.trim()).toBe("ow-8808ccf51794");
  });

  test(`width ${width}: the corner buttons take their ink from the bubble they sit on`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    // owner 2026-08-20 (rc-8056a06aa2b8): the entry used to be a chip in
    // --color-card, which is the same surface as an incoming bubble and a
    // foreign grey box on your own — that one is painted accent green. He asked
    // why the two buttons looked like different colours; they were not
    // different from each other, they were different from what they sat on.
    const incoming = cmp.getByTestId("reply-entry-incoming");
    const mine = cmp.getByTestId("reply-entry-mine");

    // 🔴 THE RULE, stated as something that can fail: the same button on two
    // differently-coloured bubbles must not come out the same colour. A
    // hardcoded muted ink passes any "is it readable" check and fails this one.
    const inkIncoming = await incoming.evaluate((e) => getComputedStyle(e).color);
    const inkMine = await mine.evaluate((e) => getComputedStyle(e).color);
    expect(inkMine).not.toBe(inkIncoming);

    // And no box of its own: the chip is what made it read as a foreign object.
    for (const btn of [incoming, mine]) {
      const box = await btn.evaluate((e) => {
        const c = getComputedStyle(e);
        return { bg: c.backgroundColor, bw: c.borderTopWidth };
      });
      expect(box.bg).toBe("rgba(0, 0, 0, 0)");
      expect(box.bw).toBe("0px");
    }
  });

  test(`width ${width}: the 正在回覆 banner stays one line and its x stays reachable`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatReplyToStory />);

    const banner = cmp.getByTestId("chat-reply-banner");
    const bannerBox = (await banner.boundingBox())!;
    // One line: a wrapped banner grows the composer and shoves the input down
    // as the owner aims at different messages.
    expect(bannerBox.height).toBeLessThan(36);
    expect(bannerBox.x + bannerBox.width).toBeLessThanOrEqual(width + 1);

    // The x must remain a real, hittable control at BOTH widths — it is the
    // only way back to the ordinary send state.
    const x = cmp.getByTestId("reply-banner-x");
    const xBox = (await x.boundingBox())!;
    expect(xBox.width).toBeGreaterThanOrEqual(20);
    expect(xBox.height).toBeGreaterThanOrEqual(20);
    expect(xBox.x + xBox.width).toBeLessThanOrEqual(
      bannerBox.x + bannerBox.width + 0.5,
    );
  });
}

// Native keyboard semantics, one width. jsdom proved the click handler; this
// proves both controls really are <button> elements — a <div onClick> mutant
// takes the reply entry and the x out of reach for anyone not using a mouse.
test("narrow 390: the reply entry and the banner x are focusable native buttons", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const cmp = await mount(<ChatReplyToStory />);

  for (const id of ["reply-entry-incoming", "reply-banner-x", "quote-jump"]) {
    const control = cmp.getByTestId(id);
    await expect(control).toHaveJSProperty("tagName", "BUTTON");
    await control.focus();
    await expect(control).toBeFocused();
  }
});
