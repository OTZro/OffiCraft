package main

import (
	"os"
	"strings"
	"testing"
)

// childEnvMap folds a KEY=VALUE slice into a map for assertions.
func childEnvMap(kv []string) map[string]string {
	m := map[string]string{}
	for _, e := range kv {
		if i := strings.IndexByte(e, '='); i > 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

// TestOcwardenChildEnv_AllowlistPassesTheDeliberateRelays is the SENTINEL half:
// an allowlist that dropped the claude/codex stamps or PATH would make every
// one-click install produce a warden that refuses every spawn. The five relays
// exist for stated reasons and must all survive.
func TestOcwardenChildEnv_AllowlistPassesTheDeliberateRelays(t *testing.T) {
	parent := []string{
		"HOME=/Users/seth",
		"PATH=/opt/homebrew/bin:/usr/bin:/bin",
		"OC_CLAUDE_BIN=/Users/seth/.asdf/shims/claude",
		"OC_CODEX_BIN=/opt/homebrew/bin/codex",
		"OC_CLAUDE_CRED_CHECK=0",
	}
	got := childEnvMap(ocwardenChildEnv(parent))
	for _, k := range []string{"HOME", "PATH", "OC_CLAUDE_BIN", "OC_CODEX_BIN", "OC_CLAUDE_CRED_CHECK"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s must be relayed to the ocwarden child (deliberate allowlist entry) — got %v", k, got)
		}
	}
	if got["PATH"] != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Errorf("PATH must be relayed VERBATIM (the serve plist's enriched PATH is the whole point): %q", got["PATH"])
	}
}

// TestOcwardenChildEnv_ScrubsEverythingElse pins the fail-closed half, key by
// key, with the CONSEQUENCE of each leak named. These are not hypothetical:
// every one of them can point an install/teardown at a different instance, or
// make it silently do nothing while reporting success.
func TestOcwardenChildEnv_ScrubsEverythingElse(t *testing.T) {
	parent := []string{
		"HOME=/Users/seth",
		"PATH=/usr/bin",
		// identity — must ride in the token sub only
		"OC_ID=m-someone-else",
		// instance steering — the live-warden class
		"OC_NAMESPACE=other",
		"OC_WARDEN_TOKFILE=/tmp/somebody-elses.tok",
		"OC_AGENT_HOME=/tmp/elsewhere",
		// "install succeeded" while mutating nothing
		"WARDEN_INSTALL_DRYRUN=1",
		// redirect the installed ocagent away from the server's verified copy
		"OC_AGENT_BIN=/tmp/evil-ocagent",
		// wiring the SERVER computes; inheriting it invites a shadowed value
		"OC_BASE=http://attacker.example",
		"OC_TOKEN=stale-token",
		// ordinary unrelated environment
		"AWS_PROFILE=prod",
		"SHELL=/bin/zsh",
		"LANG=en_US.UTF-8",
	}
	got := childEnvMap(ocwardenChildEnv(parent))
	for _, k := range []string{
		"OC_ID", "OC_NAMESPACE", "OC_WARDEN_TOKFILE", "OC_AGENT_HOME",
		"WARDEN_INSTALL_DRYRUN", "OC_AGENT_BIN", "OC_BASE", "OC_TOKEN",
		"AWS_PROFILE", "SHELL", "LANG",
	} {
		if v, ok := got[k]; ok {
			t.Errorf("%s leaked into the ocwarden child env (=%q) — the allowlist must drop everything it does not deliberately relay", k, v)
		}
	}
	if len(got) != 2 {
		t.Errorf("child env = %v, want exactly HOME+PATH from this fixture", got)
	}
}

// TestOcwardenChildEnv_UnsetStaysUnset — an absent key must NOT be synthesised
// as empty. `OC_CLAUDE_BIN=""` and "no OC_CLAUDE_BIN" mean different things to
// the child's resolution order.
func TestOcwardenChildEnv_UnsetStaysUnset(t *testing.T) {
	got := childEnvMap(ocwardenChildEnv([]string{"HOME=/h"}))
	if _, ok := got["PATH"]; ok {
		t.Error("PATH was not in the parent env; the projection must not invent it")
	}
	if _, ok := got["OC_CLAUDE_BIN"]; ok {
		t.Error("OC_CLAUDE_BIN was not in the parent env; the projection must not invent it")
	}
}

// TestOcwardenChildEnv_MalformedEntriesDropped — os.Environ() can carry entries
// with no '=' (and a leading '=' on some platforms); neither may become a key.
func TestOcwardenChildEnv_MalformedEntriesDropped(t *testing.T) {
	out := ocwardenChildEnv([]string{"HOME=/h", "NOEQUALS", "=leading", ""})
	if len(out) != 1 || out[0] != "HOME=/h" {
		t.Errorf("malformed entries must be dropped, got %v", out)
	}
}

// TestOcwardenChildEnv_RealEnvironIsProjected runs the projection over the ACTUAL
// process environment, which is where the defect lived: the old code shipped
// os.Environ() wholesale. Whatever this test process happens to carry, only
// allowlisted keys may come out.
func TestOcwardenChildEnv_RealEnvironIsProjected(t *testing.T) {
	t.Setenv("OC_NAMESPACE", "leak-me")
	t.Setenv("WARDEN_INSTALL_DRYRUN", "1")
	allowed := map[string]bool{}
	for _, k := range ocwardenChildEnvAllowlist {
		allowed[k] = true
	}
	for _, kv := range ocwardenChildEnv(os.Environ()) {
		k := kv[:strings.IndexByte(kv, '=')]
		if !allowed[k] {
			t.Errorf("non-allowlisted key %q escaped into the child env", k)
		}
	}
}
