// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The page words an event by its kind, and a kind it has no words for shows in
// English on a Persian page. So every kind the hub sends is in the page's list
// and has a string in each language, and the page's list has no kind that the
// hub does not send.
func TestEveryEventKindHasWordsOnThePage(t *testing.T) {
	const web = "../../../cmd/remote-nodes/web/src/"
	read := func(file string) string {
		t.Helper()
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// the kinds are the constants of events.go, read from the source so that a new one can't be left out
	var hub []string
	for _, m := range regexp.MustCompile(`(?m)^\s+Event\w+\s+= "([a-z_]+)"$`).FindAllStringSubmatch(read("events.go"), -1) {
		hub = append(hub, m[1])
	}
	if len(hub) < 11 {
		t.Fatalf("only %d kinds in events.go: %v", len(hub), hub)
	}
	list := regexp.MustCompile(`(?s)const EVENT_KEYS = \[(.*?)\]`).FindStringSubmatch(read(web + "lib/node.ts"))
	if list == nil {
		t.Fatal("no EVENT_KEYS in lib/node.ts")
	}
	page := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(list[1], -1) {
		page[m[1]] = true
	}
	english, persian := read(web+"i18n/en.ts"), read(web+"i18n/fa.ts")

	for _, kind := range hub {
		if !page[kind] {
			t.Errorf("event kind %q is not in EVENT_KEYS in cmd/remote-nodes/web/src/lib/node.ts", kind)
		}
		for file, text := range map[string]string{"en.ts": english, "fa.ts": persian} {
			if !strings.Contains(text, "'event."+kind+"':") {
				t.Errorf("event kind %q has no words in i18n/%s: add 'event.%s'", kind, file, kind)
			}
		}
		delete(page, kind)
	}
	for kind := range page {
		t.Errorf("the page's EVENT_KEYS has %q, which the hub does not send", kind)
	}
}
