package main

// validate_test.go asserts invariants, not fixed counts.
//
// The previous version hardcoded "every regex failure is silent", and a
// run disproved it. Now the test asserts that the baseline is wrong at
// least once and silent at least once, and logs the measured numbers so
// the article quotes this machine instead of a guess.

import (
	"testing"

	"golang.org/x/exp/ebnf"
)

func TestGrammarBuildsAndMatches(t *testing.T) {
	g, variant, rangeOp, _ := buildGrammar()
	t.Logf("variant=%s rangeOp=%q productions=%d", variant, rangeOp, len(g))
	dumpAPI(g)

	if err := ebnf.Verify(g, "Call"); err != nil {
		t.Fatalf("grammar does not verify: %v", err)
	}
	if v := Validate(g, "Call", "noop()"); !v.OK {
		t.Fatalf("the interpreter cannot match noop():\n%s", diagnostic("noop()", v))
	}
}

func TestConformance(t *testing.T) {
	g, variant, _, _ := buildGrammar()
	bad := 0
	for _, s := range samples {
		v := Validate(g, "Call", s.call)
		if v.OK == s.valid {
			continue
		}
		bad++
		what := "refused a valid call"
		if !s.valid {
			what = "accepted a broken call"
		}
		t.Errorf("#%d %s: %s\n%s", s.n, what, s.call, diagnostic(s.call, v))
	}
	if bad > 0 {
		t.Logf("the build is fine - these are results. variant=%s", variant)
	}
}

func TestBaselines(t *testing.T) {
	var rt, jt tally
	for _, s := range samples {
		rOK, rErr := regexValidate(s.call)
		jOK, jErr := jsonValidate(s.call)
		rt.add(rOK, rErr, s.valid)
		jt.add(jOK, jErr, s.valid)
	}
	t.Logf("regex       %s", rt)
	t.Logf("regex+JSON  %s", jt)

	if rt.wrong == 0 {
		t.Errorf("the regex baseline was never wrong - the sample set is too easy")
	}
	if rt.silent == 0 {
		t.Errorf("the regex baseline was never silently wrong - that is the whole point")
	}
}

func BenchmarkGrammar(b *testing.B) {
	g, _, _, _ := buildGrammar()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Validate(g, "Call", samples[6].call)
	}
}

func BenchmarkRegex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		regexValidate(samples[6].call)
	}
}

func BenchmarkJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		jsonValidate(samples[6].call)
	}
}
