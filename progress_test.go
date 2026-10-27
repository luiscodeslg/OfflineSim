package main

import (
	"strings"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line  string
		ok    bool
		p     float64
		phase string
	}{
		{"Generating Baseline: Thrallmar [=========>..........] 4512/10000 Mean=123456.7", true, 0.4512, "Generating Baseline: Thrallmar"},
		{"Profilesets (2/4): Combo 2 [=========>..........] 500/1000", true, 0.375, "Profilesets (2/4): Combo 2"},
		{"Generating Scale factors [===>...]", true, -1, "Generating Scale factors"},
		{"Player: Thrallmar orc deathknight frost 90", false, 0, ""},
	}
	for _, c := range cases {
		p, phase, ok := parseProgress(c.line)
		if ok != c.ok || (ok && (abs(p-c.p) > 1e-9 || phase != c.phase)) {
			t.Errorf("%q: got %v %q %v", c.line, p, phase, ok)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestNormalizeAPL(t *testing.T) {
	in := "actions.precombat+=/flask\nactions.precombat+=/food\nactions+=/auto_attack\nactions.cds=pillar\nactions.cds+=/erw\n"
	want := "actions.precombat=flask\nactions.precombat+=/food\nactions=auto_attack\nactions.cds=pillar\nactions.cds+=/erw\n"
	if got := normalizeAPL(in); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTopGearCombos(t *testing.T) {
	prof := `warrior="T"
spec=fury
# Helm (700)
head=,id=1
# Ring A (700)
finger1=,id=10
# Ring B (700)
finger2=,id=11
# Trinket A (700)
trinket1=,id=20
# Trinket B (700)
trinket2=,id=21
### Gear from Bags
# Helm 2 (710)
# head=,id=2
# Ring C (710)
# finger1=,id=12
# Trinket B upgraded (720)
# trinket1=,id=21,bonus_id=5
`
	info := parseProfile(prof)
	var bag []int
	for _, it := range info.Items {
		if !it.Equipped {
			bag = append(bag, it.Index)
		}
	}
	if len(bag) != 3 {
		t.Fatalf("bag items: %d", len(bag))
	}
	combos, err := topGearCombos(info, bag, 0)
	if err != nil {
		t.Fatal(err)
	}
	// head: 2 options; rings A,B,C: 3 pairs (AB equipped) -> 3 options;
	// trinkets A,B,B+: pairs AB (equipped), AB+ (new), BB+ (same item, skipped) -> 2 options.
	if len(combos) != 2*3*2-1 {
		t.Fatalf("combos: %d", len(combos))
	}
	for _, c := range combos {
		j := strings.Join(c.Changes, " ")
		if strings.Contains(j, "trinket1=,id=21") && strings.Contains(j, "trinket2=,id=21") {
			t.Errorf("same trinket twice: %s", j)
		}
	}
	labels := labelChanges(info, []string{"finger1=,id=10", "finger2=,id=12"})
	if len(labels) != 1 || labels[0] != "finger2: Ring C (710)" {
		t.Errorf("labels: %v", labels)
	}
}
