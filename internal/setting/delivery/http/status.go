package http

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/runtimestatus"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/selfsigned"
)

// StatusDeps feed GET /settings/status. Each one is optional.
type StatusDeps struct {
	Runtime *runtimestatus.Registry
	// ActiveReach counts users with an active subscription, and those of
	// them a Telegram broadcast can reach.
	ActiveReach func(ctx context.Context) (total, telegram int64, err error)
	DBSize      func(ctx context.Context) (int64, error)
	// Update is the panel version the last check offered, if any.
	Update func(ctx context.Context) (version string, ok bool)
}

func (h *SettingHandler) SetStatusDeps(d StatusDeps) { h.status = d }

type certStatus struct {
	Issuer  string   `json:"issuer,omitempty"`
	Subject string   `json:"subject,omitempty"`
	Domains []string `json:"domains,omitempty"` // host names, then IP addresses
	// Fingerprint is the SHA-256 fingerprint browsers show, to compare
	// before accepting a self-signed certificate.
	Fingerprint string    `json:"fingerprint"`
	NotAfter    time.Time `json:"not_after"`
	DaysLeft    int       `json:"days_left"`
}

type statusResponse struct {
	Hub struct {
		StartedAt time.Time `json:"started_at"`
		Version   string    `json:"version"`
		Port      int       `json:"port"`
		// Update drives the Settings dot and the header chip.
		Update struct {
			Available bool   `json:"available"`
			Version   string `json:"version,omitempty"`
		} `json:"update"`
	} `json:"hub"`
	// RestartPending lists restart-only keys whose saved value differs from
	// the one this process booted with.
	RestartPending []string `json:"restart_pending"`
	Bot            struct {
		State    string     `json:"state"`
		Username string     `json:"username,omitempty"`
		Since    *time.Time `json:"since,omitempty"`
		Error    string     `json:"error,omitempty"`
		Route    string     `json:"route"` // outbound_proxy, legacy_proxy or direct
		Legacy   string     `json:"legacy,omitempty"`
		TokenSet bool       `json:"token_set"`
	} `json:"bot"`
	HTTPS struct {
		Mode  string      `json:"mode"` // files, acme, self-signed or plain
		Error string      `json:"error,omitempty"`
		Cert  *certStatus `json:"cert,omitempty"`
		// OnDisk is set when the certificate file now holds a different
		// certificate than the one being served (renewed, waiting for a restart).
		OnDisk *certStatus `json:"on_disk,omitempty"`
	} `json:"https"`
	ReverseHub struct {
		Listening bool `json:"listening"`
		Port      int  `json:"port"`
		Connected int  `json:"connected"`
	} `json:"reverse_hub"`
	Backup struct {
		LastRunAt string          `json:"last_run_at,omitempty"`
		Result    json.RawMessage `json:"result,omitempty"`
	} `json:"backup"`
	Maintenance struct {
		Since  string `json:"since,omitempty"`
		Active int64  `json:"active"`
		Reach  int64  `json:"reach"`
	} `json:"maintenance"`
	Admin struct {
		Username string `json:"username"`
		Chats    int    `json:"chats"`
	} `json:"admin"`
	Metrics struct {
		Endpoint bool `json:"endpoint"`
	} `json:"metrics"`
	Database struct {
		SizeBytes int64 `json:"size_bytes,omitempty"`
	} `json:"database"`
}

