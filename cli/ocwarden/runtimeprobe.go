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
	if claudeBin != "" {
		loggedIn := false
		if value, ok := claude["cred_file"].(bool); ok && value {
			loggedIn = true
		}
		if value, ok := claude["keychain"].(bool); ok && value {
			loggedIn = true
		}
		claudeCap["logged_in"] = loggedIn
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
