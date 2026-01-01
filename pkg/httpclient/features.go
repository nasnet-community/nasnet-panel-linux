package httpclient

// Feature names every hub-side outbound HTTP consumer must declare so admins
// can enable/disable proxy routing per subsystem. The string value is the
// suffix of the corresponding setting key (proxy_use_<feature>). Keep these
// stable: setting keys depend on them.
type Feature string

const (
	FeatureTelegram   Feature = "telegram"
	FeatureGeofiles   Feature = "geofiles"
	FeatureXrayBinary Feature = "xray_binary"
	FeatureGitHubAPI  Feature = "github_api"
	FeatureWebhooks   Feature = "webhooks"
	FeatureGeoIP      Feature = "geoip"
	FeatureACME       Feature = "acme"
	FeatureWizard     Feature = "wizard"
)

// AllFeatures lists every known feature in display order.
func AllFeatures() []Feature {
	return []Feature{
		FeatureTelegram,
		FeatureGeofiles,
		FeatureXrayBinary,
		FeatureGitHubAPI,
		FeatureWebhooks,
		FeatureGeoIP,
		FeatureACME,
		FeatureWizard,
	}
}
