package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

func ActionStartServices(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Start Services")

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	if err := ui.Spinner("Starting containers", cfg.DockerCompose("up", "-d")); err != nil {
		ui.StepFail("Failed to start services")
	} else {
		ui.StepOk("All services started")
	}

	fmt.Println()
	viewStatusInline(cfg)
	ui.PressAnyKey()
}

func ActionStopServices(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Stop Services")

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	confirmed, err := ui.Confirm("Stop all services?")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	if err := ui.Spinner("Stopping containers", cfg.DockerCompose("down")); err != nil {
		ui.StepFail("Failed to stop services")
	} else {
		ui.StepOk("All services stopped")
	}
	ui.PressAnyKey()
}

func ActionRestartService(cfg *Config) {
	options := []string{"All Services", "Backend", "Frontend"}
	if !cfg.IsSQLite() {
		options = append(options, "Database")
	}

	choice, err := ui.Menu("Restart Service", options)
	if err != nil || choice < 0 {
		return
	}

	ui.ClearScreen()
	ui.DrawHeader("Restart Service")

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	var target string
	if cfg.IsSQLite() {
		switch choice {
		case 0:
			target = ""
		case 1:
			target = "app"
		case 2:
			target = "web-panel"
		}
	} else {
		switch choice {
		case 0:
			target = ""
		case 1:
			target = "app"
		case 2:
			target = "web-panel"
		case 3:
			target = "postgres"
		}
	}

	label := "all services"
	if target != "" {
		label = target
	}

	var cmd *exec.Cmd
	if target == "" {
		cmd = cfg.DockerCompose("restart")
	} else {
		cmd = cfg.DockerCompose("restart", target)
	}

	if err := ui.Spinner(fmt.Sprintf("Restarting %s", label), cmd); err != nil {
		ui.StepFail(fmt.Sprintf("Failed to restart %s", label))
	} else {
		ui.StepOk(fmt.Sprintf("Restarted %s", label))
	}
	ui.PressAnyKey()
}

func ActionViewStatus(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Service Status")
	viewStatusInline(cfg)
	ui.PressAnyKey()
}

func viewStatusInline(cfg *Config) {
	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		return
	}

	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=nasnet_panel",
		"--format", "{{.Names}}|{{.Status}}|{{.Ports}}").Output()
	if err != nil {
		ui.StepFail("Failed to query Docker: " + err.Error())
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var rows [][]string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		status := parts[1]
		ports := parts[2]

		var coloredStatus string
		if strings.Contains(status, "Up") {
			if strings.Contains(status, "(healthy)") {
				coloredStatus = ui.StyleSuccess.Render("● Healthy")
			} else {
				coloredStatus = ui.StyleWarning.Render("● Up")
			}
		} else {
			coloredStatus = ui.StyleError.Render("● Down")
		}

		// Shorten ports
		shortPorts := ports
		shortPorts = strings.ReplaceAll(shortPorts, "0.0.0.0:", "")
		shortPorts = strings.ReplaceAll(shortPorts, ":::", " ")
		if len(shortPorts) > 30 {
			shortPorts = shortPorts[:30]
		}

		rows = append(rows, []string{name, coloredStatus, shortPorts})
	}

	if len(rows) == 0 {
		ui.StepWarn("No nasnet-panel containers found")
		return
	}

	ui.Table([]string{"Container", "Status", "Ports"}, rows)
}

func ActionViewLogs(cfg *Config) {
	options := []string{"Backend (app)", "Frontend (web-panel)"}
	if !cfg.IsSQLite() {
		options = append(options, "Database (postgres)")
	}
	options = append(options, "All Services")

	choice, err := ui.Menu("View Logs", options)
	if err != nil || choice < 0 {
		return
	}

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	var target string
	if cfg.IsSQLite() {
		switch choice {
		case 0:
			target = "app"
		case 1:
			target = "web-panel"
		case 2:
			target = ""
		}
	} else {
		switch choice {
		case 0:
			target = "app"
		case 1:
			target = "web-panel"
		case 2:
			target = "postgres"
		case 3:
			target = ""
		}
	}

	ui.ClearScreen()
	fmt.Printf("  %s\n\n", ui.StyleDim.Render("Streaming logs... Press Ctrl+C to stop"))

	var cmd *exec.Cmd
	if target == "" {
		cmd = cfg.DockerCompose("logs", "--tail=100", "-f")
	} else {
		cmd = cfg.DockerCompose("logs", "--tail=100", "-f", target)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	ui.PressAnyKey()
}
