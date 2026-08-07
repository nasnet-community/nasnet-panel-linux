package usecase

import "os/exec"

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
