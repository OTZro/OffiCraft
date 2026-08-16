import { test, expect } from "@playwright/experimental-ct-react";
import { ChatCardTimestampStory } from "./stories/ChatCardTimestampStory";

for (const viewport of [
  { name: "wide", width: 1440, layout: "wide" },
  { name: "phone", width: 390, layout: "" },
]) {
  test(`${viewport.name}: the timestamp stays with its chat card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport.width, height: 900 });
    const cmp = await mount(<ChatCardTimestampStory />);
    await page.evaluate((layout) => {
      document.documentElement.dataset.layout = layout;
    }, viewport.layout);
    const card = cmp.locator(".reply-card--chat");
    const time = cmp.getByTestId("chat-card-time");
    await expect(card).toBeVisible();
    await expect(time).toBeVisible();

    const cardBox = await card.boundingBox();
    const timeBox = await time.boundingBox();
    if (!cardBox || !timeBox) throw new Error("chat card has no layout box");

    if (viewport.layout === "wide") {
      const gap = timeBox.x - (cardBox.x + cardBox.width);
      expect(
        gap,
        `timestamp must be adjacent to the card, not the full message row (gap=${gap})`,
      ).toBeGreaterThanOrEqual(-1);
      expect(gap, "timestamp must stay attached to the card").toBeLessThanOrEqual(12);
    } else {
      expect(timeBox.x, "phone timestamp must stay inside the card width").toBeGreaterThanOrEqual(
        cardBox.x - 1,
      );
      expect(timeBox.x + timeBox.width, "phone timestamp must not overflow the card").toBeLessThanOrEqual(
        cardBox.x + cardBox.width + 1,
      );
    }

    const pageOverflow = await page.evaluate(
      () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
    );
    expect(pageOverflow, "chat layout must not widen the page").toBeLessThanOrEqual(1);
  });
}
