package main

// api_backup_health.go — the read side of backup_health.go (T-da06).
//
// It is deliberately a THIN read of the durable verdict: whatever surface
// renders it — today the 備份健康 block under 設定 › 系統更新與備份 (T-5e71)
// — reads this one endpoint, so no two surfaces can disagree about whether the
// studio still has a retreat point. Nothing here re-derives health — see
// backup_health.go for why the watchdog is the only evaluator.

import "net/http"

// HandleGetBackupHealthApiBackupHealthGet serves GET /api/backup-health.
//
// A server whose watchdog was never armed (dependency-free assemblies) reports
// `unknown` rather than 404 or 500: "we cannot tell" is a state the caller must
// be able to render, and an error would let it read as "nothing to report".
func (s *apiServer) HandleGetBackupHealthApiBackupHealthGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.backupHealth.report())
}
