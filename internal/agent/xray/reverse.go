package xray

import "encoding/json"

// ReverseTagFromConfig reads hub metadata ignored by Xray's JSON parser. It
// keeps API-added users consistent with the users in a full configuration.
func ReverseTagFromConfig(content []byte, inboundTag string) (string, error) {
	var config struct {
		Hub struct {
			ReversePortals map[string]string `json:"reversePortals"`
		} `json:"xcoreHub"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return "", err
	}
	return config.Hub.ReversePortals[inboundTag], nil
}
