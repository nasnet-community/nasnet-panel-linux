// Package runtimestatus keeps what the running hub knows about itself and
// only boot code can see: when it started, how the bot came up, which TLS
// certificate it serves, and the restart-only settings it booted with. The
// settings page reads it to say what is actually in effect.
package runtimestatus

import (
	"crypto/x509"
	"sync"
	"time"
)

type BotState string

const (
	BotDisabled BotState = "disabled" // TELEGRAM_ENABLED=false
	BotStarting BotState = "starting" // connecting, or retrying after an error
	BotRunning  BotState = "running"
	BotRejected BotState = "rejected" // Telegram refused the token; needs a restart
)

type Bot struct {
	State    BotState
	Username string
	Since    time.Time
	Error    string
	// ViaFactory: the bot was built on the outbound-proxy client, so
	// proxy_use_telegram decides per request whether the proxy is used.
	ViaFactory bool
	// Legacy is the host:port of the legacy Telegram proxy, when used.
	Legacy   string
	TokenSet bool
}

// HTTPS is how the panel's listener came up.
type HTTPS struct {
	Mode     string // "files", "acme", "self-signed" or "plain"
	CertFile string
	Error    string
	// Cert returns the certificate being served, or nil.
	Cert func() *x509.Certificate
}

type Registry struct {
	mu              sync.RWMutex
	startedAt       time.Time
	version         string
	port            int
	bot             Bot
	https           HTTPS
	hubListening    bool
	hubPort         int
	reverseCount    func() int
	proxyConfigured func() bool
	bootValues      map[string]string
	adminUser       string
	adminChats      int
	metricsEndpoint bool
}

func New(version string, port int) *Registry {
	return &Registry{startedAt: time.Now(), version: version, port: port, bot: Bot{State: BotDisabled}}
}

func (r *Registry) SetBot(fn func(b *Bot)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.bot)
}

func (r *Registry) SetHTTPS(h HTTPS) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.https = h
}

// Port is the panel's own listening port.
func (r *Registry) Port() int { return r.port }

// ServesTLS reports whether the panel's port speaks TLS; true until boot has
// said otherwise.
func (r *Registry) ServesTLS() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.https.Mode != "plain"
}

// SelfSigned reports whether the panel serves its own self-signed certificate.
func (r *Registry) SelfSigned() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.https.Mode == "self-signed"
}

func (r *Registry) SetReverseHub(listening bool, port int, connected func() int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hubListening, r.hubPort, r.reverseCount = listening, port, connected
}

func (r *Registry) SetProxyConfigured(fn func() bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.proxyConfigured = fn
}

// SetBootValues records the restart-only settings as the process read them.
func (r *Registry) SetBootValues(values map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bootValues = values
}

func (r *Registry) SetAdmin(username string, chats int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adminUser, r.adminChats = username, chats
}

func (r *Registry) SetMetricsEndpoint(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metricsEndpoint = on
}

// Snapshot is a copy that is safe to read without the lock.
type Snapshot struct {
	StartedAt       time.Time
	Version         string
	Port            int
	Bot             Bot
	HTTPS           HTTPS
	HubListening    bool
	HubPort         int
	ReverseCount    int
	ProxyConfigured bool
	BootValues      map[string]string
	AdminUser       string
	AdminChats      int
	MetricsEndpoint bool
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	s := Snapshot{
		StartedAt:       r.startedAt,
		Version:         r.version,
		Port:            r.port,
		Bot:             r.bot,
		HTTPS:           r.https,
		HubListening:    r.hubListening,
		HubPort:         r.hubPort,
		BootValues:      make(map[string]string, len(r.bootValues)),
		AdminUser:       r.adminUser,
		AdminChats:      r.adminChats,
		MetricsEndpoint: r.metricsEndpoint,
	}
	for k, v := range r.bootValues {
		s.BootValues[k] = v
	}
	count, proxy := r.reverseCount, r.proxyConfigured
	r.mu.RUnlock()
	// called outside the lock: they take their owners' locks
	if count != nil {
		s.ReverseCount = count()
	}
	if proxy != nil {
		s.ProxyConfigured = proxy()
	}
	return s
}
