package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// ActionUninstall dispatches to the appropriate uninstall flow based on deploy mode.
func ActionUninstall(cfg *Config) {
	if cfg.IsSystemd() {
		uninstallSystemd(cfg)
	} else {
		uninstallDocker(cfg)
	}
}

// uninstallDocker removes a Docker-based nasnet-panel deployment.
func uninstallDocker(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Uninstall (Docker)")

	fmt.Printf("  %s\n", ui.StyleError.Bold(true).Render("This will remove all Docker containers, volumes, and images for nasnet-panel."))
	fmt.Printf("  %s\n", ui.StyleDim.Render("The source code and nasnet-tool will NOT be removed."))
	fmt.Println()

	confirmed, err := ui.ConfirmDangerous("Uninstall nasnet-panel Docker deployment?", "UNINSTALL")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// Stop containers and remove volumes
	ui.StepInfo("Stopping and removing containers + volumes...")
	downCmd := cfg.DockerCompose("down", "-v")
	if runErr := downCmd.Run(); runErr != nil {
		ui.StepFail("Failed to remove containers")
	} else {
		ui.StepOk("Containers and volumes removed")
	}

	// Optionally remove Docker images
	fmt.Println()
	removeImages, err := ui.Confirm("Also remove Docker images?")
	if err == nil && removeImages {
		ui.StepInfo("Removing images...")
		// Get image IDs matching nasnet*
		imgOut, imgErr := exec.Command("docker", "images",
			"--filter", "reference=*nasnet*",
			"-q").Output()
		if imgErr == nil && len(strings.TrimSpace(string(imgOut))) > 0 {
			ids := strings.Fields(string(imgOut))
			args := append([]string{"rmi", "-f"}, ids...)
			exec.Command("docker", args...).Run() //nolint:errcheck
		}
		ui.StepOk("Images removed")
	}

	// Remove systemd wrapper service if it exists
	unitFile := fmt.Sprintf("/etc/systemd/system/%s.service", DefaultBackendService)
	if _, statErr := os.Stat(unitFile); statErr == nil {
		ui.StepInfo("Removing systemd wrapper service...")
		exec.Command("sudo", "systemctl", "stop", DefaultBackendService).Run()    //nolint:errcheck
		exec.Command("sudo", "systemctl", "disable", DefaultBackendService).Run() //nolint:errcheck
		exec.Command("sudo", "rm", "-f", unitFile).Run()                          //nolint:errcheck
		exec.Command("sudo", "systemctl", "daemon-reload").Run()                  //nolint:errcheck
		ui.StepOk("Systemd wrapper removed")
	}

	// Optionally remove .env
	fmt.Println()
	removeEnv, err := ui.Confirm("Remove .env configuration file?")
	if err == nil && removeEnv {
		os.Remove(cfg.EnvFile)
		os.Remove(cfg.EnvFile + ".bak")
		ui.StepOk(".env removed")
	}

	fmt.Println()
	ui.StepOk("Docker uninstallation complete")
	ui.StepInfo(fmt.Sprintf("Source code remains at: %s", cfg.ProjectDir))
	ui.PressAnyKey()
}

// uninstallSystemd removes a systemd-based nasnet-panel deployment.
func uninstallSystemd(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Uninstall (Systemd)")

	fmt.Printf("  %s\n", ui.StyleError.Bold(true).Render("This will remove nasnet-panel services and installed files."))
	fmt.Printf("  %s\n", ui.StyleDim.Render("The source code and nasnet-tool will NOT be removed."))
	fmt.Println()

	if _, err := os.Stat(cfg.InstallDir); err == nil {
		ui.StepInfo(fmt.Sprintf("Install directory: %s", cfg.InstallDir))
	}
	fmt.Println()

	confirmed, err := ui.ConfirmDangerous("Uninstall nasnet-panel systemd deployment?", "UNINSTALL")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// Stop, disable, and remove the backend service unit
	unitFile := fmt.Sprintf("/etc/systemd/system/%s.service", DefaultBackendService)
	if _, statErr := os.Stat(unitFile); statErr == nil {
		ui.StepInfo(fmt.Sprintf("Stopping %s...", DefaultBackendService))
		exec.Command("sudo", "systemctl", "stop", DefaultBackendService).Run()    //nolint:errcheck
		exec.Command("sudo", "systemctl", "disable", DefaultBackendService).Run() //nolint:errcheck
		exec.Command("sudo", "rm", "-f", unitFile).Run()                          //nolint:errcheck
		ui.StepOk(fmt.Sprintf("%s removed", DefaultBackendService))
	}

	exec.Command("sudo", "systemctl", "daemon-reload").Run() //nolint:errcheck
	ui.StepOk("Systemd daemon reloaded")

	// Remove install directory
	if _, err := os.Stat(cfg.InstallDir); err == nil {
		ui.StepInfo(fmt.Sprintf("Removing %s...", cfg.InstallDir))
		rmCmd := exec.Command("sudo", "rm", "-rf", cfg.InstallDir)
		if rmErr := rmCmd.Run(); rmErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to remove install directory: %v", rmErr))
		} else {
			ui.StepOk("Install directory removed")
		}
	}

	// Database cleanup
	fmt.Println()
	if cfg.IsSQLite() {
		removeDB, err := ui.Confirm("Also remove the SQLite database file?")
		if err == nil && removeDB {
			if _, statErr := os.Stat(cfg.DBPath); statErr == nil {
				if removeErr := os.Remove(cfg.DBPath); removeErr == nil {
					ui.StepOk("SQLite database removed")
				} else {
					ui.StepFail(fmt.Sprintf("Failed to remove database: %v", removeErr))
				}
			} else {
				ui.StepInfo("No SQLite database file found")
			}
		}
	} else {
		dropDB, err := ui.Confirm("Also drop the PostgreSQL database and user?")
		if err == nil && dropDB {
			ui.StepInfo(fmt.Sprintf("Dropping database '%s'...", cfg.DBName))
			dropDBCmd := exec.Command("sudo", "-u", "postgres", "psql",
				"-c", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", cfg.DBName))
			if dropErr := dropDBCmd.Run(); dropErr != nil {
				ui.StepFail("Failed to drop database")
			} else {
				ui.StepOk("Database dropped")
			}

			if cfg.DBUser != "postgres" {
				ui.StepInfo(fmt.Sprintf("Dropping user '%s'...", cfg.DBUser))
				dropUserCmd := exec.Command("sudo", "-u", "postgres", "psql",
					"-c", fmt.Sprintf("DROP USER IF EXISTS %s;", cfg.DBUser))
				if dropErr := dropUserCmd.Run(); dropErr != nil {
					ui.StepFail("Failed to drop user")
				} else {
					ui.StepOk("User dropped")
				}
			}
		}
	}

	// Optionally remove .env
	fmt.Println()
	removeEnv, err := ui.Confirm("Remove .env configuration file?")
	if err == nil && removeEnv {
		os.Remove(cfg.EnvFile)
		os.Remove(cfg.EnvFile + ".bak")
		ui.StepOk(".env removed")
	}

	fmt.Println()
	ui.StepOk("Systemd uninstallation complete")
	ui.StepInfo(fmt.Sprintf("Source code remains at: %s", cfg.ProjectDir))
	ui.PressAnyKey()
}
