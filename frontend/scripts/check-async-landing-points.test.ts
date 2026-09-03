// check-async-landing-points.test.ts — the guard's OWN guard (T-48, R13-6).
//
// The census exists because eleven instances of one defect family got past a
// hand-maintained list. A census nobody has watched fail is worth exactly as
// much as that list was, so every way of getting past it is replayed here: the
// real sources are copied to a temp tree, ONE of them is edited, and the script
// must exit non-zero. ASYNC_LANDING_SRC exists for exactly this.

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "check-async-landing-points.mjs");
const REAL_SRC = join(HERE, "..", "src");

let root: string;

beforeAll(() => {
  root = mkdtempSync(join(tmpdir(), "async-landing-"));
});
afterAll(() => {
  rmSync(root, { recursive: true, force: true });
});

/** Run the guard over a fresh copy of the real sources, after `sabotage` has had
 *  its way with them. Returns the exit code and the combined output. */
function run(
  sabotage?: (edit: (rel: string, f: (code: string) => string) => void) => void,
) {
  const src = mkdtempSync(join(root, "src-"));
  cpSync(REAL_SRC, src, { recursive: true });
  sabotage?.((rel, f) => {
    const file = join(src, rel);
    writeFileSync(file, f(readFileSync(file, "utf8")));
  });
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, ASYNC_LANDING_SRC: src },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}` };
  }
}

const CHAT_AREA = "components/ChatArea.tsx";

describe("check-async-landing-points", () => {
  it("passes on the tree as shipped", () => {
    const { code, out } = run();
    expect(out, out).toContain("[async-landing] ok");
    expect(code).toBe(0);
  });

  it("reddens when a NEW landing point appears with no verdict", () => {
    // The whole point: a new `setTimeout` in ChatArea is an unanswered question
    // until somebody writes down what happens if it commits after the owner has
    // left the room.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          "  const isComposingRef = useRef(false);",
          "  const isComposingRef = useRef(false);\n  setTimeout(() => {}, 0);",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/ChatArea.tsx | setTimeout/setInterval");
  });

  it("reddens when a landing point DISAPPEARS, so the register cannot describe code that is gone", () => {
    const { code, out } = run((edit) =>
      edit("hooks/useAttachmentStaging.ts", (code) =>
        code.replace("const reader = new FileReader();", "const reader = fr();"),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("hooks/useAttachmentStaging.ts | FileReader");
  });

  it("reddens on a SECOND occurrence appended to an already-counted line (R10-5 B5)", () => {
    // The census counts occurrences, not lines. It used to count lines, so a
    // second `await` on a line that already had one was free.
    const { code, out } = run((edit) =>
      edit("lib/shareLink.ts", (code) =>
        code.replace("await ", "await await "),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("lib/shareLink.ts | await");
  });

  it("reddens when a SECOND module-level per-room table grows outside the draft store (R14-3.1)", () => {
    // The instance that actually happened: `liveComposers`, a peer-keyed table
    // in ChatArea.tsx rather than in chatDraftStore.ts. The store's own test
    // cannot see it — it reads the store's file. This walks the whole graph.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          "export function ChatArea({",
          'const liveComposers = new Map<string, () => void>();\nexport function ChatArea({',
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("components/ChatArea.tsx | liveComposers");
  });

  it("reddens when registered module-level state disappears, so the register cannot describe a table that is gone", () => {
    const { code, out } = run((edit) =>
      edit("lib/chatDraftStore.ts", (code) =>
        code.replace("const drafts = new Map", "const draftsById = new Map"),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("registered module-level state that no longer exists");
    expect(out).toContain("lib/chatDraftStore.ts | drafts");
  });

  it("reddens when a SECOND component calls useQuotedMessageOverlay (R14-1.6)", () => {
    // The overlay carries no room stamp of its own: it relies on ChatArea being
    // unmounted by a room switch. A caller keyed on a card id is not.
    const { code, out } = run((edit) =>
      edit("components/ReplyComposer.tsx", (code) =>
        code.replace(
          'import { useI18n } from "../i18n";',
          'import { useI18n } from "../i18n";\nimport { useQuotedMessageOverlay } from "../hooks/useQuotedMessageOverlay";\nconst _q = () => useQuotedMessageOverlay((id: string) => id);',
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("useQuotedMessageOverlay's callers changed");
    expect(out).toContain("components/ReplyComposer.tsx");
  });

  it("reddens when a file drops out of the WALK (R10-5 B1/B2/B3)", () => {
    // The population is derived from ChatArea's imports. A file that stops being
    // reachable takes its landing points with it, silently, unless the register
    // notices the rows are gone.
    const { code, out } = run((edit) =>
      edit(CHAT_AREA, (code) =>
        code.replace(
          'import { ChatGalleryPanel } from "./ChatGalleryPanel";',
          "const ChatGalleryPanel = (() => null) as never;",
        ),
      ),
    );
    expect(code, out).not.toBe(0);
    expect(out).toContain("ChatGalleryPanel.tsx");
  });
});
