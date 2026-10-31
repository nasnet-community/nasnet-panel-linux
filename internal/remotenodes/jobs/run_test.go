// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStepsMoveThroughTheirStates(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindUpdateAgent, Steps: []string{"a", "b", "c"}})
	steps := func() []Step { return g.stepsOf(t, job.ID) }
	same := func(what string, want ...Step) {
		t.Helper()
		if got := steps(); !slices.Equal(got, want) {
			t.Fatalf("%s: steps = %v, want %v", what, got, want)
		}
	}

	r.Begin("a")
	same("Begin", running("a"), pending("b"), pending("c"))
	r.Detail("a", "38% · 5 of 13 MB")
	same("Detail leaves the state", Step{Name: "a", State: StepRunning, Detail: "38% · 5 of 13 MB"}, pending("b"), pending("c"))
	r.Done("a")
	same("Done leaves the text", Step{Name: "a", State: StepOK, Detail: "38% · 5 of 13 MB"}, pending("b"), pending("c"))
	r.Fail("b", errors.New("it broke"))
	same("Fail", Step{Name: "a", State: StepOK, Detail: "38% · 5 of 13 MB"}, Step{Name: "b", State: StepFailed, Detail: "it broke"}, pending("c"))
	// a retry may begin a step that failed
	r.Begin("b")
	r.Detail("b", "")
	r.Done("b")
	same("a retry", Step{Name: "a", State: StepOK, Detail: "38% · 5 of 13 MB"}, ok("b"), pending("c"))
	r.Set("c", StepRunning, "x")
	r.Set("c", StepFailed, "y")
	same("Set", Step{Name: "a", State: StepOK, Detail: "38% · 5 of 13 MB"}, ok("b"), Step{Name: "c", State: StepFailed, Detail: "y"})
	r.Set("c", StepPending, "")
	same("Set back to pending", Step{Name: "a", State: StepOK, Detail: "38% · 5 of 13 MB"}, ok("b"), pending("c"))
	end(nil)
}

// The key is the page's handle on a step, so it is given when the step is
// made, whether the plan listed it or a call added it, and it stays through
// every change of state.
func TestAStepGetsItsKeyWhenItIsMade(t *testing.T) {
	var asked []string
	g := newRig(t, func(c *Config) {
		c.StepKey = func(name string) string {
			asked = append(asked, name)
			return "key_of_" + strings.ToLower(name)
		}
	})
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"Plan", "Other"}})
	keys := func() []string {
		var out []string
		for _, s := range g.stepsOf(t, job.ID) {
			out = append(out, s.Key)
		}
		return out
	}
	if got, want := keys(), []string{"key_of_plan", "key_of_other"}; !slices.Equal(got, want) {
		t.Fatalf("planned keys = %v, want %v", got, want)
	}
	if job.Steps[0].Key != "key_of_plan" {
		t.Errorf("the job Start returned has no key: %+v", job.Steps)
	}
	r.Begin("Plan")
	r.Detail("Plan", "1%")
	r.Done("Plan")
	r.Set("Late", StepRunning, "x") // one the plan missed
	r.Fail("Late", errors.New("boom"))
	if got, want := keys(), []string{"key_of_plan", "key_of_other", "key_of_late"}; !slices.Equal(got, want) {
		t.Fatalf("keys after the calls = %v, want %v", got, want)
	}
	// asked once for each step made, not for each call about it
	if want := []string{"Plan", "Other", "Late"}; !slices.Equal(asked, want) {
		t.Errorf("StepKey was asked about %v, want %v", asked, want)
	}
	end(nil)
}

// A job that ends carries its keys to the page, in the JSON of every step.
func TestEveryStepIsSentWithItsKey(t *testing.T) {
	for name, hook := range map[string]func(string) string{
		"with a key for it": func(n string) string { return "k_" + n },
		"with no hook":      nil,
	} {
		g := newRig(t, func(c *Config) { c.StepKey = hook })
		job := g.run(t, Spec{Kind: KindInstall, Steps: []string{"a", "b"}}, func(_ context.Context, r *Run) error {
			r.Begin("a")
			r.Done("a")
			r.Fail("c", errors.New("boom"))
			return errors.New("boom")
		})
		b, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		var got struct{ Steps []map[string]any }
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Steps) != 3 {
			t.Fatalf("%s: %s", name, b)
		}
		for _, s := range got.Steps {
			key, there := s["key"].(string)
			if !there {
				t.Errorf("%s: a step is sent with no key field: %v", name, s)
			}
			if hook != nil && key != "k_"+s["name"].(string) {
				t.Errorf("%s: key %q for step %q", name, key, s["name"])
			}
			if hook == nil && key != "" {
				t.Errorf("%s: key %q with no hook", name, key)
			}
		}
	}
}

