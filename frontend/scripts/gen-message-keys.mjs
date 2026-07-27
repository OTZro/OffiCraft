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
// SECOND EXCLUSION — a theme must not be able to rename ANOTHER theme (T-081b).
// A `themeIdentity` subtree is excluded whole, even though its leaves are plain
// strings. Everything in it is some theme's own `name`: the row in the theme
// picker, the `name` written into the file when a theme is exported, the
// default name a newly created theme gets. While `profile.themeOffice` was
// overridable, importing a 「精靈村」 pack renamed the BUILT-IN theme to 「精靈村」
// as well — two identically named rows in the picker and no way back to the
// shipped one (owner report 2026-07-27). A theme pack may rename the PLACE
// (`nav.office` — still overridable); it may not rename a THEME.
//
// Like the function exclusion, this is a rule on the dictionary's STRUCTURE and
// not a second hand-kept key list: put a future built-in theme's name inside
// `themeIdentity` and it is non-overridable for free, wherever it is read from.
//
// THIRD EXCLUSION — nor may a theme forge the markers that tell themes APART
// (T-081b review round 3). The `themeMarkers` subtree holds the quick picker's
// built-in / custom <optgroup> headings and the tag a downloaded copy of a
// built-in theme is named with; it is excluded by the SAME structural rule. See
// THEME_MARKERS_SUBTREE below for what each one bought back.
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
const ZH_TS = join(ROOT, "frontend", "src", "i18n", "locales", "zh.ts");
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
const dictZh = await loadDict(ZH_TS, "zh");

// The subtree name that marks THEME IDENTITY — a theme's own `name`. Excluded
// from the whitelist wholesale: see the header for why a theme bundle must not
// be able to rename another theme.
const THEME_IDENTITY_SUBTREE = "themeIdentity";

// The subtree name that marks the theme STRUCTURAL MARKERS — the labels the
// product uses to tell themes APART rather than to name one: the quick picker's
// built-in / custom <optgroup> headings and the tag a downloaded copy of a
// built-in theme is named with. Excluded by the SAME structural rule, for the
// same reason one level along: an overridable group heading lets a pack swap
// 內建 and 自訂 so the grouping lies, and an overridable copy tag lets a pack put
// bidi (or 200 characters) into the file name the built-in theme's download
// button composes — producing a file the product's own importer then refuses.
const THEME_MARKERS_SUBTREE = "themeMarkers";

// Every subtree a `wording` overlay may not reach, by KEY NAME at any depth.
// Both entries are STRUCTURAL rules on the dictionary, which is the whole point:
// a future non-overridable string is covered by moving it into one of these
// subtrees, never by hand-keeping a second key list on each side of the wire.
const NON_OVERRIDABLE_SUBTREES = new Set([
  THEME_IDENTITY_SUBTREE,
  THEME_MARKERS_SUBTREE,
]);

