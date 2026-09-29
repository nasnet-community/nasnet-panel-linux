package sharelink

import (
	"bytes"

	"github.com/lilendian0x00/xray-knife/v11/pkg/subscription"
)

// DecodeSubscription is subscription.Decode minus "#" metadata lines.
// Providers prepend "#profile-title:", "#subscription-userinfo:" and the like,
// and xray-knife v11.2 rejects every line that is not a share link.
func DecodeSubscription(body []byte, opts subscription.DecodeOptions) ([]string, error) {
	// Check the raw size so a truncated read cannot shrink under the limit.
	if opts.MaxBytes > 0 && int64(len(body)) > opts.MaxBytes {
		return nil, subscription.ErrTooLarge
	}
	body = bytes.TrimPrefix(bytes.TrimSpace(body), []byte("\xef\xbb\xbf"))
	kept := make([]byte, 0, len(body))
	for line := range bytes.Lines(body) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("#")) {
			kept = append(kept, line...)
		}
	}
	return subscription.Decode(kept, opts)
}
