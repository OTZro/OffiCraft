// SCRATCH FILE — T-51 baseline measurement only. NOT for commit.
// Seeds one member's gallery perspective at real scale against the ISOLATED
// e2e server (:8791). Run:  node e2e_test/tmp-t51-seed.js
//
// Corpus shape (lopsided long tail, as in the real station):
//   ~2200 attachment-carrying chat messages, one tiny attachment each,
//   ~114 distinct senders (owner + the target member + 112 peers),
//   ~1200 images (1x1 PNG), the rest empty zips.
//   31 senders with exactly 1 file; 59 senders with 1-3 files.
//
// THROTTLED ON PURPOSE: this is a behaviour measurement, not a load test.
const fs = require('fs');
const path = require('path');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const STATE = path.join(__dirname, '.state');
const PASSWORD = fs.readFileSync(path.join(STATE, 'owner.password'), 'utf8').trim();

const PNG_1x1_B64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const ZIP_EMPTY_B64 = 'UEsFBgAAAAAAAAAAAAAAAAAAAAAAAA==';

const TOTAL_FILES = 2200;
const TARGET_IMAGES = 1200;
const N_SENDERS = 114; // owner + target member + 112 peers
const CONCURRENCY = 4; // throttle: never saturate the box

async function j(method, url, token, body) {
  const res = await fetch(BASE + url, {
    method,
    headers: {
      'content-type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  if (!res.ok) throw new Error(`${method} ${url} -> ${res.status} ${await res.text()}`);
  return res.json();
}

// Per-sender file counts: 31x1, 14x2, 14x3 (=59 senders, 101 files), the
// remaining 55 senders carry the rest on a decaying curve summing exactly to
// TOTAL_FILES.
function senderCounts() {
  const counts = [];
  for (let i = 0; i < 31; i++) counts.push(1);
  for (let i = 0; i < 14; i++) counts.push(2);
  for (let i = 0; i < 14; i++) counts.push(3);
  const head = N_SENDERS - counts.length; // 55
  let remaining = TOTAL_FILES - counts.reduce((a, b) => a + b, 0);
  const weights = [];
  for (let i = 0; i < head; i++) weights.push(1 / Math.pow(i + 1, 0.75));
  const wsum = weights.reduce((a, b) => a + b, 0);
  const heavy = weights.map((w) => Math.max(4, Math.round((w / wsum) * remaining)));
  // reconcile to exact total
  let drift = heavy.reduce((a, b) => a + b, 0) - remaining;
  let k = heavy.length - 1;
  while (drift !== 0) {
    if (drift > 0 && heavy[k] > 4) { heavy[k] -= 1; drift -= 1; }
    else if (drift < 0) { heavy[k] += 1; drift += 1; }
    k = k === 0 ? heavy.length - 1 : k - 1;
  }
  return heavy.concat(counts); // heaviest first
}

async function pool(items, worker) {
  let idx = 0;
  const runners = Array.from({ length: CONCURRENCY }, async () => {
    while (idx < items.length) {
      const i = idx++;
      await worker(items[i], i);
    }
  });
  await Promise.all(runners);
}

(async () => {
  const t0 = Date.now();
  const { token: owner } = await j('POST', '/api/login', null, { password: PASSWORD });

  const target = await j('POST', '/api/members', owner, { name: 'T51 Target', kind: 'assistant' });
  const targetTok = (await j('POST', '/api/mint', owner, { member_id: target.id, ttl_days: 1 })).token;
  console.log('target member', target.id);

  const counts = senderCounts();
  console.log('senders', counts.length, 'files', counts.reduce((a, b) => a + b, 0));

  // Sender 0 = owner (posts to target), sender 1 = the target itself (posts to
  // owner), senders 2.. = hired peers (post to target).
  const senders = [{ kind: 'owner', tok: owner, to: target.id }, { kind: 'self', tok: targetTok, to: 'owner' }];
  const peerNames = [];
  for (let i = 2; i < counts.length; i++) peerNames.push(`T51 Peer ${String(i).padStart(3, '0')}`);
  await pool(peerNames, async (name) => {
    const m = await j('POST', '/api/members', owner, { name, kind: 'assistant' });
    const tok = (await j('POST', '/api/mint', owner, { member_id: m.id, ttl_days: 1 })).token;
    senders.push({ kind: 'peer', id: m.id, tok, to: target.id });
  });
  console.log('hired', senders.length, 'senders in', ((Date.now() - t0) / 1000).toFixed(1), 's');

  // Build the flat message plan, interleaved so the gallery order mixes senders.
  const plan = [];
  for (let s = 0; s < counts.length; s++) {
    for (let n = 0; n < counts[s]; n++) plan.push(s);
  }
  // deterministic Fisher-Yates interleave (LCG), so senders are mixed in time
  let seed = 20260902;
  const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648;
  for (let i = plan.length - 1; i > 0; i--) {
    const jx = Math.floor(rnd() * (i + 1));
    [plan[i], plan[jx]] = [plan[jx], plan[i]];
  }
  // Deterministic image/file split: spread the 1200 images evenly over the plan.
  const step = plan.length / TARGET_IMAGES;
  const imageIdx = new Set();
  for (let n = 0; n < TARGET_IMAGES; n++) imageIdx.add(Math.floor(n * step));
  const msgs = plan.map((s, i) => ({ s, i, isImage: imageIdx.has(i) }));
  console.log('planned images', msgs.filter((m) => m.isImage).length, 'of', msgs.length);

  let done = 0;
  await pool(msgs, async (m) => {
    const snd = senders[m.s];
    const att = m.isImage
      ? { data_b64: PNG_1x1_B64, filename: `shot-${m.i}.png`, mime: 'image/png' }
      : { data_b64: ZIP_EMPTY_B64, filename: `bundle-${m.i}.zip`, mime: 'application/zip' };
    await j('POST', '/api/chat', snd.tok, { to: snd.to, body: `t51 seed ${m.i}`, attachments: [att] });
    done++;
    if (done % 200 === 0) console.log('  posted', done, `${((Date.now() - t0) / 1000).toFixed(1)}s`);
    await new Promise((r) => setTimeout(r, 5)); // throttle
  });

  const rows = await j('GET', `/api/chat/attachments?with=${target.id}`, owner);
  const images = rows.filter((r) => r.is_image).length;
  const uniq = new Set(rows.map((r) => r.from)).size;
  console.log(JSON.stringify({ targetId: target.id, rows: rows.length, images, files: rows.length - images, distinctSenders: uniq, seconds: (Date.now() - t0) / 1000 }, null, 2));
  fs.writeFileSync(path.join(STATE, 't51-target.json'), JSON.stringify({ targetId: target.id, targetName: 'T51 Target', rows: rows.length, images, distinctSenders: uniq }, null, 2));
})();
