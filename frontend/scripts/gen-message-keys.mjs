#!/usr/bin/env node
// T-16a1 P3 — message-key whitelist generator (the single source of truth for
// "which i18n message code an owner theme bundle's `wording` overlay may
// re-value").
//
// The i18n dictionaries (src/i18n/locales/{zh,en}.ts) ARE the message-code
// contract: every user-visible string is addressed by an internal key PATH such
// as `nav.tasks` or `profile.themeOffice`. A theme bundle's optional `wording`
// overlay is `{ <lang>: { <code>: <replacement text> } }`; its code set must be
// exactly the dictionary's leaf paths — never a hand-kept second list that
// silently drifts. So we EXTRACT the leaf paths from en.ts (the canonical
// English dict) and emit two committed generated files:
//
//   * frontend/src/i18n/messageKeys.generated.ts — MESSAGE_KEYS, the client +
//     mock validation whitelist.
//   * server/ocserverd/message_keys_gen.go — messageKeys, the server whitelist.
//
// ONLY string-valued leaves are whitelisted. A dictionary leaf can also be an
// interpolation FUNCTION (e.g. `dateOn: (month, day, weekday) => ...`); those
// cannot be replaced by a static string, so they are deliberately NOT
// overridable and excluded here.
//
// SECOND (AND LAST) EXCLUSION — a theme must not be able to rename ANOTHER
// theme (T-081b). The `themeIdentity` subtree is excluded whole, even though its
// leaves are plain strings. Everything in it is some theme's own `name`: the row
// in the theme picker, the `name` written into the file when a theme is
// exported, the default name a newly created theme gets. While
// `profile.themeOffice` was overridable, importing a 「精靈村」 pack renamed the
// BUILT-IN theme to 「精靈村」 as well — two identically named rows in the picker
// and no way back to the shipped one (owner report 2026-07-27). A theme pack may
// rename the PLACE (`nav.office` — still overridable); it may not rename a THEME.
//
// Like the function exclusion, this is a rule on the dictionary's STRUCTURE and
// not a second hand-kept key list: put a future built-in theme's name inside
// `themeIdentity` and it is non-overridable for free, wherever it is read from.
//
// Rounds 3–4 added a THIRD exclusion (`themeMarkers`, the 內建 / 自訂 labels) to
// stop a pack forging the markers that tell themes apart. Round 8 removed it —
// owner ruling: 「這是大家自己用的,自己要怎麼搞我們不用特別管,我們只要確定主題名稱
// 不會隨著主題改變就好」. Those labels are ordinary overridable wording again; the
// ONE guarantee left is this file's single remaining exclusion.
//
// Both outputs are checked into the tree and pinned by a CI drift gate
// (bin/ci.sh): change the dictionary's leaf-string set without re-running
// `npm run gen:msgkeys` and CI goes red — the SAME regen-and-diff discipline as
// the gen-ocapi / schema.ts / gen:tokens gates.
//
// ⚠️ The Go output filename is message_keys_gen.go — it MUST NOT contain the
// substring `_token` (the ci.sh hygiene denylist rejects tracked filenames
// carrying it; T-16a1 P2 hit this with theme_colornames_gen.go).
//
// Run: `npm run gen:msgkeys` (or `node scripts/gen-message-keys.mjs`).

