package accesslog

import (
	"testing"
	"time"
)

func TestParser_Parse(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name       string
		line       string
		wantNil    bool
		wantEmail  string
		wantDomain string
		wantPort   int
		wantStatus string
		wantNet    string
	}{
		{
			name:       "standard accepted tcp",
			line:       "2024/01/15 10:30:45 from 192.168.1.100:54321 accepted tcp:www.google.com:443 [vless-in >> direct] email: user_123_a1b2c3d4",
			wantEmail:  "user_123_a1b2c3d4",
			wantDomain: "www.google.com",
			wantPort:   443,
			wantStatus: "accepted",
			wantNet:    "tcp",
		},
		{
			name:       "rejected connection",
			line:       "2024/06/20 08:15:00 from 10.0.0.5:12345 rejected tcp:blocked.example.com:80 [vmess-in >> blackhole] email: user_456_deadbeef",
			wantEmail:  "user_456_deadbeef",
			wantDomain: "blocked.example.com",
			wantPort:   80,
			wantStatus: "rejected",
			wantNet:    "tcp",
		},
		{
			name:       "udp connection",
			line:       "2024/03/10 12:00:00 from 172.16.0.1:9999 accepted udp:dns.google:53 [trojan-in >> freedom-0] email: manual_789_cafe1234",
			wantEmail:  "manual_789_cafe1234",
			wantDomain: "dns.google",
			wantPort:   53,
			wantStatus: "accepted",
			wantNet:    "udp",
		},
		{
			name:    "non-access log line (info)",
			line:    "2024/01/15 10:30:45 [Info] [DNS] google.com got answer: 142.250.80.46",
			wantNil: true,
		},
		{
			name:    "non-access log line (warning)",
			line:    "2024/01/15 10:30:45 [Warning] failed to handler mux client connection",
			wantNil: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantNil: true,
		},
		{
			name:       "IP destination (no domain)",
			line:       "2024/02/01 00:00:00 from 1.2.3.4:1000 accepted tcp:93.184.216.34:443 [vless-in >> direct] email: user_1_abcd1234",
			wantEmail:  "user_1_abcd1234",
			wantDomain: "93.184.216.34",
			wantPort:   443,
			wantStatus: "accepted",
			wantNet:    "tcp",
		},
		{
			name:       "fractional seconds with -> arrow",
			line:       "2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main",
			wantEmail:  "main",
			wantDomain: "8.8.4.4",
			wantPort:   53,
			wantStatus: "accepted",
			wantNet:    "udp",
		},
		{
			name:       "fractional seconds with -> arrow tcp",
			line:       "2026/03/07 17:05:46.572365 from 5.126.80.233:0 accepted tcp:web.whatsapp.com:443 [vless -> wg-out] email: RubikDentistry",
			wantEmail:  "RubikDentistry",
			wantDomain: "web.whatsapp.com",
			wantPort:   443,
			wantStatus: "accepted",
			wantNet:    "tcp",
		},
		{
			name:       "==> arrow (PickRoute API)",
			line:       "2026/03/07 12:00:00.000000 from 10.0.0.1:12345 accepted tcp:example.com:443 [vless ==> direct] email: user1",
			wantEmail:  "user1",
			wantDomain: "example.com",
			wantPort:   443,
			wantStatus: "accepted",
			wantNet:    "tcp",
		},
		{
			name:       "no inbound tag (just outbound)",
			line:       "2026/03/07 12:00:00.000000 from 10.0.0.1:12345 accepted tcp:example.com:80 [direct] email: user2",
			wantEmail:  "user2",
			wantDomain: "example.com",
			wantPort:   80,
			wantStatus: "accepted",
			wantNet:    "tcp",
		},
		{
			name:       "trailing newline (from ReadString)",
			line:       "2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main\n",
			wantEmail:  "main",
			wantDomain: "8.8.4.4",
			wantPort:   53,
			wantStatus: "accepted",
			wantNet:    "udp",
		},
		{
			name:       "accepted to blocked outbound treated as rejected",
			line:       "2026/03/08 12:00:00.123456 from 5.126.80.233:0 accepted tcp:20.33.92.5:443 [vless -> blocked] email: RubikDentistry",
			wantEmail:  "RubikDentistry",
			wantDomain: "20.33.92.5",
			wantPort:   443,
			wantStatus: "rejected",
			wantNet:    "tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := p.Parse(tt.line)
			if tt.wantNil {
				if entry != nil {
					t.Errorf("expected nil, got %+v", entry)
				}
				return
			}
			if entry == nil {
				t.Fatal("expected non-nil entry, got nil")
			}
			if entry.Email != tt.wantEmail {
				t.Errorf("email = %q, want %q", entry.Email, tt.wantEmail)
			}
			if entry.Domain != tt.wantDomain {
				t.Errorf("domain = %q, want %q", entry.Domain, tt.wantDomain)
			}
			if entry.Port != tt.wantPort {
				t.Errorf("port = %d, want %d", entry.Port, tt.wantPort)
			}
			if entry.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", entry.Status, tt.wantStatus)
			}
			if entry.Network != tt.wantNet {
				t.Errorf("network = %q, want %q", entry.Network, tt.wantNet)
			}
		})
	}
}

