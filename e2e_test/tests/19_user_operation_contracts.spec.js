// Focused browser checks for the user-operation contract list.
//
// 13_reply_cards.spec.js owns the larger historical loop and remains behaviorally
// unchanged; this file adds the missing single-card draft-preservation checks on
// both surfaces so the two sentences in docs/guide/quickstart.md:88 do not share
// one vague assertion.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

function options(texts, aiPickAt) {
  return texts.map((text, i) => ({ text, ai_pick: i === aiPickAt }));
}

function repliesTab(page) {
  return page.locator('.nav-tab', { hasText: '請示' });
}

function chip(scope, idx) {
  return scope.locator(
    `[data-testid="reply-option"][data-option-idx="${idx}"]`,
  );
}

async function createReplyCardAs(request, agentToken, card) {
  const res = await request.post(`${BASE}/api/reply-cards`, {
    headers: authHeaders(agentToken),
    data: { linked_task: null, ...card },
  });
  expect(res.status(), 'creating a reply card must succeed').toBe(200);
  return res.json();
}

async function readReplyCardAs(request, token, cardId) {
  const res = await request.get(`${BASE}/api/reply-cards/${cardId}`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'reading the answered reply card must succeed').toBe(200);
  return res.json();
}

test.describe('user-operation contract · single card draft preservation', () => {
  test('a page one-tap answer carries the text already in its composer', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('UOC page member'));
    const memberToken = await mintMemberToken(request, token, member.id, 1);
    const summary = uniqueName('單選頁面草稿');
    const draft = uniqueName('這段字不能丟');
    const card = await createReplyCardAs(request, memberToken, {
      kind: 'decision',
      summary,
      options: options(['保留', '送出'], 1),
      select_mode: 'single',
    });

    await bootAuthedSpa(page, token);
    await repliesTab(page).click();
    const waiting = page.getByTestId('waiting-card').filter({ hasText: summary });
    await expect(waiting).toBeVisible();
    await waiting.locator('.chat__input').fill(draft);
    await chip(waiting, 1).click();

    await expect(waiting, 'one tap must close the page card').toHaveCount(0);
    await page.getByTestId('answered-toggle').click();
    const answered = page
      .getByTestId('answered-card')
      .filter({ hasText: summary });
    await expect(answered).toBeVisible();
    const finalAnswer = answered.getByTestId('final-answer');
    // UOC_ASSERT id=UOC-RC-SINGLE-DRAFT screen=replies-page name=single_option_keeps_draft_on_replies_page
    await expect(
      finalAnswer,
      'the page one-tap answer must keep the draft text',
    ).toContainText(draft);

    const readback = await readReplyCardAs(request, memberToken, card.id);
    expect(readback.answer.option_idxs).toEqual([1]);
    expect(readback.answer.text).toBe(draft);
  });

  test('a chat one-tap answer carries the text already in its composer', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('UOC chat member'));
    const memberToken = await mintMemberToken(request, token, member.id, 1);
    const summary = uniqueName('單選聊天草稿');
    const draft = uniqueName('聊天字不能丟');
    const card = await createReplyCardAs(request, memberToken, {
      kind: 'decision',
      summary,
      options: options(['保留', '送出'], 1),
      select_mode: 'single',
    });

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: member.name }).click();
    const chatCard = page.locator(
      `[data-testid="chat-reply-card"][data-reply-card-id="${card.id}"]`,
    );
    await expect(chatCard).toBeVisible();
    await chatCard.locator('.chat__input').fill(draft);
    await chip(chatCard, 1).click();

    // UOC_ASSERT id=UOC-RC-SINGLE-DRAFT screen=chat-page name=single_option_keeps_draft_in_chat
    await expect(
      chatCard.getByTestId('final-answer'),
      'the chat one-tap answer must keep the draft text',
    ).toContainText(draft);

    const readback = await readReplyCardAs(request, memberToken, card.id);
    expect(readback.answer.option_idxs).toEqual([1]);
    expect(readback.answer.text).toBe(draft);
  });
});