import { readFileSync, writeFileSync, mkdtempSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import * as esbuild from "esbuild";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..", "..");
const EN_TS = join(ROOT, "frontend", "src", "i18n", "locales", "en.ts");
const TS_OUT = join(ROOT, "frontend", "src", "i18n", "messageKeys.generated.ts");
const GO_OUT = join(ROOT, "server", "ocserverd", "message_keys_gen.go");

const tmpDir = mkdtempSync(join(tmpdir(), "genmsgkeys-"));

// The locale files import only TYPES (import type { Dict }, { Effort }) plus a
// couple of local const arrays — esbuild's ts loader erases the type imports,
// leaving a module with no runtime dependencies, so the transformed JS imports
// cleanly.
async function loadDict(tsPath, exportName) {
  const { code } = await esbuild.transform(readFileSync(tsPath, "utf8"), {
    loader: "ts",
    format: "esm",
  });
  const tmpFile = join(tmpDir, `${exportName}.mjs`);
  writeFileSync(tmpFile, code);
  const mod = await import(pathToFileURL(tmpFile).href);
  const d = mod[exportName];
  if (!d || typeof d !== "object") {
    console.error(
      `[gen-message-keys] ${tsPath} did not export a \`${exportName}\` dictionary object — aborting`
    );
    process.exit(1);
  }
  return d;
}

const dict = await loadDict(EN_TS, "en");

// The subtree name that marks THEME IDENTITY — a theme's own `name`. Excluded
// from the whitelist wholesale: see the header for why a theme bundle must not
// be able to rename another theme.
const THEME_IDENTITY_SUBTREE = "themeIdentity";

// The one subtree a `wording` overlay may not reach, by KEY NAME at any depth.
// A STRUCTURAL rule on the dictionary, which is the whole point: a future
// non-overridable string is covered by moving it into this subtree, never by
// hand-keeping a second key list on each side of the wire.
const NON_OVERRIDABLE_SUBTREES = new Set([THEME_IDENTITY_SUBTREE]);

// Recursively collect dotted paths to every STRING leaf. Function leaves
// (interpolation) and any non-string/non-object value are skipped.
function collect(node, prefix, out) {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (NON_OVERRIDABLE_SUBTREES.has(key)) {
      continue; // theme identity — not overridable at any depth (T-081b §6).
    }
    if (typeof value === "string") {
      out.push(path);
    } else if (value && typeof value === "object" && !Array.isArray(value)) {
      collect(value, path, out);
    }
    // functions / arrays / numbers: not overridable plain text — skip.
  }
}

const keys = [];
collect(dict, "", keys);
keys.sort();

if (keys.length === 0) {
  console.error("[gen-message-keys] no string leaves found in en.ts — aborting");
  process.exit(1);
}

// The locales' themeIdentity subtree was ALSO emitted as data (THEME_IDENTITY_
// NAMES / themeIdentityNames) so both validators could reject a CUSTOM theme
// that called itself 辦公室. Round 8 dropped that rule — the owner does not care
// how a user names their own packs, only that the built-in's name survives, and
// that is the whitelist exclusion above, not a name comparison. The emission had
// no other reader, so it is gone rather than left as generated dead data.

const banner = (tool) =>
  `// Code generated by frontend/scripts/gen-message-keys.mjs — DO NOT EDIT.\n` +
  `// Source of truth: frontend/src/i18n/locales/en.ts (the i18n message-code set).\n` +
  `// Regenerate: npm run gen:msgkeys (${tool}).\n`;

const ts =
  banner("this file") +
  `\n/** The whitelist of i18n message codes a theme bundle's \`wording\` overlay\n` +
  ` * may re-value. Extracted from en.ts (string leaves only — interpolation\n` +
  ` * functions are not overridable). The single source of truth for both the\n` +
  ` * client wording validator (lib/themeBundle.ts) and the mock API. */\n` +
  `export const MESSAGE_KEYS: readonly string[] = [\n` +
  keys.map((k) => `  ${JSON.stringify(k)},`).join("\n") +
  `\n];\n`;

// gofmt aligns the values of a map literal (tabwriter, padding 1). Replicate it
// here so the emitted file is gofmt-clean and the CI drift gate stays byte-
// stable: pad each `"key":` out to the widest one, then a single space + value.
const keyParts = keys.map((k) => `${JSON.stringify(k)}:`);
const widest = Math.max(...keyParts.map((k) => k.length));
const go =
  banner("server/ocserverd/message_keys_gen.go") +
  `\npackage main\n\n` +
  `// messageKeys is the whitelist of i18n message codes a theme bundle's\n` +
  `// wording overlay may re-value (wording_bundle.go validation). Extracted from\n` +
  `// frontend/src/i18n/locales/en.ts — the single message-code contract.\n` +
  `var messageKeys = map[string]bool{\n` +
  keyParts.map((k) => `\t${k}${" ".repeat(widest - k.length + 1)}true,`).join("\n") +
  `\n}\n`;

writeFileSync(TS_OUT, ts);
writeFileSync(GO_OUT, go);
console.log(
  `[gen-message-keys] wrote ${keys.length} message keys →\n  ${TS_OUT}\n  ${GO_OUT}`
);
