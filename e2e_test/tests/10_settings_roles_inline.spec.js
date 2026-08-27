// e2e_test/tests/10_settings_roles_inline.spec.js
// B10 · 角色誌/監控 inline create rows + role rename/reset gating (M2-2 batch:
// 8d947d1 / 23d8063 / cee768f / 49d1930 / 3f88128).
//
//   • 角色誌「新增角色定義」: the add entry grows a single-field inline row
//     (name only); the server names the founding member itself and mints both
//     ids; Esc collapses without creating.
//   • CUSTOM role detail: the 角色名 gets the pencil InlineEdit (rename rides
//     the role PATCH choke and the roster follows); the 版本紀錄 list carries NO
//     初始版本 row (the server 404s a custom reset — the affordance is honestly
//     omitted); NO internal `role-….md` filename chip.
//   • SEED role detail: name LOCKED (no pencil), the 版本紀錄 list DOES carry
//     初始版本 (a file seed exists to restore).
//
// The reset affordance moved at d0c2ea3 (T-1f39): the doc card's standalone
// 重置 became 版本紀錄, and the reset is that list's last row.
//   • 監控「新增機器 / 上線」: the same inline pattern — the machine row is
//     created under the typed name (no real warden needed; the onboard row
//     surfaces immediately), Esc collapses without creating.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  listMembers,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

// English names on purpose: the inline rows guard IME composition (keyCode 229)
// — a CJK Enter would be swallowed as a candidate-confirm, not a submit.
const ROLE_NAME = uniqueName('Test Officer');
const ROLE_RENAMED = uniqueName('Chief Tester');
const MACHINE_NAME = uniqueName('e2e-box');

async function openSettings(page) {
  await page.locator('button[aria-label="settings"]').click();
  await expect(page.locator('.settings__title')).toBeVisible();
}