func TestFailWithNoErrorHasNoText(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindUpdateAgent, Steps: []string{"a"}})
	r.Detail("a", "old")
	r.Fail("a", nil)
	if got := g.stepsOf(t, job.ID); !slices.Equal(got, []Step{{Name: "a", State: StepFailed, Detail: ""}}) {
		t.Fatalf("steps = %v", got)
	}
	end(nil)
}

func TestAStepThePlanMissedIsAddedAtTheEnd(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a", "b"}})
	r.Begin("extra")
	r.Done("other")
	r.Detail("quiet", "just a note")
	r.Set("set", StepFailed, "why")
	want := []Step{pending("a"), pending("b"), running("extra"), ok("other"), {Name: "quiet", State: StepPending, Detail: "just a note"}, {Name: "set", State: StepFailed, Detail: "why"}}
	if got := g.stepsOf(t, job.ID); !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	end(nil)
}

func TestARepeatedOrEmptyStepNameIsLeftOut(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a", "", "a", " ", "b", "a"}})
	if want := []Step{pending("a"), pending("b")}; !slices.Equal(job.Steps, want) {
		t.Fatalf("planned steps = %v, want %v", job.Steps, want)
	}
	r.Begin("")
	r.Done("   ")
	r.Detail("", "x")
	r.Begin("a")
	r.Begin("a")
	if got, want := g.stepsOf(t, job.ID), []Step{running("a"), pending("b")}; !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	end(nil)
}

func TestAStateThatIsNoneOfTheFourIsRefused(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	r.Begin("a")
	for _, bad := range []StepState{"", "done", "OK", "skipped"} {
		r.Set("a", bad, "ignored")
		r.Set("new", bad, "ignored")
	}
	if got, want := g.stepsOf(t, job.ID), []Step{running("a")}; !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want them untouched: %v", got, want)
	}
	if !strings.Contains(g.log.String(), "state it does not have") {
		t.Errorf("a refused state should be logged:\n%s", g.log.String())
	}
	end(nil)
}

// installer.Step's states are strings, so passing them on is a conversion.
func TestSetTakesAnotherPackagesProgressAsItIs(t *testing.T) {
	g := newRig(t)
	type progress struct{ Name, State, Detail string }
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"upload", "agent"}})
	for _, p := range []progress{{"upload", "running", ""}, {"upload", "ok", "18 MB"}, {"agent", "running", ""}, {"agent", "failed", "dial timeout"}} {
		r.Set(p.Name, StepState(p.State), p.Detail)
	}
	if got, want := g.stepsOf(t, job.ID), []Step{{Name: "upload", State: StepOK, Detail: "18 MB"}, {Name: "agent", State: StepFailed, Detail: "dial timeout"}}; !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	end(nil)
}

func TestTheNumberOfStepsIsCapped(t *testing.T) {
	g := newRig(t)
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, fmt.Sprint("planned-", i))
	}
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: names})
	if len(job.Steps) != MaxSteps || MaxSteps != 32 {
		t.Fatalf("%d planned steps, want the cap of 32", len(job.Steps))
	}
	if job.Steps[31].Name != "planned-31" {
		t.Errorf("the plan was cut in the wrong place: %v", job.Steps[31])
	}

	// at the cap a new name is dropped, a known one still works
	r.Begin("one too many")
	r.Detail("planned-3", "still here")
	got := g.stepsOf(t, job.ID)
	if len(got) != MaxSteps || got[3].Detail != "still here" {
		t.Fatalf("%d steps; step 3 = %v", len(got), got[3])
	}
	end(nil)

	// a runaway job that keeps inventing names
	g2 := newRig(t)
	job, r, end = g2.begin(t, Spec{Kind: KindInstall})
	for i := 0; i < 10000; i++ {
		r.Begin(fmt.Sprint("retry ", i))
	}
	if n := len(g2.stepsOf(t, job.ID)); n != MaxSteps {
		t.Fatalf("%d steps after 10000 names", n)
	}
	end(nil)
	if done := settle(t, g2.e, job.ID); len(done.Steps) != MaxSteps {
		t.Fatalf("%d steps at the end", len(done.Steps))
	}
}

