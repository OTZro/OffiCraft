# T-081b — independent review

Reviewer: independent (did not build any of this). Worktree
`/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b`, branch `feat/T-081b-theme-token-split`,
all changes uncommitted, base `origin/main` @ `8545b8e`. Read-only throughout; all
experiments were run on copies under the scratchpad.

⚠️ The worktree was **still being edited during this review** (15:54–15:57: `i18n/*`,
`ThemeSettings.tsx`, plus new `docs/T-081b-acceptance.md` and `docs/T-081b-evidence/`).
Every finding below was re-confirmed against the tree as of 16:05.

---

## Verdict

**Do not land as-is. 3 BLOCKERs, 6 SHOULD-FIXes.**

The core engineering is sound and better than the doc claims in places: the CSS
inventory is exactly right, pixel identity is real (proved by screenshot hash),
the theme-identity exclusion is complete and generator-based, and the per-language
cap really is counted on raw input. What fails is the *periphery*: the frozen wire
contract was not updated, the guard lint does not guard, and the new zone tokens
break the product's own theme-authoring flow.

---

## BLOCKERS

### BLOCKER-1 — the guard lint does not catch the most obvious re-merge

`frontend/scripts/check-token-roles.mjs` exists (per its own header, lines 11–15)
to make a re-merge "fail in CI instead" of shipping. Reverting one of the two
exact call sites T-081b moved leaves it green.

```
$ cd <scratch copy of frontend/src + scripts>
$ node scripts/check-token-roles.mjs
[token-roles] ok — 3 split tokens keep to one role each; ...
$ # revert styles/global.css:40 to the pre-ticket value
$ sed -n '39,41p' src/styles/global.css
::-webkit-scrollbar-thumb {
  background: var(--color-indigo);
$ node scripts/check-token-roles.mjs; echo rc=$?
[token-roles] ok — 3 split tokens keep to one role each; ...
rc=0
```

Root cause: line 145 tests `/scrollbar/.test(d.prop + " " + d.rel)`. The parser
never sees selectors, so the WebKit rule — where `prop` is `background` and `rel`
is `styles/global.css` — has no "scrollbar" anywhere to match. Only the Firefox
`scrollbar-color` *property* is caught (verified as a control: caught).

**Six bypasses found in total**, all measured (`>>> BYPASS (lint PASSED)`):

| # | Re-merge | Why it slips |
|---|---|---|
| A | `::-webkit-scrollbar-thumb { background: var(--color-indigo) }` | selectors not parsed; `prop`/`rel` carry no "scrollbar" (line 145) |
| B | any scrollbar rule where the indigo use is **not the first** declaration | same; the accidental catch only works when buffer pollution drags the selector into `prop` |
| C | `color-mix(in srgb, var(--color-overlay) 100%, #000)` | `veilOnly` (line 115) checks only "is it inside a `color-mix()`", never that the partner is `transparent` — contradicting its own comment on line 114. Fully opaque, passes |
| D | any offending declaration placed **inside `styles/theme.css`** | line 121 `if (d.rel === THEME) continue` skips the whole file. Verified with all three tokens re-merged there at once → green |
| E | `var(--color-scrollbar-thumb, var(--color-indigo))` | `var()` fallback; `prop` is still `background` |
| F | `VAR(--color-overlay)` (uppercase) | CSS function names are case-insensitive; all lint regexes are case-sensitive |
| G | indirect alias `--color-ink: var(--color-shadow); --color-surface-sunken: var(--color-ink);` | the alias check (line 165) only looks one hop |

Controls that *were* caught (so the lint is not useless): Firefox
`scrollbar-color`, `--color-shadow` on a `background`, a re-merge inside an
`@media` block, a re-merge in a brand-new `.css` file, a one-hop
`--color-shadow-alias` (caught only by luck — `\b` in the line-165 regex).

Also out of scope entirely: the lint only walks `frontend/src/**/*.css`, so any
CSS-in-JS / inline `style={{}}` / `<style>` in `index.html` is unguarded.

### BLOCKER-2 — the frozen wire spec was not updated, and now states the opposite of the behaviour

`spec/openapi.json` is untouched (`git status --short spec/` → empty) yet the
observable HTTP behaviour changed. The spec still says:

- `ThemeBundleDTO.properties.wording.description` — *"The server **422s** any wording
  that violates the language set, **the key whitelist**, or the value rules."*
- `SettingsUpdateDTO.properties.custom_themes.description` — *"any violation is a
  **422 and nothing is written**."*

Both are now false: an unknown key is a **200** and the response echoes a bundle the
client did not send (silently pruned — asserted by the branch's own
`server/ocserverd/api_settings_test.go:531-536`).

