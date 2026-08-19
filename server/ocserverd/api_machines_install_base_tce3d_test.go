package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// T-ce3d — bootstrap-here's OC_BASE comes from the SERVER, not from whoever
// called it.
//
// 🔴 Why this file exists: the base used to be requestBaseURL(r) = scheme://
// r.Host, and mcp.go's loopbackCall synthesises its request with a literal
// `Host: "loopback"`. So every AI-triggered reinstall handed the installer
// OC_BASE=http://loopback — a warden that can never call home. It was not a
// flaky failure, it was structural: the only path that worked was a browser,
// and the cockpit button for it is disabled while the machine is online.
//
// The first-run onboarding path (onboarding.go) has always passed s.selfBase.
// This pins that the button agrees with it, so the two cannot drift into two
// answers again.
func ocwardenInstallBaseServer(t *testing.T) *apiServer {
	t.Helper()
	s := newMachinesTestServer(t)
	s.selfBase = "http://127.0.0.1:7755"
	s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")
	s.ocwardenFS = fstest.MapFS{
		"ocwarden":  {Data: []byte("fake warden — never exec'd")},
		"officraft": {Data: []byte("fake anchor — never exec'd")},
	}
	putTestMember(t, s, Member{
		ID: "m-here", Name: "m-here", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	return s
}

func ocBaseOf(t *testing.T, env []string) string {
	t.Helper()
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "OC_BASE="); ok {
			return v
		}
	}
	t.Fatalf("the child env carries no OC_BASE at all: %v", env)
	return ""
}

func TestBootstrapHereBaseIgnoresTheCallersHost(t *testing.T) {
	// Every one of these is a Host a real caller produces. "loopback" is the
	// one mcp.go writes, and it is the reason this test exists; the others are
	// the browser paths, present so a fix that merely special-cased the string
	// "loopback" would still have to answer for them.
	for _, host := range []string{"loopback", "officraft.example.com", "127.0.0.1:7755", "attacker.example"} {
		t.Run("Host: "+host, func(t *testing.T) {
			s := ocwardenInstallBaseServer(t)
			runs := withRecordedOcwarden(t, 0)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/machines/m-here/bootstrap-here", nil)
			req.Host = host
			s.HandleBootstrapHereApiMachinesMachineIdBootstrapHerePost(rec, req, "m-here")
			if rec.Code != http.StatusOK {
				t.Fatalf("bootstrap-here: %d %s", rec.Code, rec.Body.String())
			}
			if len(*runs) != 1 {
				t.Fatalf("expected exactly one ocwarden invocation, got %d", len(*runs))
			}
			if got := ocBaseOf(t, (*runs)[0].env); got != s.selfBase {
				t.Fatalf("OC_BASE = %q, want the server's own base %q — the caller's Host reached the installer",
					got, s.selfBase)
			}
		})
	}
}

// POSITIVE CONTROL: the paths that legitimately need an EXTERNALLY reachable
// address still take it from the request. A "fix" that routed those through
// selfBase too would hand a remote machine a curl of 127.0.0.1 — this is the
// assertion that says the change was scoped, not global.
func TestRemoteInstallCommandStillUsesTheRequestHost(t *testing.T) {
	if got := buildBootCommand("https://officraft.example.com", "CODE1"); !strings.Contains(got, "https://officraft.example.com/install.sh?code=CODE1") {
		t.Fatalf("the copy-paste installer must carry the externally reachable base, got %q", got)
	}
	r := httptest.NewRequest("GET", "/api/machines", nil)
	r.Host = "officraft.example.com"
	if got := requestBaseURL(r); got != "http://officraft.example.com" {
		t.Fatalf("requestBaseURL must still read the request Host (the remote-install paths need it), got %q", got)
	}
}
