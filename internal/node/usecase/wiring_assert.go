package usecase

import (
	"fmt"
	"strings"
)

// AssertWired verifies that the setter-injected dependencies of a nodeUsecase
// were actually plumbed in during bootstrap. These deps are set via setters
// (SetAuditUsecase, SetWGPeerSource, SetEmbeddedServer) after construction
// because they become available later in the boot sequence. If a setter is
// missed, the field stays nil and its consumer silently degrades — most
// dangerously audit logging for destructive Node Nuke/Wipe ops, which is
// nil-guarded to a no-op. Call this once at boot, after all setters have run,
// to turn that silent gap into a fail-fast startup error.
//
// It type-asserts to the concrete type so it can inspect unexported fields;
// test doubles that are not *nodeUsecase are treated as externally wired and
// pass. Optional deps (xrayDeps, httpClientFactory, ingressUplinkFn and
// routerWANs — the last two only meaningful in router mode) are intentionally
// excluded.
func AssertWired(uc NodeUsecase) error {
	u, ok := uc.(*nodeUsecase)
	if !ok {
		return nil
	}
	var missing []string
	if u.auditUC == nil {
		missing = append(missing, "auditUC (SetAuditUsecase)")
	}
	if u.wgPeerSource == nil {
		missing = append(missing, "wgPeerSource (SetWGPeerSource)")
	}
	if u.embeddedSrv == nil {
		missing = append(missing, "embeddedSrv (SetEmbeddedServer)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("nodeUsecase wiring incomplete: %s", strings.Join(missing, ", "))
	}
	return nil
}