Repo `CLAUDE.md` §13: *"動 wire(HTTP OpenAPI 面)= **先改 `spec/*.json` + owner 過目,
再動碼**"*. `docs/T-081b-token-split-mapping.md` never mentions the spec
(`grep -n "openapi\|spec/"` → no hits).

CI will not catch it: the wire-freeze gate only checks that `ocapi_gen.go`
regenerates byte-identically from the spec, and neither file changed. And
`grep -rn "custom_themes" conformance/` → **no hits**, so the behavioural
regression authority does not cover this route either. `bin/ci.sh` is green
while the contract lies.

### BLOCKER-3 — exporting the built-in theme bakes the zone tokens, breaking the primary authoring flow

The three new zone tokens are the **only** `var()`-valued defaults in the entire
token contract (`grep -n -- '--color-[a-z0-9-]*:\s*var(' styles/theme.css` → lines
10, 11, 12 and nothing else). Export is `getComputedStyle`-based by design
(`themeExport.ts:16-32`), which *resolves* the `var()`:

```
$ node scratchpad/pixel/export.mjs
--color-bg        => "#191c24"
--color-topbar-bg => "#191c24"
--color-nav-bg    => "#191c24"
--color-main-bg   => "#191c24"
```

`ThemeSettings.tsx:175` seeds every new custom theme from
`exportOfficeBaseTheme(...)`. So the author gets a bundle with the three zones
**pinned to the concrete built-in colour**, and the classic re-tint (edit only
`--color-bg` — exactly what the 精靈村 pack does) no longer moves the chrome:

```
$ node scratchpad/pixel/trap.mjs        # 71 tokens seeded from getComputedStyle,
                                        # then --color-bg edited to #3aa06a
narrow: body=rgb(58,160,106)  topbar=rgb(25,28,36)  nav=rgb(25,28,36)  main=rgb(25,28,36)
wide:   body=rgb(58,160,106)  topbar=rgb(25,28,36)  nav=rgb(25,28,36)  main=rgb(25,28,36)
```

In **wide** layout the gutter is 0px (measured: `.app__main left=0 width=1440`),
so the entire visible chrome stays dark slate and the author's `--color-bg` edit
is *completely invisible*. Before this ticket the same edit re-tinted everything.

The doc's §7 backward-compat claim is correct only for **pre-existing** packs
(they carry no zone tokens → fall back to `var(--color-bg)` → verified fine).
It does not hold for any theme authored from now on inside the product. Doc §7
even flags the wide-layout hazard, but frames it as advice to theme authors
rather than as a regression the export path causes.

Fix options: skip `var()`-defaulted tokens on export, or make the editor treat
"equal to the default" as "don't store", or drop the `var()` default in favour of
explicit inheritance.

---

## SHOULD-FIX

### SF-1 — the in-app wording editor destroys the three space-bearing fragments

`frontend/src/components/ThemeSettings.tsx:387`

```ts
if (typeof val === "string" && val.trim() !== "") kept[code] = val.trim();
```

Three of the fragments T-081b *newly made overridable* carry semantically
required boundary whitespace:

| code | en | zh |
|---|---|---|
| `monitor.machine.bootstrapFailedLead` | `en.ts:831` `"Install failed (exit code "` | `zh.ts:920` `"安裝失敗(結束碼 "` |
| `monitor.machine.uninstallWarnBody2` | `en.ts:859` `"” still has "` | `zh.ts:944` `"」上還有 "` |
| `monitor.machine.uninstallWarnBody3` | `en.ts:860` `" member(s) online on it. …"` | `zh.ts:945` `" 位成員在線上。…"` |

Re-typing the same words in the editor yields:
`"“mac-01” still has3member(s) online on it."` /
`"「mac-01」上還有3位成員在線上。"`. Overriding through the product's own editor —
the intended path — produces broken text. It also contradicts `compose.ts`'s own
stated rule that fragment values stay free of boundary whitespace and the join
carries spacing. Both languages want a space on both sides here, so the fix is
free: trim the fragments and put a literal `" "` in the join.

### SF-2 — 10 of 71 tokens have no friendly label; the editor shows raw `--color-*` names

`frontend/src/lib/themeTokenMeta.ts` was not touched. Its own header states the
requirement: *"The theme editor never shows a raw `--color-*` name any more
(owner: colour editing must be human-friendly)"*.

