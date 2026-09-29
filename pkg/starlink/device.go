package starlink

import (
	"context"
	"fmt"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
)

// DishConfig is the dish's user config. Read-only for us: current firmware
// answers dish_set_config with PermissionDenied from the local network.
type DishConfig struct {
	SnowMeltMode             string `json:"snow_melt_mode"`
	LocationRequestMode      string `json:"location_request_mode"`
	LevelDishMode            string `json:"level_dish_mode"`
	PowerSaveMode            bool   `json:"power_save_mode"`
	PowerSaveStartMinutes    uint32 `json:"power_save_start_minutes"`
	PowerSaveDurationMinutes uint32 `json:"power_save_duration_minutes"`
	SwupdateRebootHour       uint32 `json:"swupdate_reboot_hour"`
	SwupdateThreeDayDeferral bool   `json:"swupdate_three_day_deferral"`
	AssetClass               uint32 `json:"asset_class"`
}

// DeviceInfo is identity + diagnostics + config, fetched far less often than status.
type DeviceInfo struct {
	ID                    string      `json:"id"`
	HardwareVersion       string      `json:"hardware_version"`
	SoftwareVersion       string      `json:"software_version"`
	ManufacturedVersion   string      `json:"manufactured_version"`
	BuildID               string      `json:"build_id"`
	CountryCode           string      `json:"country_code"`
	UTCOffsetS            int32       `json:"utc_offset_s"`
	BootCount             int32       `json:"boot_count"`
	HardwareSelfTest      string      `json:"hardware_self_test"`
	HardwareSelfTestCodes []uint32    `json:"hardware_self_test_codes"`
	Stowed                bool        `json:"stowed"`
	Config                *DishConfig `json:"config,omitempty"`
}

func configFromPB(c *pb.DishConfig) *DishConfig {
	if c == nil {
		return nil
	}
	return &DishConfig{
		SnowMeltMode:             c.SnowMeltMode.String(),
		LocationRequestMode:      c.LocationRequestMode.String(),
		LevelDishMode:            c.LevelDishMode.String(),
		PowerSaveMode:            c.PowerSaveMode,
		PowerSaveStartMinutes:    c.PowerSaveStartMinutes,
		PowerSaveDurationMinutes: c.PowerSaveDurationMinutes,
		SwupdateRebootHour:       c.SwupdateRebootHour,
		SwupdateThreeDayDeferral: c.SwupdateThreeDayDeferralEnabled,
		AssetClass:               c.AssetClass,
	}
}

func selfTestLabel(v uint32) string {
	switch v {
	case 1:
		return "PASSED"
	case 2:
		return "FAILED"
	default:
		return "NO_RESULT"
	}
}

// GetDevice returns device info; diagnostics and config are best-effort.
func (c *Client) GetDevice(ctx context.Context) (*DeviceInfo, error) {
	resp, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_GetDeviceInfo{GetDeviceInfo: &pb.GetDeviceInfoRequest{}},
	})
	if err != nil {
		return nil, fmt.Errorf("get device info: %w", err)
	}
	di := resp.GetGetDeviceInfo().GetDeviceInfo()
	if di == nil {
		return nil, fmt.Errorf("unexpected response type for get_device_info")
	}
	info := &DeviceInfo{
		ID:                  di.Id,
		HardwareVersion:     di.HardwareVersion,
		SoftwareVersion:     di.SoftwareVersion,
		ManufacturedVersion: di.ManufacturedVersion,
		BuildID:             di.BuildId,
		CountryCode:         di.CountryCode,
		UTCOffsetS:          di.UtcOffsetS,
		BootCount:           di.Bootcount,
		HardwareSelfTest:    "NO_RESULT",
	}

	if dr, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_GetDiagnostics{GetDiagnostics: &pb.GetDiagnosticsRequest{}},
	}); err == nil {
		if d := dr.GetDishGetDiagnostics(); d != nil {
			info.HardwareSelfTest = selfTestLabel(d.HardwareSelfTest)
			info.HardwareSelfTestCodes = d.HardwareSelfTestCodes
			info.Stowed = d.Stowed
		}
	}

	if cr, err := c.handle(ctx, &pb.Request{
		Request: &pb.Request_DishGetConfig{DishGetConfig: &pb.DishGetConfigRequest{}},
	}); err == nil {
		info.Config = configFromPB(cr.GetDishGetConfig().GetDishConfig())
	}
	return info, nil
}