func TestParser_ParseTimestamp(t *testing.T) {
	p := NewParser()
	entry := p.Parse("2024/03/15 14:30:45 from 1.2.3.4:5678 accepted tcp:example.com:443 [in >> out] email: test@user")
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}

	expected := time.Date(2024, 3, 15, 14, 30, 45, 0, time.UTC)
	if !entry.Timestamp.Equal(expected) {
		t.Errorf("timestamp = %v, want %v", entry.Timestamp, expected)
	}
}

func TestStore_AddAndGet(t *testing.T) {
	s := NewStore(5, 100)

	// Add entries for two users
	for i := 0; i < 10; i++ {
		s.Add(Entry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Email:     "user_a",
			Domain:    "google.com",
			Port:      443,
		})
	}
	s.Add(Entry{
		Timestamp: time.Now(),
		Email:     "user_b",
		Domain:    "example.com",
		Port:      80,
	})

	// user_a should have at most 5 entries (ring buffer limit)
	entriesA := s.GetByEmail("user_a", 100)
	if len(entriesA) != 5 {
		t.Errorf("user_a entries = %d, want 5", len(entriesA))
	}

	// user_b should have 1 entry
	entriesB := s.GetByEmail("user_b", 100)
	if len(entriesB) != 1 {
		t.Errorf("user_b entries = %d, want 1", len(entriesB))
	}

	// Non-existent user
	entriesC := s.GetByEmail("user_c", 100)
	if len(entriesC) != 0 {
		t.Errorf("user_c entries = %d, want 0", len(entriesC))
	}

	// GetAll should return entries across all users, limited
	all := s.GetAll(3)
	if len(all) != 3 {
		t.Errorf("GetAll(3) = %d, want 3", len(all))
	}
}

func TestStore_LRUEviction(t *testing.T) {
	s := NewStore(5, 3) // max 3 emails

	s.Add(Entry{Email: "a", Domain: "a.com", Timestamp: time.Now()})
	s.Add(Entry{Email: "b", Domain: "b.com", Timestamp: time.Now()})
	s.Add(Entry{Email: "c", Domain: "c.com", Timestamp: time.Now()})
	// Adding a 4th should evict "a" (oldest)
	s.Add(Entry{Email: "d", Domain: "d.com", Timestamp: time.Now()})

	if entries := s.GetByEmail("a", 10); len(entries) != 0 {
		t.Error("expected 'a' to be evicted")
	}
	if entries := s.GetByEmail("d", 10); len(entries) != 1 {
		t.Error("expected 'd' to exist")
	}
}