```
$ node -e '...'   # cross-check themeTokens.generated.ts against themeTokenMeta.ts
tokens: 71 with meta: 61 MISSING META: 10
--color-backdrop --color-knob --color-main-bg --color-nav-bg --color-on-backdrop
--color-on-danger --color-on-indigo --color-scrollbar-thumb --color-surface-sunken
--color-topbar-bg
```

`tokenMeta()` degrades to `{ group: "other", label: token }`, so all ten render as
raw CSS names in a catch-all group, in both zh and en. This compounds BLOCKER-3:
the very tokens an author must now fix by hand are the unlabelled ones. Not
mentioned anywhere in the docs (`grep -rn "themeTokenMeta" docs/ frontend/CLAUDE.md`
→ no hits).

### SF-3 — the guard lint has a false positive that teaches the *wrong* fix

A perfectly legitimate `box-shadow` that happens to be the **first** declaration
of a rule is rejected:

```
components/office.css:1984  }    .zzz-fp {  box-shadow: 0 1px 2px color-mix(in srgb, var(--color-shadow) 20%, transparent)
    --color-shadow used on '}    .zzz-fp {  box-shadow'; it is the box-shadow ink only.
    Use --color-surface-sunken (sunken surface) or --color-backdrop (lightbox/preview scrim).
```

`declarations()` (lines 77–107) is not a CSS parser: it never tracks `{`/`}`, so
the first declaration after a selector gets the selector glued into `prop`, and
`/(^|-)box-shadow$/` (line 135) then fails to match. Today's tree only passes
because no existing `--color-shadow` box-shadow happens to sit first in its rule.
The advice printed is actively harmful — following it would produce exactly the
mis-colouring the lint exists to prevent.

### SF-4 — legacy DB rows are never re-validated

`server/ocserverd/settings.go:337-342` loads `display.custom_themes` with a bare
`json.Unmarshal`, no `validateThemeBundles`. A row written before T-081b keeps
`profile.themeOffice` in the DB and is echoed on every `GET /api/settings` until
the owner happens to PATCH themes again. Inert in the UI (`i18n/wording.ts:36`
`setPath` refuses to write a key that is not an existing string leaf, and
`profile.themeOffice` no longer exists), but the doc's "a re-export carries only
live codes" only becomes true after the next write.

### SF-5 — the drop has zero signal, not even a server-side log

`grep -n "log\.\|slog\|Printf" server/ocserverd/wording_bundle.go` → nothing.
The *user-facing* silence is the owner's explicit ruling (ACCEPTED-TRADEOFF), but
the operator-facing silence is a free win not taken, and it sits against the
repo's own stated principle in `api_helpers.go:84-89`: *"DisallowUnknownFields —
any key the DTO does not declare is a 422, **not a silent drop**. This is the
single highest-leverage guard"*, written after a silent-drop bug wiped a document.

### SF-6 — `tasks.elapsed` left un-split beside a key that was split

`TaskCard.tsx:1565-1566` renders `{msg.taskProgress(…)} · {t.tasks.elapsed(elapsedText)}`
→ "步驟 3/7 · 已歷時 2h". After this change 「步驟」 is theme-overridable and 「已歷時」
is not, **on the same visible string** — the exact "同一句話有時換得掉、有時換不掉"
defect the doc's §5 went out of its way to eliminate for the `bootstrapError` twin.

---

## NITs

- **Doc §4 count is stale.** It says the whitelist goes "61 → 68 槽". Measured:
  base `git show 8545b8e:...themeTokens.generated.ts | grep -c '"--color-'` → **61**;
  head → **71** (`gen-theme-tokens` prints `wrote 71 tokens`). §7's three zone tokens
  were added later and §4 was never updated.
- `themeExport.ts:74` still documents `parseImportedBundle` as *"Never mutates
  anything"*. It now mutates its parsed input via the validator — that is how the
  drop reaches the returned bundle. Contradicts repo CLAUDE.md §8.
- `gen-message-keys.mjs` excludes `key === "themeIdentity"` at **any** depth; a
  future unrelated `foo.themeIdentity` would be silently non-overridable.
- `gen-theme-tokens.mjs` runs its extraction regex over the **raw** theme.css
  including comments (no `stripComments`, unlike the two lints). Harmless today —
  no comment contains `--color-x:` — but this ticket added several token-naming
  comments, so the trap is now much closer.
- Validator mutates the caller's maps on the **failure** path too (both TS and Go),
  and `api_stub.go:371-375` `displayCustomThemesSnapshot()` is a shallow copy whose
  `Wording` pointers alias live in-memory state. Latent, not reachable today.
- Trim semantics diverge (pre-existing): JS `trim()` strips U+FEFF, Go
  `strings.TrimSpace` does not — a value of `"﻿"` is rejected offline,
  accepted online.
