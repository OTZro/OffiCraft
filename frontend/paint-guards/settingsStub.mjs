// settingsStub.mjs — T-1500 gate 4c. The server half of the zero-flash guard.
//
// WHY THIS EXISTS instead of `vite preview`:
//
// The flash this ticket fixes is the gap between first paint and the moment
// /api/settings answers. Reproducing it needs a server that (a) actually knows
// the owner's custom theme so the reconcile CONFIRMS the cached picture instead
// of deleting it, and (b) answers over the network so CDP throttling delays it.
//
// `vite preview` + the default build gives neither: the shipped-by-default mock
// adapter answers getServerSettings() from memory in ~0 ms with
// custom_themes: [], so reconcile finds the cached theme unknown and calls
// writePaint(active, []) — which REMOVES the record. Measured on the real build:
// with no auth token the frame probe reads BAD_FRAMES=0 (reconcile never runs,
// because it is gated on hasToken()); add a token and the SAME build reads
// BAD_FRAMES=231/233/249. A guard that green/reds on whether a token happens to
// be in localStorage is not measuring the product.
//
// So the guard builds with VITE_USE_MOCK=false — which is what bin/build ships —
// and points the app at this stub. Every frame number it produces is from the
// authenticated path, against a server that agrees the theme exists.
//
// Usage:
//   node settingsStub.mjs --port 4318 --dist dist --mode ok [--delay 400]
//
// Modes:
//   ok            — the server KNOWS the theme (the happy path this ticket is about)
//   unknown-theme — the server's custom_themes is empty and display_theme is "":
//                   the cached picture is legitimately stale and MUST be dropped.
//                   Not a failure mode — documented behaviour, asserted separately.

import { createServer } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = fileURLToPath(new URL(".", import.meta.url));

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  return i === -1 ? fallback : process.argv[i + 1];
}

const PORT = Number(arg("port", "4318"));
const DIST = resolve(HERE, "..", arg("dist", "dist"));
const MODE = arg("mode", "ok");
const DELAY = Number(arg("delay", "0"));

// The ONE source of truth for the server's theme — the same JSON the jsdom suite
// and the Playwright specs read, so the three layers cannot disagree about what
// the server said. (Plain Node here: it cannot import the TypeScript module.)
const SERVER_THEME = JSON.parse(
  readFileSync(resolve(HERE, "..", "src/lib/paintFixtures.theme.json"), "utf8")
);

/** GET /api/settings → SettingsDTO. Only the fields the cockpit reads are set to
 * anything interesting; the rest are the shipped defaults so no other panel
 * renders an error state that could add a page error the guard would blame on
 * the paint path. */
function settingsDTO() {
  const known = MODE !== "unknown-theme";
  return {
    token_ttl: 86400,
    handover_pct: 50,
    codex_compaction_threshold: 3,
    monitoring_refresh_seconds: 5,
    outsource_max_parallel: 3,
    doc_cap_chars_duty: 1000,
    doc_cap_chars_insight: 10000,
    doc_cap_chars_learning: 10000,
    doc_cap_chars_manual: 10000,
    updater_receive_beta: false,
    updater_auto_update: false,
    org_name: "",
    owner_name: "",
    push_contact_email: "",
    display_theme: known ? SERVER_THEME.id : "",
    display_language: "zh",
    display_wide: false,
    custom_themes: known ? [SERVER_THEME] : [],
    // The real settingsDTO carries no `omitempty`, so this key is ALWAYS on the
    // wire — null once onboarding has finished, which is every installation the
    // owner reloads. Absent and null map to the same `null` in the FE mapper, so
    // this changes no behaviour; it makes the stub's key set byte-equal to the
    // server's, which is what stops "the stub drifted" from being a live theory
    // every time this guard goes red.
    onboarding: null,
  };
}

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".webmanifest": "application/manifest+json",
  ".woff2": "font/woff2",
  ".ico": "image/x-icon",
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function sendJSON(res, status, body) {
  const buf = Buffer.from(JSON.stringify(body));
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": buf.length,
    // No caching anywhere: a cached settings response would silently remove the
    // very round trip this guard is built to measure.
    "cache-control": "no-store",
  });
  res.end(buf);
}

async function serveFile(res, absPath) {
  const body = await readFile(absPath);
  res.writeHead(200, {
    "content-type": MIME[extname(absPath)] ?? "application/octet-stream",
    "content-length": body.length,
    "cache-control": "no-store",
  });
  res.end(body);
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`);
  const path = url.pathname;

  if (path === "/api/settings" && req.method === "GET") {
    if (DELAY > 0) await sleep(DELAY);
    sendJSON(res, 200, settingsDTO());
    return;
  }

  if (path.startsWith("/api/")) {
    // Everything else the cockpit pokes at is out of scope. 404 with the unified
    // error envelope so the client's own error mapping runs (an ApiError the
    // caller catches) rather than an unhandled shape. NEVER 401: a 401 clears the
    // token and bounces the app to the login wall, which would unmount the very
    // page the guard is sampling.
    sendJSON(res, 404, {
      error: { code: "not_found", message: `paint-guard stub: ${path} not stubbed` },
    });
    return;
  }

  // Static, with an SPA fallback to index.html.
  const rel = normalize(path).replace(/^(\.\.[/\\])+/, "").replace(/^\/+/, "");
  const candidate = join(DIST, rel);
  try {
    if (rel && (await stat(candidate)).isFile()) {
      await serveFile(res, candidate);
      return;
    }
  } catch {
    /* fall through to index.html */
  }
  try {
    await serveFile(res, join(DIST, "index.html"));
  } catch {
    res.writeHead(500, { "content-type": "text/plain" });
    res.end(`paint-guard stub: no index.html under ${DIST} — run the build first`);
  }
});

server.listen(PORT, () => {
  console.log(
    `[paint-guard stub] :${PORT} dist=${DIST} mode=${MODE} settingsDelay=${DELAY}ms theme=${SERVER_THEME.id}`
  );
});
