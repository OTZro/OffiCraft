package main

import "strings"

// ── shared core / Global Context ───────────────────────────────────────────
//
// "Global Context" is what the cockpit shows under 全域情境 — THREE blocks:
//
//	1. 系統互動   seeds/system_interaction.md   (read-only seed)
//	2. 使用者自訂 /api/global-context           (owner-editable additive block)
//	3. 啟動程序   seeds/boot_sequence*.md       (read-only studio SOP,
//	                                            per-runtime — see below)
//
// Members receive all three through buildBootContext. Workers receive the same
// three blocks grouped at the opening of their boot context, followed by their
// role-specific overlay and assignment. The seed blocks deliberately remain
// byte-for-byte shared; only the overlay describes role differences.
//
// "Byte-for-byte shared" is per RUNTIME, not global: the 啟動程序 block is the
// boot sequence of the runtime the reader is actually running
// (bootSequenceSeedName, assets.go), and staff and outsource on the SAME runtime
// get the same bytes. Handing every worker the Claude seed is not parity — it is
// how a codex worker ended up being told to run its own `ocagent listen`.

// workerGlobalContext returns the worker's view of all THREE 全域情境 blocks,
// grouped in cockpit order (系統互動 → 使用者自訂 → 啟動程序). This is the FIRST
// section of every worker boot context.
//
// The 使用者自訂 block follows the member rule: skipped entirely when the owner
// text is blank, so a worker never sees an empty header.
//
// runtime is the worker's OWN runtime (OutsourceWorker.Runtime). The
// boot-sequence seed is chosen from it through bootSequenceSeedName — the same
// single expression the staff fold uses — because parity with staff means "read
// the seed for the runtime you are running", not "everyone reads the Claude
// one". A codex worker handed boot_sequence.md is told to run a bare `ocagent
// listen` under Monitor, which directly contradicts the codex runtime tail its
// spawn appends.
func (s *apiServer) workerGlobalContext(runtime string) (string, error) {
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		return "", err
	}
	parts := []string{strings.TrimSpace(sys)}

	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts,
			"# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(userCtx.Text))
	}

	boot, err := s.root.readSeedFile(bootSequenceSeedName(runtime))
	if err != nil {
		return "", err
	}
	parts = append(parts, strings.TrimSpace(boot))

	return strings.Join(parts, "\n\n"), nil
}
