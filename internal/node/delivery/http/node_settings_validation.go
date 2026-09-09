package http

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var nodeInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`)
var nodeCountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var nodeDishHostPattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

// Validate only the submitted settings, so a metadata edit does not fail on
// unrelated legacy configuration. Disabled automation can retain its draft.
func (r *updateNodeRequest) validateSettings() error {
	if r.Name != nil {
		*r.Name = strings.TrimSpace(*r.Name)
		if *r.Name == "" || utf8.RuneCountInString(*r.Name) > 255 {
			return fmt.Errorf("name must contain 1 to 255 characters")
		}
	}
	if r.Country != nil {
		*r.Country = strings.ToUpper(strings.TrimSpace(*r.Country))
		if *r.Country != "" && !nodeCountryPattern.MatchString(*r.Country) {
			return fmt.Errorf("country_code must be a two-letter code")
		}
	}
	if r.Datacenter != nil && utf8.RuneCountInString(*r.Datacenter) > 255 {
		return fmt.Errorf("datacenter must contain at most 255 characters")
	}
	if r.LogLevel != nil {
		switch *r.LogLevel {
		case "debug", "info", "warning", "error", "none":
		default:
			return fmt.Errorf("log_level must be debug, info, warning, error, or none")
		}
	}
	if settings := r.BandwidthSettings; settings != nil && settings.Enabled {
		// NASNET derives the shaping device from the router ingress unless an
		// explicit override is set. Empty legacy interfaces remain valid.
		if settings.Interface != "" && !nodeInterfacePattern.MatchString(settings.Interface) {
			return fmt.Errorf("bandwidth interface must be a valid interface name of 1 to 15 characters")
		}
		if settings.InterfaceOverride != "" && !nodeInterfacePattern.MatchString(settings.InterfaceOverride) {
			return fmt.Errorf("bandwidth interface_override must be a valid interface name of 1 to 15 characters")
		}
		if settings.TotalBW < 1 || settings.TotalBW > 100000 {
			return fmt.Errorf("bandwidth total_bw must be between 1 and 100000 Mbps")
		}
	}
	if settings := r.StarlinkSettings; settings != nil && settings.Enabled {
		host, port, err := net.SplitHostPort(settings.DishAddress)
		if err != nil || host == "" || (net.ParseIP(host) == nil && !nodeDishHostPattern.MatchString(host)) {
			return fmt.Errorf("Starlink dish_address must contain a valid host and port")
		}
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return fmt.Errorf("Starlink dish port must be between 1 and 65535")
		}
	}
	if settings := r.CrashRecoverySettings; settings != nil {
		if utf8.RuneCountInString(settings.Command) > 1000 {
			return fmt.Errorf("recovery command must contain at most 1000 characters")
		}
		if settings.Enabled {
			if strings.TrimSpace(settings.Command) == "" {
				return fmt.Errorf("recovery command is required when crash recovery is enabled")
			}
			if settings.CommandTimeout < 5 || settings.CommandTimeout > 300 {
				return fmt.Errorf("recovery command_timeout must be between 5 and 300 seconds")
			}
			if settings.Cooldown < 1 || settings.Cooldown > 1440 {
				return fmt.Errorf("recovery cooldown must be between 1 and 1440 minutes")
			}
			if settings.MaxAttempts < 0 || settings.MaxAttempts > 100 {
				return fmt.Errorf("recovery max_attempts must be between 0 and 100 (0 means unlimited)")
			}
		}
	}
	return nil
}
