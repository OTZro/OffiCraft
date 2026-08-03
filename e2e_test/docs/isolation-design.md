# e2e destructive-suite isolation design (T-8aa1)

## Problem

Four e2e suites are DESTRUCTIVE full-reset regressions that operate on the real
officraft lifecycle:

- `single_machine_e2e.sh`
- `task_system_e2e.sh`
- `a1_zombie_e2e.sh`
- `cross_machine.sh`

They `launchctl bootout` the real launchd labels, `rm -rf` `~/.officraft`, and
kill agent tmux sessions. Their only pre-T-8aa1 protection against running on a
**live agent-fleet host** was *coincidental*: a seth-m1 hardware whitelist plus an
"`~/.officraft` must be empty" check (`oc_preflight_guards`). If the directory
happened to be empty and the machine happened to match, the suite would — **by
design** — bootout the live `com.officraft.ocwarden`, wipe `~/.officraft`,
and kill live agents (agent suicide / machine offline). `cross_machine.sh` was
worse: its STAGE 1 destructive teardown ran with **only** `OC_CROSS_MACHINE_YES=1`
acked — its isolation gate (`REQUIRE_ISOLATION_CONFIRMED`) sat *after* the
teardown, at STAGE 3. (**Fixed in T-e1dd** — see "Layer 3" below. The ordering
outlived T-8aa1 because the live-fleet guard added then sits before the teardown
and reads as the protection, while the ack it was supposed to reinforce stayed
where it was.)