- `NIT` value rules are never applied to dropped keys (parity holds), so the only
  bound on a junk payload is the 1000-key raw cap; there is no `MaxBytesReader`
  on the route.
- `compose.ts:120` hardcodes `" · "` and a `language === "zh" ? ":" : ": "`
  conditional, in a module that otherwise routes exactly that through `sp`.
- World-view nouns still stuck in non-overridable leaves: `office.staffSub`
  (`${n} people` / `${n} 人`), `office.outsource.workerSub`, `chat.attachTooMany`,
  `tasks.parallel`. Doc §5 classified these as "純機械文字(計數)"; 「人」 is not.
- `effortText` is declared `=> string` but really returns `string | undefined`
  (callers still write `?? a.effort`, `TaskManualsPage.tsx:934`).

---

## ACCEPTED-TRADEOFF

- Unknown wording codes vanish silently for the theme author — the owner's
  explicit ruling `rc-1599a0026a80`.
- `--color-backdrop` is a new token rather than a re-use of `--color-scrim`
  (would have changed built-in pixels). Doc §2 reasons this correctly.

---

## What I verified and found CLEAN

### Inventory — doc counts re-derived from scratch, all correct

| Doc claim | Measured at `8545b8e` | ✓ |
|---|---|---|
| `--color-overlay` 212 (202 kept + 9 moved + 1 def) | 211 `var()` refs + 1 def = 212; 8 opaque uses + the `:1005` circle bg = 9 moved; head has exactly 202, **all** inside `color-mix()` | ✓ |
| `--color-shadow` 24 (11 box-shadow + 12 bg + 1 def) | 23 refs + 1 def; 11 `box-shadow`, 12 `background` (10 sunken + 2 backdrop) | ✓ |
| `--color-indigo` 20 (16 kept + 2 scrollbar + def + comment) | 18 refs + def + comment; head has 16 in `components/` — 13 background + 3 border, exactly as tabulated | ✓ |
| smurf-village: 186 wording, 1 illegal, 61 colours all legal | reproduced on **both** validators: `dropped=1 [profile.themeOffice]`, `surviving=185`, `bad tokens=[] bad values=[]`, pack accepted | ✓ |

Every individual `檔案:行` in the doc's §1 and §2 per-site tables matched the base
tree. Only §4's "61 → 68" is wrong (see NITs).

### Pixel identity — real browser measurement, not reasoning

Chromium (playwright 1.61.1), base CSS tree vs head CSS tree, 25 selectors
covering every moved call site:

```
$ node scratchpad/pixel/measure.mjs
DIFF .topbar     base background-color=rgba(0,0,0,0) → head rgb(25,28,36)
DIFF .nav-tabs   base background-color=rgba(0,0,0,0) → head rgb(25,28,36)
DIFF .app__main  base background-color=rgba(0,0,0,0) → head rgb(25,28,36)
25 selectors measured, 3 computed-style differences.
```

All 20 split-token call sites are computed-style **identical**, including
`.chat__lightbox-close` (both `color` and the 10% circle background) and both
scrollbar surfaces. The only three deltas are the zone selectors going
transparent → the same colour as the body behind them.

Rendered pixels, full-page screenshot SHA-256, both layouts:

```
$ node scratchpad/pixel/shot.mjs
  narrow: .app__main left=200 width=1040 gutter=400px
narrow: base=a283779fc2ee9518 head=a283779fc2ee9518 → PIXEL-IDENTICAL
  wide:   .app__main left=0 width=1440 gutter=0px
wide:   base=8a818584802ed14f head=8a818584802ed14f → PIXEL-IDENTICAL
```

The gutter measurements independently confirm doc §7's 400px / 0px claim.
Layering works and is genuinely new:

```
layering(head, zones set): body=rgb(25,28,36) .topbar=rgb(255,0,0) .nav-tabs=rgb(0,255,0) .app__main=rgb(0,0,255)
layering(base, zones set): body=rgb(25,28,36) .topbar=rgba(0,0,0,0) .nav-tabs=rgba(0,0,0,0) .app__main=rgba(0,0,0,0)
```

### i18n — 34 keys, both languages, executed comparison

Old template vs new `makeMessages(...)` output diffed codepoint-by-codepoint in a
scratch harness: **79 tests, zero byte drift**, covering the `interAgentExpand`
0/1/2 plural branch, the `“” / 「」 / "` quote variants, the zh `嗎？` vs en `?`,
`uninstallWarnBody` at count 1 and 3 with `member(s)` and 「位」, and the
`statusOf`/`effortOf` lookup tables including their unknown-value passthrough.
Both folded twins render identically. en/zh leaf sets are identical in name *and*
kind. 15 dead keys exist — **all 15 pre-existing at `8545b8e`**; T-081b introduced
none. No call site references a removed function.