test.describe('B10 · settings roles + monitor — inline create rows & gating', () => {
  test('角色誌: inline create → server-named founding member; rename custom / lock seed; reset seed-only', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    await bootAuthedSpa(page, token);
    await openSettings(page);
    await page.locator('.set-entry', { hasText: '角色誌' }).click();

    // ── negative first: Esc collapses the inline row without creating ──
    await page.locator('.add-entry').click();
    const row = page.getByTestId('role-create-row');
    await expect(row, 'the add entry must grow the inline row').toBeVisible();
    await page.getByTestId('role-create-name').fill('ShouldNeverExist');
    await page.getByTestId('role-create-name').press('Escape');
    await expect(row, 'Esc must collapse the row').toHaveCount(0);
    const rolesAfterEsc = await (
      await request.get(`${BASE}/api/roles`, { headers: authHeaders(token) })
    ).json();
    expect(
      rolesAfterEsc.find((r) => r.name === 'ShouldNeverExist'),
      'Esc must not create anything',
    ).toBeUndefined();

    // ── inline create: name only, Enter submits ──
    await page.locator('.add-entry').click();
    await page.getByTestId('role-create-name').fill(ROLE_NAME);
    await page.getByTestId('role-create-name').press('Enter');
    const roleEntry = page.locator('.set-entry', { hasText: ROLE_NAME });
    await expect(roleEntry, 'the new custom role must surface in the list').toBeVisible();

    // API對照: the server minted the role AND its ONE founding member, naming
    // the member ITSELF from the name pool (never the role name, never blank).
    const roles = await (
      await request.get(`${BASE}/api/roles`, { headers: authHeaders(token) })
    ).json();
    const role = roles.find((r) => r.name === ROLE_NAME);
    expect(role, 'the custom role must exist server-side').toBeTruthy();
    expect(role.key, 'the server mints the role key').toMatch(/^r-[0-9a-f]{12}$/);
    const founding = (await listMembers(request, token)).find(
      (m) => m.role_key === role.key,
    );
    expect(founding, 'the founding member must exist').toBeTruthy();
    expect(founding.name, 'the server names the member itself').not.toBe('');
    expect(founding.name, 'the member name is not the role name').not.toBe(ROLE_NAME);
    expect(founding.role_name, "the member's role_name follows the role").toBe(ROLE_NAME);

    // ── custom role detail: pencil rename, no reset, no internal filename ──
    await roleEntry.click();
    const title = page.locator('.settings__title--doc');
    await expect(title).toContainText(ROLE_NAME);
    // no internal `role-….md` filename chip anywhere on the page
    await expect(
      page.locator('.doc-card__file'),
      'the internal role file name must never render',
    ).not.toContainText(/role-.*\.md/);
    // enter doc edit mode: a CUSTOM role offers 儲存/取消 but NO 重置.
    // Scope to the ROLE doc card (.first()) — the page also renders the shared
    // per-role LessonsCard below, which has its own edit affordance.
    const roleDocCard = page.locator('.doc-card').first();
    await roleDocCard.locator('.doc-btn--edit').click();
    // d0c2ea3 (T-1f39) removed the standalone 重置 button from the doc card and
    // put 版本紀錄 in its place; the reset became the version list's LAST row,
    // 初始版本 (`doc-history-seed`), rendered only where `onReset` is wired.
    // ⚠ So asserting a MISSING 重置 button here is now vacuously green — that
    // node no longer exists for ANY document. The seed-only gating is asserted
    // where it actually lives: open the list and demand the 初始版本 row be
    // absent for a custom role (SettingsPage passes onReset only when isSeed).
    await roleDocCard.getByTestId('doc-history-entry-role_definition').click();
    const customHistory = roleDocCard.getByTestId('doc-history-list');
    await expect(customHistory, 'the 版本紀錄 list must open').toBeVisible();
    await expect(
      customHistory.getByTestId('doc-history-seed'),
      'a custom role has no file seed to restore — the 初始版本 row must be absent',
    ).toHaveCount(0);
    await customHistory.getByTestId('doc-history-list-close').click();
    await expect(customHistory).toHaveCount(0);
    await roleDocCard.locator('.doc-card__actions .doc-btn', { hasText: '取消' }).click();
    // rename via the title pencil InlineEdit
    await title.locator('.inline-edit__iconbtn').click();
    const nameInput = title.locator('.inline-edit__input');
    await nameInput.fill(ROLE_RENAMED);
    await nameInput.press('Enter');
    await expect(title, 'the title must show the committed rename').toContainText(ROLE_RENAMED);
    // roster follows (single truth role.name): the founding member's card in
    // the office roster reads the NEW role name.
    // T-8f6e removed the ‹返回 back row — navigate up via the shared breadcrumb
    // (nav.crumbs; the 角色誌 parent segment jumps back to the roles list).
    await page.locator('nav.crumbs .crumbs__seg', { hasText: '角色誌' }).click();
    await expect(
      page.locator('.set-entry', { hasText: ROLE_RENAMED }),
      'the roles list must show the renamed role',
    ).toBeVisible();
    await page.locator('.nav-tab', { hasText: '辦公室' }).click();
    await expect(
      page
        .locator('.member-card', { hasText: founding.name })
        .locator('.member-card__presence'),
      "the roster member card's role label must follow the rename",
    ).toContainText(ROLE_RENAMED);

    // ── seed role (特助): locked name (no pencil), 重置 offered ──
    // Matched on the role's own LABEL. It used to be '助理', which came from the
    // persona's first body line — the roster row printed a one-line preview of
    // `definition_md`. T-1170 removed both the field from the list answer and
    // the preview from the row (owner-approved, card rc-a86771f4476f), so that
    // text is no longer on this page at all.
    await openSettings(page);
    await page.locator('.set-entry', { hasText: '角色誌' }).click();
    await page.locator('.set-entry', { hasText: '特助' }).first().click();
    await expect(page.locator('.settings__title--doc')).toBeVisible();
    await expect(
      page.locator('.settings__title--doc .inline-edit__iconbtn'),
      'a seed role name is locked — no rename pencil',
    ).toHaveCount(0);
    const seedDocCard = page.locator('.doc-card').first();
    await seedDocCard.locator('.doc-btn--edit').click();
    await seedDocCard.getByTestId('doc-history-entry-role_definition').click();
    const seedHistory = seedDocCard.getByTestId('doc-history-list');
    await expect(seedHistory, 'the 版本紀錄 list must open').toBeVisible();
    await expect(
      seedHistory.getByTestId('doc-history-seed'),
      'a seed role has a file seed to restore — the 初始版本 row must be offered',
    ).toBeVisible();
  });

  test('監控: 新增機器/上線 inline row creates the named machine; Esc collapses', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    await bootAuthedSpa(page, token);
    await page.locator('.nav-tab', { hasText: '監控' }).click();

    // negative: Esc collapses without creating
    await page.locator('#mon-onboard-entry').click();
    const row = page.getByTestId('mon-onboard-row');
    await expect(row).toBeVisible();
    await row.locator('input').fill('ghost-machine');
    await row.locator('input').press('Escape');
    await expect(row, 'Esc must collapse the onboard row').toHaveCount(0);
    const machinesAfterEsc = await (
      await request.get(`${BASE}/api/machines`, { headers: authHeaders(token) })
    ).json();
    expect(
      machinesAfterEsc.find((m) => m.display_name === 'ghost-machine'),
      'Esc must not onboard anything',
    ).toBeUndefined();

    // create: type the name, Enter → the machine row surfaces under that name
    // (no real warden needed — the registry row exists immediately, offline).
    await page.locator('#mon-onboard-entry').click();
    await row.locator('input').fill(MACHINE_NAME);
    await row.locator('input').press('Enter');
    await expect(row, 'a successful create collapses the row').toHaveCount(0, {
      timeout: 10_000,
    });

    // ── T-10: both assertions below are anchored so the 5s trailing poll can
    // NOT be what satisfies them. Without that, this step is a luck detector.
    //
    // MonitorPage's onboard awaits `refetchMachines()` and only THEN collapses
    // the row, so under a correct hook the new row is already committed when
    // the collapse above passes — required latency is zero. When the hook
    // instead discards that refetch (the T-10 defect), the row arrives only on
    // the trailing poll, and both assertions must fail rather than wait it out.
    //
    // Measured on this suite's own flow (fresh station, /api/settings reports
    // monitoring_refresh_seconds=5): the row collapses ~0.25s after mount and
    // the trailing poll lands ~5.1s after mount — a gap of 4907/4950/5019ms
    // over three runs. Playwright's default expect timeout is 5000ms (this
    // config sets no `expect.timeout`), so the old bare `toContainText` was
    // racing that poll to within ~50ms and usually LOSING the race in the
    // defect's favour — i.e. going green on a broken hook. The CI trace on
    // run 33033163627 is the same race landing 4ms the other way.
    //
    // (1) The causal anchor: read the table at the moment of collapse. No later
    //     poll, at any speed, can retroactively satisfy this.
    const tableAtCollapse = await page
      .locator('.mon-table, table')
      .first()
      .innerText();
    expect(
      tableAtCollapse,
      'the new row must already be in the table when the inline row collapses — ' +
        'arriving later means the create refetch was discarded and only the 5s poll repaired it',
    ).toContain(MACHINE_NAME);

    // (2) The retrying assertion, kept for a genuinely slow render, but with an
    //     explicit budget FAR below the ~4.9s poll gap. 2s is ~2900ms of
    //     clearance under the poll while still granting 2s to a DOM check that
    //     is already satisfied, so it cannot turn into a new false RED — the
    //     failure mode this whole ticket must not create.
    await expect(
      page.locator('.mon-table, table').first(),
      'the machine table must show the new row under the typed name',
    ).toContainText(MACHINE_NAME, { timeout: 2_000 });
    // API對照: the registry carries it (created via POST /api/machines).
    const machines = await (
      await request.get(`${BASE}/api/machines`, { headers: authHeaders(token) })
    ).json();
    expect(
      machines.find((m) => m.display_name === MACHINE_NAME),
      'the machine registry must carry the onboarded machine',
    ).toBeTruthy();
  });

  // ── T-10 deterministic regression guard ─────────────────────────────────
  // The test above is now a CORRECT detector, but not a deterministic one: it
  // only reddens on the runs where the race actually flips, which unforced is
  // ~1% (the member frame is delivered uniformly within 250ms of the POST —
  // server api_infra.go `ssePoll = 250ms` — while the reconciling GET resolves
  // in single-digit ms). A regression that reproduces 1% of the time gets read
  // as "fine". This case removes the luck: it holds the reconciling GET open
  // long enough that the frame CANNOT miss it, so the exact code path CI hits
  // intermittently is exercised on every run.
  //
  // It widens the race window; it does NOT relax any timeout, and it leaves the
  // assertions above untouched.
  test('監控: 建立後那支重抓不得被 onboard 自己觸發的 member 幀作廢（T-10 迴歸護欄，強制重疊）', async ({
    page,
  }) => {
    const token = await ownerToken(page.request);

    // Record every SSE frame the page's own EventSource delivers, with a clock
    // shared with resource timing (performance.now()).
    await page.addInitScript(() => {
      const Real = window.EventSource;
      window.__SSE = [];
      function Wrapped(url, cfg) {
        const es = new Real(url, cfg);
        es.addEventListener('message', (e) => {
          let topic = null;
          try {
            topic = JSON.parse(e.data).topic ?? null;
          } catch {
            topic = '(non-json)';
          }
          window.__SSE.push({ t: performance.now(), topic });
        });
        return es;
      }
      Wrapped.prototype = Real.prototype;
      window.EventSource = Wrapped;
      window.__REAL_ES = Real;
    });

    await bootAuthedSpa(page, token);
    await page.locator('.nav-tab', { hasText: '監控' }).click();
    expect(
      await page.evaluate(() => window.EventSource !== window.__REAL_ES),
      'the SSE probe must be installed on the constructor the app actually uses',
    ).toBe(true);

    // Hold the reconciling GET open 400ms — comfortably longer than the 250ms
    // worst-case frame delivery, so the overlap is certain rather than lucky.
    const HOLD_MS = 400;
    await page.route('**/api/machines', async (route) => {
      if (route.request().method() === 'GET') {
        await new Promise((r) => setTimeout(r, HOLD_MS));
      }
      await route.continue();
    });

    const name = uniqueName('e2e-race');
    await page.locator('#mon-onboard-entry').click();
    const raceRow = page.getByTestId('mon-onboard-row');
    await expect(raceRow).toBeVisible();
    await raceRow.locator('input').fill(name);
    await raceRow.locator('input').press('Enter');
    await expect(raceRow, 'a successful create collapses the row').toHaveCount(0, {
      timeout: 15_000,
    });

    // Read the table at the moment of collapse — before any poll could run.
    const tableAtCollapse = await page.locator('.mon-table, table').first().innerText();

    const evidence = await page.evaluate(() => ({
      frames: window.__SSE,
      machineCalls: performance
        .getEntriesByType('resource')
        .filter((e) => e.name.includes('/api/machines'))
        .map((e) => ({ start: Math.round(e.startTime), end: Math.round(e.responseEnd) })),
    }));

    // ── ANTI-VACUITY GATE ────────────────────────────────────────────────
    // Without these three, a run that never actually staged the race would go
    // green and look like a pass — the failure mode that makes a guard worse
    // than no guard at all. Each says "this run does not count", not "the
    // product is fine".
    const reconcilingGet = evidence.machineCalls[evidence.machineCalls.length - 1];
    expect(
      reconcilingGet && reconcilingGet.end - reconcilingGet.start,
      `the 400ms hold must have applied to the reconciling GET — got ${JSON.stringify(reconcilingGet)}; ` +
        'without it the race window was never widened and this run proves nothing',
    ).toBeGreaterThan(HOLD_MS * 0.8);
    const memberFrames = evidence.frames.filter((f) => f.topic === 'member');
    expect(
      memberFrames.length,
      'POST /api/machines must have produced a member frame — no frame means the ' +
        'cancelling event never arrived and this run proves nothing',
    ).toBeGreaterThan(0);
    const inWindow = memberFrames.some(
      (f) => f.t > reconcilingGet.start && f.t < reconcilingGet.end,
    );
    expect(
      inWindow,
      `a member frame must land INSIDE the reconciling GET [${reconcilingGet.start}, ${reconcilingGet.end}] — ` +
        `frames were ${JSON.stringify(memberFrames)}; outside it, the cancellation never had ` +
        'anything to cancel and this run proves nothing',
    ).toBe(true);

    // ── THE GUARD ────────────────────────────────────────────────────────
    // The race was provably staged. Under a correct hook the reconciling GET's
    // own answer lands, so the row is on screen the instant the inline row
    // collapses. Under the T-10 defect that answer is discarded and the row
    // cannot appear until the 5s trailing poll — which has not run here.
    expect(
      tableAtCollapse,
      'with a member frame landing mid-refetch, the create refetch must STILL put the new ' +
        'row on screen — if it only appears later, the frame cancelled it and the 5s poll repaired it',
    ).toContain(name);
  });
});