This host (eva's Mac) is a live fleet host: `com.officraft.ocwarden` registered,
the canonical serve port (`:7755`) held, a live `member-*` session on socket
`officraft`, non-empty `~/.officraft`. Exactly the danger case.

## Two-layer fix

### Layer 1 — live-fleet guard (cheap first line, `oc_lifecycle.sh`)

`oc_detect_live_canonical_fleet` (read-only: `launchctl print` / `lsof` / `tmux
ls`, never a kill) reports any of three signals of a live **canonical** fleet:

1. `launchctl print gui/<uid>/com.officraft.ocwarden` succeeds (warden registered)
2. the canonical serve port (`OC_CANONICAL_SERVE_PORT`, currently `7755`, read
   from `server/ocserverd/config.go`'s `defaultPort` — see `oc_lifecycle.sh`) has
   a live listener
3. a `member-*` / `worker-*` session exists on the canonical tmux socket `officraft`

`oc_live_fleet_guard` reads `OC_NS` for the mode and acts BEFORE any teardown:

- **canonical run** (`OC_NS` empty) + live fleet detected → **`die`** (exit 1)
  before a single destructive action, naming the signals and telling the operator
  to use the default namespace mode.
- **namespace run** (`OC_NS` set) + live fleet detected → **log + continue**
  (coexistence is the whole point — the namespaced run touches none of the
  canonical resources).

It runs first inside `oc_preflight_guards` (single/task/a1) and is called
explicitly at the top of `cross_machine.sh`, before its STAGE 1 teardown.

### Layer 2 — namespace construction isolation (root cure, `oc_resolve_instance`)

The product already supports same-machine multi-instance namespacing end-to-end
(`bin/ocserver --namespace`, `OC_NAMESPACE` → `cli/ocwarden` derivation,
`server` stamps it into `bootstrap-here` / `install.sh`). T-8aa1 makes the e2e
suites **default to an isolated namespace** instead of the canonical instance.

`oc_resolve_instance` (default path) mints a run-scoped namespace `e2e<hex>`
(charset `[a-z0-9-]{1,16}`, the product lock) and derives **every** resource axis
off it, overriding the suite globals:

| axis            | canonical                    | namespaced (default)                 |
|-----------------|------------------------------|--------------------------------------|
| serve port      | 7755 (`OC_CANONICAL_SERVE_PORT`) | a free port in 8800–9699         |
| launchd labels  | `com.officraft.*`          | `com.officraft.*.<ns>`             |
| data root       | `~/.officraft`            | `~/.officraft-<ns>`               |
| tmux socket     | `officraft`               | `officraft-<ns>`                  |
| agent workdir   | `~/.officraft/agents/<id>`| `~/.officraft-<ns>/agents/<id>`   |

The whole lifecycle then operates only on the namespaced siblings:

- `oc_fresh_install` seeds `oc.toml` with `[server].namespace = "<ns>"` + the ns
  port (the installer never clobbers a pre-existing `oc.toml`, so the e2e injects
  the namespace itself — matching `render_oc_toml`), and installs with
  `--namespace <ns> --port <port>`. The server thus boots as the namespaced
  instance, so `bootstrap-here` stamps `OC_NAMESPACE` into the warden install →
  the bootstrapped warden keys its own root/label/socket/tokfile off `<ns>`.
- `oc_teardown_bounded` derives every destructive path from `OC_ROOT` (never the
  hardcoded `~/.officraft`), boots out only the namespaced labels, kills only
  `member-*`/`worker-*` on the namespaced socket, and removes the whole disposable
  `~/.officraft-<ns>` root at the end (guarded on `OC_NS` being set).
- `agent_workdir` carries the ns suffix — load-bearing for `a1_zombie`, whose
  `kill -9` only ever targets the listener anchored to the namespaced workdir, so
  it can never hit a canonical live agent that happens to share an id (e.g. `mira`).

A namespaced run is safe on **any** host, so `oc_preflight_guards` skips the
seth-m1 hardware whitelist (0a) in namespace mode (the empty-state check now runs
against the namespaced `OC_ROOT`, still protecting against a colliding prior run).

### Escape hatch — `OC_E2E_ALLOW_CANONICAL=1`

Setting it makes `oc_resolve_instance` keep the canonical instance
(`OC_NS=""`, port 7755, `com.officraft.*`, `~/.officraft`, socket
`officraft`). Canonical is then still gated by **all** of: the live-fleet guard
(dies if a live fleet is present), the seth-m1 triple hardware whitelist, the
empty-`~/.officraft` state whitelist, and the prod-port refusal. So canonical
is reachable only on a provably clean, whitelisted host with no live fleet.

## cross_machine.sh — why it stays canonical

`cross_machine.sh` relocates an agent to a **second machine**, which installs via
the PUBLIC host's `install.sh` (host-derived `OC_BASE`, gotcha #1). That public
tunnel only ever exposes the **canonical** instance — a namespaced install is
serve-only (no tunnel), so a namespaced local instance cannot drive the remote
leg. `cross_machine.sh` therefore stays canonical (`OC_NS=""`), and its
construction-enforced protection is the **live-fleet guard** placed before STAGE
1: it now `die`s on any host with a live fleet, so the suite can only run on a
clean host — exactly condition (b) in its own header, backed by a hard,
self-checking gate instead of resting on the operator *assert*
(`REQUIRE_ISOLATION_CONFIRMED`) alone. Full namespace isolation of the
cross-machine relocate is out of scope (architecturally tied to the public
tunnel).

### Layer 3 — prod-host guard + one preflight, before anything (T-e1dd)

The live-fleet guard answers "is a fleet **running** here?". That leaves a hole
it cannot close by construction: a **production host whose server is stopped**
(mid-deploy, just booted out, crashed) presents no registered warden, no port
listener and no live session, so the guard passes and the teardown deletes the
real server root. A stopped server is also precisely when someone reaches for a
reset script.

`oc_prod_host_guard` asks the other question — **"is this machine one of the
production stations?"** — and answers it twice, on purpose:

- **identity**: the immutable hardware UUID (`ioreg IOPlatformUUID`) against
  `OC_PROD_HOST_HW_UUIDS`. A *blacklist*, unlike the seth-m1 whitelist in
  `oc_preflight_guards` 0a — this suite is specified to run on a throwaway VM, so
  it cannot demand one specific machine, and 0a's third anchor requires a
  vibe-clicking fleet to be *present*, the opposite of this suite's precondition.
- **residue**: `~/.officraft/server` exists at all. Deliberately coarse.

**Both are kept, and the redundancy is the design — do not remove one as
duplicated.** The blacklist's known weakness is that a production host nobody
added to the list is not covered; that weakness is *accepted* only because
residue backs it up. Residue in turn over-refuses: a throwaway VM that already
ran this suite once looks exactly like a production host to it and must be
rebuilt. That is the accepted price, and the trade is not close — residue failing
costs one rebuilt VM, identity failing alone costs a production database.

**Both questions are also asked about the SECOND machine**
(`oc_prod_host_remote_guard`), in the preflight rather than at STAGE 5b where the
remote wipe happens. This is not symmetry for its own sake: STAGE 5b deletes the
remote host's *entire* `~/.officraft`, more than the local teardown deletes here.
Guarding only the local host leaves the cheaper mistake available — from a
genuinely clean throwaway VM, naming a production station as `SECOND_MACHINE`
passes every local gate. The local bug at least required standing on production;
that one needs only its name typed. The remote residue signal is
`~/.officraft/server` rather than `~/.officraft`, because a relocate target is
*expected* to carry the latter — that is what being onboarded means — and never
the former. An unreachable second machine is a refusal, not a warning: a host
whose identity cannot be established is not thereby safe to wipe.

Both derive their paths from `$HOME`, never from `$SERVER_ROOT`/`$OC_ROOT`:
those are env-overridable, and deriving from them would let `OC_SERVER_ROOT` aim
the guard at an empty directory while the deletion still landed on the real tree.
There is **no ack flag** for either: an unset flag is indistinguishable from a
guard that was never there, which is the failure mode this whole document is
about. The two refusals deliberately read differently — the residue message must
say how to proceed (its likely reader is re-running on their own VM, and a
refusal that only says "no" gets worked around), while the identity message must
never name a way to clear the obstacle, because its reader is standing on a
production station and "delete this and retry" *is* the disaster.

Both guards, both acks, the containment check and the ambient-env strip now run
inside a single sourceable `oc_cross_machine_preflight` **before the first
destructive action**, and `cross_machine.sh`'s hand-rolled STAGE 1 teardown was
replaced by `oc_teardown_bounded` — so this suite's teardown is finally covered
by the same `tests_guard` cases (18/18c/18d/18e) as the others, which is what
made the ordering bug invisible for so long: destructive top-level code cannot
be tested without running it.

## Test strategy

- **Function-level + mock** — `e2e_test/tests_guard/run.sh` (hermetic; a PATH shim
  stubs `launchctl`/`tmux`/`lsof`; no real fleet touched, no teardown exercised).
  Covers: live-warden→die (canonical), no-fleet→pass, live-warden→coexist
  (namespace), each detection signal, empty detection on a clean host, all five
  namespace axes non-canonical, the canonical escape hatch, `agent_workdir`
  ns-awareness, and a tripwire asserting no `launchctl bootout` is ever reached.
  Wired into `bin/ci.sh` as gate (0).
- **Product derivation** — the `OC_NAMESPACE` → root/label/socket/tokfile/agent-home
  contract is covered by `cli/ocwarden`'s own namespace unit tests, run by CI
  step (1) `go test ./...`.
- **Live read-only proof** — on this live host, sourcing the lib and calling
  `oc_live_fleet_guard` in canonical mode `die`s citing all three live signals
  (warden label + the canonical serve port `:7755` + `member-m-f663f3c5de9a`)
  before any teardown; in
  namespace mode it coexists (rc=0). (The destructive suites themselves are NOT
  run here — the safety mandate forbids it.)

## Honest limitations

- The full **namespaced install/bootstrap/teardown** path is not run-verified on a
  live host (safety mandate: the destructive suites must not be executed here). It
  is correct by construction + covered by the product's own namespace unit tests +
  the function-level allocator tests. First real exercise should be on a throwaway
  clean host/VM.
- `cross_machine.sh` remote/relocate remains canonical (see above); its protection
  is the live-fleet guard, not namespace isolation.
