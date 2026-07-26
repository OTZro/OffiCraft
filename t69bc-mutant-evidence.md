# T-69bc mutant evidence

Date: 2026-07-26

Single-site mutant: in `telemetryAccount`, changed the runtime mismatch guard
from `!=` to `==`.

Command run while mutated:

```sh
go test -run 'Test(GetMonitoring_RuntimeAccountNeverBorrowsAnotherRuntime|GetMonitoring_RuntimeAccountKeepsCodexAndOwnerGate|GetMonitoring_RuntimeHeartbeatCannotReattributePairedAccount|OutsourceWorker_RuntimeAccountNeverBorrowsAnotherRuntime)$' -count=1
```

Result: red. It failed the Claude member negative test, Codex sentinel,
runtime-only heartbeat counterfactual, and outsourced list/detail negative
test. The source was then restored.

Post-restore verification:

```sh
go test -run 'Test(GetMonitoring_RuntimeAccountNeverBorrowsAnotherRuntime|GetMonitoring_RuntimeAccountKeepsCodexAndOwnerGate|GetMonitoring_RuntimeHeartbeatCannotReattributePairedAccount|OutsourceWorker_RuntimeAccountNeverBorrowsAnotherRuntime|HandleIngestTelemetry_AccountProvenanceStaysPairedWithAccount)$' -count=1
go test ./...
```

Both commands passed in `server/ocserverd`.
