package main

// api_backup_health.go — the read side of backup_health.go (T-da06).
//
// It is deliberately a THIN read of the durable verdict: the cockpit's
// top-right indicator and the monitor page's backup card both call this one
// endpoint, so the two surfaces cannot disagree about whether the studio still
// has a retreat point. Nothing here re-derives health — see backup_health.go
// for why the watchdog is the only evaluator.

import "net/http"

// HandleGetBackupHealthApiBackupHealthGet serves GET /api/backup-health.
//
// A server whose watchdog was never armed (dependency-free assemblies) reports
// `unknown` rather than 404 or 500: the caller is a permanently mounted
// indicator, and "we cannot tell" is a state it must be able to render.
func (s *apiServer) HandleGetBackupHealthApiBackupHealthGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.backupHealth.report())
}
