// lib/docSections.ts — cut a boot-context document into independently editable
// SECTIONS (T-791e).
//
// WHY THIS EXISTS AT ALL. The write door is a whole-document replace, and it
// stays that way — but a whole-document EDITOR cannot serve what this surface
// is for. The workflow it has to carry is: someone hands the owner a set of
// proposed blocks, and he agrees with three of them and not the other four.
// One textarea holding 45,000 characters makes that an all-or-nothing button.
// So the document is cut here, each piece is pasted and applied on its own, and
// the pieces are re-joined into the one replace the wire actually takes.
//
// 🔴 THE JOIN MUST BE EXACT. `joinDocSections(splitDocSections(t)) === t` for
// every input, byte for byte — a splitter that normalises whitespace, drops a
// trailing newline or re-spaces a heading would make merely OPENING the page
// and saving it a silent rewrite of a document that boots every agent in the
// studio. Every section therefore keeps its boundary line and everything after
// it verbatim, up to (not including) the next boundary; nothing is trimmed, and
// the concatenation is a plain join with no separator. docSections.test.ts
// pins the round trip on the real seed files rather than on toy strings.
//
// THE GRAMMAR is two boundaries, chosen from what these documents actually
// look like rather than from markdown in general:
//   * an ATX heading (`# ` … `###### `) — how system_interaction is organised;
//   * a TOP-LEVEL ordered list item (`1. `, `2. ` … at column 0) — how the two
//     boot sequences are organised. Their whole body is one `##` block of
//     numbered steps, so heading-only cutting would give them ONE section and
//     hand back the all-or-nothing button this module exists to remove.
// Indented ordered items are NOT boundaries: they are sub-steps of the item
// above, and cutting there would let a paste land a step's tail in the next
// section.
//
// FENCED CODE IS INERT. A ``` or ~~~ fence suspends both rules until it closes
// — the seeds contain fenced shell blocks whose lines start with `#`, and
// cutting inside one produces two sections neither of which is valid markdown.

/** One independently editable piece of a document. */
export interface DocSection {
  /** Stable within one split of one document — position, not content, so a
   * pending edit stays attached to its section while its text changes. */
  id: string;
  /** What the section is called on screen: the heading text, or the ordered
   * item's first line. Never empty (the opening piece has no boundary line of
   * its own and is labelled by the caller). */
  label: string;
  /** VERBATIM source of this section, boundary line included, trailing blank
   * lines included. Concatenating every section's text reproduces the input. */
  text: string;
  /** False only for the opening piece — the content before the first boundary,
   * which has no heading of its own. */
  hasBoundary: boolean;
}

const HEADING = /^(#{1,6}) +\S/;
const TOP_ORDERED = /^(\d+)\. +\S/;
const FENCE = /^(?:```|~~~)/;

/** The label a boundary line contributes: heading text without its `#`s, or the
 * ordered item's own line. Kept whole rather than truncated — the surface
 * decides how much of it to show, and a truncated label in the data would make
 * two long sibling headings indistinguishable to anything reading this. */
function labelOf(line: string): string {
  const heading = HEADING.exec(line);
  if (heading) return line.slice(heading[1].length).trim();
  return line.trim();
}

function isBoundary(line: string): boolean {
  return HEADING.test(line) || TOP_ORDERED.test(line);
}

/**
 * Cut `text` into sections. Always returns at least one section for a non-empty
 * document; an empty document returns an empty list (there is nothing to paste
 * over, and a phantom empty section would offer an edit affordance for content
 * that does not exist).
 */
export function splitDocSections(text: string): DocSection[] {
  if (text === "") return [];
  // Split keeping the separators so nothing about line endings is invented:
  // the last element is the text after the final newline (often "").
  const lines = text.split("\n");
  const sections: DocSection[] = [];
  let buffer: string[] = [];
  let label = "";
  let hasBoundary = false;
  let inFence = false;

  const flush = () => {
    if (buffer.length === 0) return;
    sections.push({
      id: `s${sections.length}`,
      label,
      // The join undoes the split exactly; the boundary between two sections is
      // the "\n" that ENDS the previous one, so it is re-added here for every
      // section except the last.
      text: buffer.join("\n"),
      hasBoundary,
    });
    buffer = [];
  };

  for (const line of lines) {
    if (FENCE.test(line)) {
      inFence = !inFence;
    } else if (!inFence && isBoundary(line) && buffer.length > 0) {
      // Everything collected so far belongs to the PREVIOUS section, and the
      // newline that separated them belongs to it too.
      buffer.push("");
      flush();
      label = labelOf(line);
      hasBoundary = true;
      buffer.push(line);
      continue;
    }
    if (buffer.length === 0 && isBoundary(line) && !inFence) {
      label = labelOf(line);
      hasBoundary = true;
    }
    buffer.push(line);
  }
  flush();
  return sections;
}

/** Re-join sections into the whole document. Inverse of splitDocSections for
 * any input it produced — see this module's header on why that is a hard
 * requirement and not a nicety. */
export function joinDocSections(sections: readonly { text: string }[]): string {
  return sections.map((s) => s.text).join("");
}
