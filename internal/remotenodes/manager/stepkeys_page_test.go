// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The page words a step by its key, and a key it has no words for shows in
// English on a Persian page. So every key here has its row in the page's table
// (src/lib/jobs.ts) and a string in each language, and the page keeps no row for
// a key that is gone.
func TestEveryStepKeyHasWordsOnThePage(t *testing.T) {
	const web = "../../../cmd/remote-nodes/web/src/"
	read := func(file string) string {
		t.Helper()
		b, err := os.ReadFile(web + file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	table, english, persian := read("lib/jobs.ts"), read("i18n/en.ts"), read("i18n/fa.ts")

	hubKeys := map[string]bool{}
	for name, key := range stepKeys {
		hubKeys[key] = true
		if !regexp.MustCompile(`(?m)^\s+` + key + `: 'step\.` + key + `',$`).MatchString(table) {
			t.Errorf("step %q (key %s) has no row in STEP_KEYS: add %s: 'step.%s' to cmd/remote-nodes/web/src/lib/jobs.ts", name, key, key, key)
		}
		for lang, text := range map[string]string{"en.ts": english, "fa.ts": persian} {
			if !strings.Contains(text, "'step."+key+"':") {
				t.Errorf("step %q (key %s) has no words in i18n/%s: add 'step.%s'", name, key, lang, key)
			}
		}
	}
	for _, m := range regexp.MustCompile(`(?m)^\s+([a-z_]+): 'step\.`).FindAllStringSubmatch(table, -1) {
		if !hubKeys[m[1]] {
			t.Errorf("the page's STEP_KEYS has a row for %q, which the hub does not send", m[1])
		}
	}
	if len(hubKeys) < 20 {
		t.Fatalf("only %d keys: the table is not what this test expects", len(hubKeys))
	}
}
