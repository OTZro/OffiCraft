package main

import "strings"

// collectRuntimeCapabilities reports launch readiness without exposing any
// credential material. Claude reuses its existing presence-only probe; Codex
// uses the CLI's non-interactive login status command.
func collectRuntimeCapabilities(env func(string) string, runner CmdRunner,
	claude map[string]any) map[string]any {
	out := map[string]any{}
	claudeBin := resolveClaudeBin(env)
	claudeCap := map[string]any{"installed": claudeBin != ""}
	if version, ok := claude["version"].(string); ok && version != "" {
		claudeCap["version"] = version
	}
	// EVIDENCE ONLY, NEVER A GUESS. The two arms of this function are not
	// symmetric and must not be written as if they were. Codex below actually
	// runs `codex login status`: its false is a MEASUREMENT. Claude has no such
	// probe — it has two presence checks (a credential file, a keychain item),
	// and finding neither means only "we found no evidence here", which is not
	// the same claim as "this host is signed out".
	//
	// Emitting that non-finding as logged_in:false published a guess as a fact,
	// and the server then spent it: reconcile.go's runtimeCapabilityReady
	// rejects an explicit false, so a machine with claude installed but its
	// credential kept somewhere these two checks cannot see had its out-of-box
	// assistant resolved to codex and PERSISTED there — an irreversible choice
	// made on a guess. Omitting the key instead reports unknown (declared in
	// spec/openapi.json: "Absent = not probed, which placement reads as
	// unknown, not as false"), which every reader already handles: the server's
	// two readiness gates both spell it `LoggedIn == nil || *LoggedIn`, and the
	// cockpit's "signed out" badge is keyed on `loggedIn === false`, so unknown
	// stops claiming something we did not measure.
	//
	// The cost is accepted and named: a host that IS genuinely signed out now
	// gets picked for claude and fails at spawn instead — where
	// claude_not_logged_in / claude_bin_unresolved tells the owner what to do,
	// including switching the member to Codex. A visible failure beats an
	// invisible irreversible guess.
	if claudeBin != "" {
		if value, ok := claude["cred_file"].(bool); ok && value {
			claudeCap["logged_in"] = true
		} else if value, ok := claude["keychain"].(bool); ok && value {
			claudeCap["logged_in"] = true
		}
	}
	out["claude"] = claudeCap

	codexBin := resolveCodexBin(env)
	codexCap := map[string]any{"installed": codexBin != ""}
	if codexBin != "" {
		if version, err := runner.Run(codexBin, "--version"); err == nil {
			fields := strings.Fields(version)
			if len(fields) > 0 {
				codexCap["version"] = fields[len(fields)-1]
			}
		}
		_, err := runner.Run(codexBin, "login", "status")
		codexCap["logged_in"] = err == nil
	}
	out["codex"] = codexCap
	return out
}