func TestTextIsCutAtTheCapsInRunes(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a", "b"}})

	exact := strings.Repeat("é", MaxDetail)
	r.Detail("a", exact)
	if got := g.stepsOf(t, job.ID)[0].Detail; got != exact {
		t.Errorf("text of exactly %d runes was changed: %d runes", MaxDetail, utf8.RuneCountInString(got))
	}
	for name, long := range map[string]string{
		"two-byte":   strings.Repeat("é", 300),
		"three-byte": strings.Repeat("ع", 300),
		"four-byte":  strings.Repeat("😀", 300),
		"just over":  strings.Repeat("x", MaxDetail+1),
	} {
		r.Detail("b", long)
		got := g.stepsOf(t, job.ID)[1].Detail
		if utf8.RuneCountInString(got) != MaxDetail || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
			t.Errorf("%s: detail has %d runes (valid %v), want %d ending in an ellipsis", name, utf8.RuneCountInString(got), utf8.ValidString(got), MaxDetail)
		}
		if want := strings.TrimSuffix(got, "…"); !strings.HasPrefix(long, want) {
			t.Errorf("%s: what is kept is not the start of the text", name)
		}
	}
	r.Fail("b", errors.New(strings.Repeat("ع", 600)))
	if got := g.stepsOf(t, job.ID)[1].Detail; utf8.RuneCountInString(got) != MaxDetail {
		t.Errorf("a failed step's text has %d runes", utf8.RuneCountInString(got))
	}
	r.Set("b", StepOK, strings.Repeat("y", 900))
	if got := g.stepsOf(t, job.ID)[1].Detail; utf8.RuneCountInString(got) != MaxDetail {
		t.Errorf("Set's text has %d runes", utf8.RuneCountInString(got))
	}

	// the job's own error has more room than a step's text
	long := strings.Repeat("😀", 2000)
	end(errors.New(long))
	done := settle(t, g.e, job.ID)
	if n := utf8.RuneCountInString(done.Error); n != MaxError || MaxError != 500 || !strings.HasSuffix(done.Error, "…") || !utf8.ValidString(done.Error) {
		t.Errorf("error has %d runes, want %d", n, MaxError)
	}
	for _, s := range done.Steps {
		if utf8.RuneCountInString(s.Detail) > MaxDetail {
			t.Errorf("step %s has %d runes", s.Name, utf8.RuneCountInString(s.Detail))
		}
	}
}

func TestTextIsTrimmedAndMadeValidUTF8(t *testing.T) {
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}})
	for in, want := range map[string]string{
		"  padded \n": "padded",
		"ab\xffcd":    "ab\uFFFDcd",
		"\n\n":        "",
		"two\nlines":  "two\nlines",
	} {
		r.Detail("a", in)
		if got := g.stepsOf(t, job.ID)[0].Detail; got != want {
			t.Errorf("Detail(%q) = %q, want %q", in, got, want)
		}
	}
	end(errors.New("failed \xc3\x28 here\n"))
	if done := settle(t, g.e, job.ID); done.Error != "failed \uFFFD( here" {
		t.Errorf("error = %q", done.Error)
	}
}

func TestAStepNameIsCappedToo(t *testing.T) {
	g := newRig(t)
	long := strings.Repeat("n", 100)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{long}})
	if name := job.Steps[0].Name; utf8.RuneCountInString(name) != 64 || !strings.HasSuffix(name, "…") {
		t.Fatalf("name = %q", name)
	}
	// the same long name is the same step, however it is spelled out again
	r.Begin(long)
	r.Done(long)
	if got := g.stepsOf(t, job.ID); len(got) != 1 || got[0].State != StepOK {
		t.Fatalf("steps = %v", got)
	}
	end(nil)
}

func TestClip(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want string
	}{
		{"", 5, ""}, {"abc", 5, "abc"}, {"abcde", 5, "abcde"}, {"abcdef", 5, "abcd…"}, {"  abcdef  ", 5, "abcd…"},
		{"ééééé", 5, "ééééé"}, {"éééééé", 5, "éééé…"}, {"abcdef", 1, "…"}, {"ab", 1, "…"}, {"a", 1, "a"},
	} {
		if got := clip(c.in, c.max); got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}
