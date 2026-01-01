package usecase

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// UpdateGeoFiles downloads geoip.dat and geosite.dat for the specified region
// (or custom URLs) and pushes them to the node via the agent, then restarts Xray.
func (u *nodeUsecase) UpdateGeoFiles(ctx context.Context, nodeID uint, region string, customGeoIPURL, customGeoSiteURL string) error {
	log := logger.GetLogger().WithField("node_id", nodeID)

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}

	// Resolve source URLs
	var src geofiles.GeoSource
	if geofiles.Region(region) == geofiles.RegionCustom {
		src = geofiles.GeoSource{
			GeoIPURL:   customGeoIPURL,
			GeoSiteURL: customGeoSiteURL,
		}
	} else {
		var ok bool
		src, ok = geofiles.GetSource(geofiles.Region(region))
		if !ok {
			return fmt.Errorf("unknown region: %s", region)
		}
	}

	var geoipData, geositeData []byte

	// Try embedded files first (currently only Iran has embedded geofiles)
	if embGeoIP, embGeoSite, ok := geofiles.GetEmbeddedGeoFiles(geofiles.Region(region)); ok {
		geoipData = embGeoIP
		geositeData = embGeoSite
		log.WithFields(map[string]any{
			"region":       region,
			"geoip_size":   len(geoipData),
			"geosite_size": len(geositeData),
		}).Info("[UpdateGeoFiles] Using embedded geofiles")
	} else {
		log.WithField("region", region).Info("[UpdateGeoFiles] Downloading geofiles")

		var err error
		var client = (*http.Client)(nil)
		if u.httpFactory != nil {
			client = u.httpFactory.ClientFor(httpclient.FeatureGeofiles, 2*time.Minute)
		}
		geoipData, geositeData, err = geofiles.DownloadGeoFiles(ctx, client, src)
		if err != nil {
			return fmt.Errorf("failed to download geofiles: %w", err)
		}

		log.WithFields(map[string]any{
			"geoip_size":   len(geoipData),
			"geosite_size": len(geositeData),
		}).Info("[UpdateGeoFiles] Download complete, pushing to node")
	}

	// Connect to agent
	client, err := u.getAgentClient(node)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer closeAgentClient(client)

	// Push geoip.dat
	if _, err := client.WriteFile(ctx, "geoip.dat", geoipData, 0644); err != nil {
		return fmt.Errorf("failed to write geoip.dat: %w", err)
	}

	// Push geosite.dat
	if _, err := client.WriteFile(ctx, "geosite.dat", geositeData, 0644); err != nil {
		return fmt.Errorf("failed to write geosite.dat: %w", err)
	}

	log.Info("[UpdateGeoFiles] Files pushed, restarting Xray")

	// Restart Xray to load new geofiles
	if err := client.RestartXray(ctx, false); err != nil {
		return fmt.Errorf("failed to restart xray after geofile update: %w", err)
	}

	log.Info("[UpdateGeoFiles] Geofiles updated successfully")
	return nil
}