// Status reports what is in effect right now, as opposed to what is saved:
// the bot's state, the certificate being served, restart-only changes that
// haven't applied yet, and the last scheduled backup.
func (h *SettingHandler) Status(c *gin.Context) {
	ctx := c.Request.Context()
	var out statusResponse
	out.RestartPending = []string{}
	raw, rawErr := h.usecase.RawValues(ctx)
	get := func(key string) string { return raw[key] }

	if rt := h.status.Runtime; rt != nil {
		snap := rt.Snapshot()
		out.Hub.StartedAt, out.Hub.Version, out.Hub.Port = snap.StartedAt, snap.Version, snap.Port
		if h.status.Update != nil {
			out.Hub.Update.Version, out.Hub.Update.Available = h.status.Update(ctx)
		}

		// without a settings read nothing can be compared; claim nothing
		for key, boot := range snap.BootValues {
			if rawErr == nil && get(key) != boot {
				out.RestartPending = append(out.RestartPending, key)
			}
		}
		sort.Strings(out.RestartPending)

		b := snap.Bot
		out.Bot.State, out.Bot.Username, out.Bot.Error, out.Bot.TokenSet = string(b.State), b.Username, b.Error, b.TokenSet
		if !b.Since.IsZero() {
			since := b.Since
			out.Bot.Since = &since
		}
		switch {
		case b.ViaFactory && snap.ProxyConfigured && get("proxy_use_telegram") == "true":
			out.Bot.Route = "outbound_proxy"
		case !b.ViaFactory && b.Legacy != "":
			out.Bot.Route, out.Bot.Legacy = "legacy_proxy", b.Legacy
		default:
			out.Bot.Route = "direct"
		}

		out.HTTPS.Mode, out.HTTPS.Error = snap.HTTPS.Mode, snap.HTTPS.Error
		var served *x509.Certificate
		if snap.HTTPS.Cert != nil {
			served = snap.HTTPS.Cert()
		}
		if served != nil {
			out.HTTPS.Cert = describeCert(served)
		}
		if snap.HTTPS.Mode == "files" && snap.HTTPS.CertFile != "" {
			if disk, err := readCertFile(snap.HTTPS.CertFile); err == nil && served != nil && !disk.NotAfter.Equal(served.NotAfter) {
				out.HTTPS.OnDisk = describeCert(disk)
			}
		}

		out.ReverseHub.Listening, out.ReverseHub.Port, out.ReverseHub.Connected = snap.HubListening, snap.HubPort, snap.ReverseCount
		out.Admin.Username, out.Admin.Chats = snap.AdminUser, snap.AdminChats
		out.Metrics.Endpoint = snap.MetricsEndpoint
	}

	out.Backup.LastRunAt = get("backup_last_run_at")
	if raw := get("backup_last_result"); raw != "" && json.Valid([]byte(raw)) {
		out.Backup.Result = json.RawMessage(raw)
	}
	out.Maintenance.Since = get("maintenance_mode_since")
	if h.status.ActiveReach != nil {
		if total, tg, err := h.status.ActiveReach(ctx); err == nil {
			out.Maintenance.Active, out.Maintenance.Reach = total, tg
		}
	}
	if h.status.DBSize != nil {
		if n, err := h.status.DBSize(ctx); err == nil {
			out.Database.SizeBytes = n
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func describeCert(c *x509.Certificate) *certStatus {
	cs := &certStatus{
		Issuer:      c.Issuer.CommonName,
		Subject:     c.Subject.CommonName,
		Fingerprint: selfsigned.Fingerprint(c),
		NotAfter:    c.NotAfter,
		// floor, so a certificate that ran out hours ago reads as -1, not 0
		DaysLeft: int(math.Floor(time.Until(c.NotAfter).Hours() / 24)),
	}
	// a self-signed certificate's common name is a label, not a host
	if c.Subject.CommonName != "" && !strings.ContainsAny(c.Subject.CommonName, " \t") {
		cs.Domains = append(cs.Domains, c.Subject.CommonName)
	}
	for _, d := range c.DNSNames {
		if d != c.Subject.CommonName {
			cs.Domains = append(cs.Domains, d)
		}
	}
	for _, ip := range c.IPAddresses {
		cs.Domains = append(cs.Domains, ip.String())
	}
	return cs
}

// readCertFile parses the leaf certificate of a PEM file.
func readCertFile(path string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil, os.ErrNotExist
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
	}
}
