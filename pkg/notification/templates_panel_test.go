package notification

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func TestPanelUpdateMessages(t *testing.T) {
	notes := "## What's new\n\n- Faster **checks** for `node_agent`\n- Fixes\n- More\n- Hidden\n<!-- min_from: v1.0.0 -->"
	e := events.Event{Type: events.EventPanelUpdateAvailable, Payload: events.PanelUpdatePayload{
		Version: "v1.1.0", From: "v1.0.0", Notes: notes, Link: "https://panel.example/secret_path/settings/updates"}}
	m := getTemplate(e.Type)(e)
	if m == nil || m.Title != "Panel v1.1.0 is available" {
		t.Fatalf("message = %+v", m)
	}
	for _, want := range []string{"What's new", "• Faster checks for node\\_agent", "secret\\_path"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, m.Body)
		}
	}
	if strings.Contains(m.Body, "Hidden") || strings.Contains(m.Body, "min_from") {
		t.Errorf("body has more than three lines of notes:\n%s", m.Body)
	}

	back := events.Event{Type: events.EventPanelUpdated, Payload: events.PanelUpdatePayload{Version: "v1.0.0", From: "v1.1.0", Kind: "rollback"}}
	if m := getTemplate(back.Type)(back); !strings.Contains(m.Title, "rolled back to v1.0.0") {
		t.Fatalf("rollback message = %+v", m)
	}
	for _, typ := range []events.EventType{events.EventPanelUpdated, events.EventPanelUpdateFailed, events.EventPanelUpdateSkipped} {
		if getTemplate(typ)(events.Event{Type: typ, Payload: events.SystemAlertPayload{}}) != nil {
			t.Errorf("%s: built a message from another payload", typ)
		}
	}
}