### Validation strictness — I tried to smuggle things past and failed

The per-language cap is counted on **raw** input on both sides, measured, not read:

```
Go: [cap-raw] raw=6000 cap=1000 err=theme: wording[zh] holds more than 1000 entries
    [cap+1 all junk] err=theme: wording[zh] holds more than 1000 entries
TS: raw entries=6000 → error "theme: wording[zh] holds more than 1000 entries"
    [cap+1 all junk] → same error
```

TS and Go agree exactly on every rule and in the same order (language allowlist →
raw cap → per-entry drop → value rules): language set, 1000-entry cap, control
chars, 1..200 runes after trim, empty values, duplicate-key last-wins. No unknown
key survives into storage (`[smurf] persisted JSON contains profile.themeOffice:
false`), into the export path, or into the applied overlay (three independent
layers). Junk keys are dropped in place and only the pruned map is marshalled;
there is exactly one write path.

### Theme identity — exclusion is complete and load-bearing

The rule is in the **generator** as §6 required (`gen-message-keys.mjs:80,87-90`),
a structural subtree skip, no hand-kept second list. Exactly two keys left the
whitelist (`profile.themeNewName`, `profile.themeOffice`) — not over-broad; all
other `profile.theme*` / `settings.theme*` chrome stays overridable. Every
identity surface is covered: picker row, settings row, all three aria-labels, the
**exported file's `name`**, and the default name of a new theme. `nav.office` (the
place) is untouched. Proved load-bearing by neutering the generator rule and
re-running: 771 keys with `themeIdentity.*` back in the list, so the guard test
goes red on a revert.

### Repo checks — all green

```
$ bash bin/ci.sh                      rc=0, last line exactly "[ci] all green"
                                      (incl. 973 conformance tests passed)
$ npm run lint:tokens                 [css-tokens] ok
$ npm run lint:token-roles            [token-roles] ok
$ npm run typecheck                   rc=0
$ npm run test                        162 files, 1256 tests passed
$ gofmt -l server/ocserverd           (empty)
$ go vet ./... / go build ./...       rc=0
$ go test ./...                       ok  ocserverd  47.598s
                                      (after bin/build-seedsdist + bin/build-docsdist)
```

Generator drift, checked in a scratch copy (worktree left clean):
`gen-theme-tokens` → 71 tokens, TS and Go both byte-identical to committed;
`gen-message-keys` → TS and Go both byte-identical to committed.

### Things I tried that did NOT turn up a problem

- Looked for a *missed* call site of any of the three original tokens that
  semantically belongs to the split-off job. There is none — after the change all
  202 `--color-overlay` refs are inside `color-mix()`, all 11 `--color-shadow` refs
  are on `box-shadow`, and all 16 `--color-indigo` refs are saturated fills under
  text. The partition is genuinely complete.
- Checked whether the newly-opaque zones could cover something painted behind them
  (body gradient, `::before` decoration, `backdrop-filter`, a stacking-context
  change). `body` has a flat `background: var(--color-bg)`; the only gradient in
  the shell is the logo mark, which sits *inside* `.topbar`. Nothing is occluded.
- Checked whether the 1040px cap sits inside `.app__main` (which would let the
  new `--color-main-bg` swallow the gutter and break the 5-layer story). It does
  not — `chrome.css:9-15` puts the cap on the three zone elements themselves.
- Tried to defeat the lint via `@media` blocks, a brand-new `.css` file, a
  `text-shadow`, and a one-hop alias — all caught.
- Tried to smuggle a 6000-key junk payload and a 1001-key all-junk payload past
  the per-language cap on both TS and Go — both rejected.
- Tried to get an unknown key into storage / the export / the applied overlay —
  blocked at three independent layers.
- Looked for another string that becomes a theme's name or lets one theme affect
  another (custom-theme names, the export `name`, picker labels, settings title) —
  the exclusion is complete.
- Ran the pre-existing `smurf-village.theme.json` through both validators — it is
  accepted, matching the doc exactly.
- Checked that the built-in `xian` theme is unaffected — there is only one built-in
  (`RESERVED_THEME_IDS = ["office"]`); 修仙 is an importable bundle with no zone
  tokens, so it falls back cleanly.
- Verified `::-webkit-scrollbar-thumb:hover` still uses `--color-text-muted` and
  was correctly left alone (doc §3).
