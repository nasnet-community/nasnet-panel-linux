package config

import (
	"os"
	"testing"
)

// Router mode is set only by nasnet-tool and never inferred.
func TestRouterMode_OffByDefault(t *testing.T) {
	os.Unsetenv("NASNET_ROUTER_MODE")
	if Load().Router.Enabled {
		t.Error("router mode defaulted to on")
	}
}

func TestRouterMode_OnlyExactlyOneEnablesIt(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE"} {
		t.Setenv("NASNET_ROUTER_MODE", v)
		if !Load().Router.Enabled {
			t.Errorf("NASNET_ROUTER_MODE=%q did not enable router mode", v)
		}
	}
	for _, v := range []string{"", "0", "no", "maybe"} {
		t.Setenv("NASNET_ROUTER_MODE", v)
		if Load().Router.Enabled {
			t.Errorf("NASNET_ROUTER_MODE=%q enabled router mode", v)
		}
	}
}
