// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

// NO_NEW_PRIVS can't be undone, so it is set in a child process and read
// back there, not in the process that runs the tests.
func TestNoNewPrivs(t *testing.T) {
	if os.Getenv("BRIDGE_TEST_NNP_CHILD") == "1" {
		if err := noNewPrivs(); err != nil {
			fmt.Println("noNewPrivs:", err)
			os.Exit(3)
		}
		if v, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0); err != nil || v != 1 {
			fmt.Println("PR_GET_NO_NEW_PRIVS after the call:", v, err)
			os.Exit(4)
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNoNewPrivs$")
	cmd.Env = append(os.Environ(), "BRIDGE_TEST_NNP_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