// Recursively collect dotted paths to every STRING leaf. Function leaves
// (interpolation) and any non-string/non-object value are skipped.
function collect(node, prefix, out) {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (NON_OVERRIDABLE_SUBTREES.has(key)) {
      // Theme identity / theme structural markers — not overridable at any
      // depth (T-081b §6 and review round 3, SHOULD-5).
      continue;
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

// THE SAME SUBTREE, READ THE OTHER WAY (T-081b follow-up). Excluding
// themeIdentity from the whitelist stops a pack renaming a theme through
// `wording`; it does not stop a pack simply CALLING ITSELF 辦公室 and putting a
// second 辦公室 row in the picker. Blocking that needs the display names
// themselves on both sides of the wire, so the subtree is ALSO emitted as data —
// keyed on the theme id, valued with every language's spelling of that name.
//
// Emitting the WHOLE subtree (not just the built-ins) keeps this generator
// ignorant of which ids are reserved: each validator intersects the map with its
// own reserved-id set (RESERVED_THEME_IDS / reservedThemeIDs), which is why
// `newTheme` — the default name a NEW custom theme gets, not a theme's
// identity — is never banned. A future built-in theme is covered the moment its
// name is added to the subtree and its id to the reserved set.
const identityNames = {};
for (const d of [dict, dictZh]) {
  for (const [id, name] of Object.entries(d[THEME_IDENTITY_SUBTREE] ?? {})) {
    if (typeof name !== "string") continue;
    (identityNames[id] ??= []).push(name);
  }
}
const identityIds = Object.keys(identityNames).sort();
for (const id of identityIds) {
  identityNames[id] = [...new Set(identityNames[id])].sort();
}

if (identityIds.length === 0) {
  console.error(
    `[gen-message-keys] no ${THEME_IDENTITY_SUBTREE} entries found in the locales — aborting`
  );
  process.exit(1);
}

const banner = (tool) =>
  `// Code generated by frontend/scripts/gen-message-keys.mjs — DO NOT EDIT.\n` +
  `// Source of truth: frontend/src/i18n/locales/{en,zh}.ts (the i18n message-code\n` +
  `// set + the themeIdentity display names).\n` +
  `// Regenerate: npm run gen:msgkeys (${tool}).\n`;

const ts =
  banner("this file") +
  `\n/** The whitelist of i18n message codes a theme bundle's \`wording\` overlay\n` +
  ` * may re-value. Extracted from en.ts (string leaves only — interpolation\n` +
  ` * functions are not overridable). The single source of truth for both the\n` +
  ` * client wording validator (lib/themeBundle.ts) and the mock API. */\n` +
  `export const MESSAGE_KEYS: readonly string[] = [\n` +
  keys.map((k) => `  ${JSON.stringify(k)},`).join("\n") +
  `\n];\n` +
  `\n/** Every theme's own display name, keyed on its theme id and holding one\n` +
  ` * entry per language. Extracted from the locales' \`themeIdentity\` subtree —\n` +
  ` * the same subtree the whitelist above deliberately omits. Intersect it with\n` +
  ` * RESERVED_THEME_IDS to get the names a CUSTOM theme may not claim; the ids\n` +
  ` * outside that set (e.g. \`newTheme\`) are ordinary defaults, not identities. */\n` +
  `export const THEME_IDENTITY_NAMES: Readonly<Record<string, readonly string[]>> = {\n` +
  identityIds
    .map(
      (id) =>
        `  ${JSON.stringify(id)}: [${identityNames[id]
          .map((n) => JSON.stringify(n))
          .join(", ")}],`
    )
    .join("\n") +
  `\n};\n`;

// gofmt aligns the values of a map literal (tabwriter, padding 1). Replicate it
// here so the emitted file is gofmt-clean and the CI drift gate stays byte-
// stable: pad each `"key":` out to the widest one, then a single space + value.
const keyParts = keys.map((k) => `${JSON.stringify(k)}:`);
const widest = Math.max(...keyParts.map((k) => k.length));
const idWidest = Math.max(...identityIds.map((id) => JSON.stringify(id).length + 1));
const go =
  banner("server/ocserverd/message_keys_gen.go") +
  `\npackage main\n\n` +
  `// messageKeys is the whitelist of i18n message codes a theme bundle's\n` +
  `// wording overlay may re-value (wording_bundle.go validation). Extracted from\n` +
  `// frontend/src/i18n/locales/en.ts — the single message-code contract.\n` +
  `var messageKeys = map[string]bool{\n` +
  keyParts.map((k) => `\t${k}${" ".repeat(widest - k.length + 1)}true,`).join("\n") +
  `\n}\n` +
  `\n// themeIdentityNames holds every theme's own display name, keyed on its theme\n` +
  `// id, one entry per UI language. Extracted from the locales' themeIdentity\n` +
  `// subtree — the twin of THEME_IDENTITY_NAMES in messageKeys.generated.ts.\n` +
  `// theme_bundle.go intersects it with reservedThemeIDs to reject a custom\n` +
  `// bundle that claims a BUILT-IN theme's display name; an id outside that set\n` +
  `// (e.g. newTheme) is an ordinary default name and stays claimable.\n` +
  `var themeIdentityNames = map[string][]string{\n` +
  identityIds
    .map((id) => {
      const k = `${JSON.stringify(id)}:`;
      const pad = " ".repeat(idWidest - k.length + 1);
      return `\t${k}${pad}{${identityNames[id].map((n) => JSON.stringify(n)).join(", ")}},`;
    })
    .join("\n") +
  `\n}\n`;

writeFileSync(TS_OUT, ts);
writeFileSync(GO_OUT, go);
console.log(
  `[gen-message-keys] wrote ${keys.length} message keys →\n  ${TS_OUT}\n  ${GO_OUT}`
);
