package cmd

import (
	"strings"
	"testing"
)

// Boot order is load-bearing and easy to break silently.
func TestBootOrder_RollbackThenReconcileThenXray(t *testing.T) {
	src := readSource(t, "root.go")

	rollback := strings.Index(src, "net rollback boot check")
	reconcile := strings.Index(src, "startRouterMode(")
	xray := strings.Index(src, "embeddedSrv.StartLocal(")

	if rollback < 0 || reconcile < 0 || xray < 0 {
		t.Fatalf("markers missing: rollback=%d reconcile=%d xray=%d", rollback, reconcile, xray)
	}
	if rollback > reconcile {
		t.Error("the boot rollback check must run before the network reconciler, " +
			"so a reboot mid-apply reverts before we reconcile onto bad state")
	}
	if reconcile > xray {
		t.Error("the network reconciler must run before the embedded agent starts, " +
			"so xray starts into a correct routing world")
	}
}

// Router mode must never be inferred from the environment.
func TestBootWiring_GatesEverythingOnTheConfigFlag(t *testing.T) {
	src := readSource(t, "root.go")
	if !strings.Contains(src, "cfg.Router.Enabled") {
		t.Error("router mode is not gated on cfg.Router.Enabled")
	}
}

// The usecase has to reach AdminDeps or the routes never register and every
// /network call quietly falls through to the SPA.
func TestBootWiring_NetworkUsecaseReachesTheHTTPLayer(t *testing.T) {
	src := readSource(t, "root.go")
	for _, field := range []string{"NetworkUsecase:", "RouterMode:"} {
		if !strings.Contains(src, field) {
			t.Errorf("AdminDeps is missing %s, so the network API is unreachable", field)
		}
	}
}

// One writer for `table inet nasnet`: two managers would clobber each other.
func TestBootWiring_SharesOneNftManager(t *testing.T) {
	src := readSource(t, "root.go")
	if !strings.Contains(src, "embeddedSrv.NftManager()") {
		t.Error("router mode does not reuse the agent's nft manager")
	}
}
