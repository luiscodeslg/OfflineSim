package main

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

// Summary is the compact result the UI renders, distilled from simc's json2.
type Summary struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Type        string             `json:"type"`
	Created     time.Time          `json:"created"`
	SimcVersion string             `json:"simcVersion"`
	FightStyle  string             `json:"fightStyle"`
	MaxTime     float64            `json:"maxTime"`
	Targets     int                `json:"targets"`
	Iterations  int                `json:"iterations"`
	ElapsedCPU  float64            `json:"elapsedCpu"`
	WallSeconds float64            `json:"wallSeconds"`
	Actors      []ActorResult      `json:"actors"`
	Profilesets []ProfilesetResult `json:"profilesets,omitempty"`
	Notes       []string           `json:"notes,omitempty"`
}

type Dist struct {
	Mean   float64 `json:"mean"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	StdDev float64 `json:"stdDev"`
	Error  float64 `json:"error"` // 95% confidence half-width
}

type ActorResult struct {
	Name         string             `json:"name"`
	Spec         string             `json:"spec"`
	Race         string             `json:"race"`
	Level        int                `json:"level"`
	Role         string             `json:"role"`
	Talents      string             `json:"talents"`
	DPS          Dist               `json:"dps"`
	HPS          float64            `json:"hps"`
	DTPS         float64            `json:"dtps"`
	FightLength  float64            `json:"fightLength"`
	Abilities    []Ability          `json:"abilities"`
	ScaleFactors map[string]float64 `json:"scaleFactors,omitempty"`
	Buffs        []BuffUptime       `json:"buffs,omitempty"`
}

type Ability struct {
	Name     string    `json:"name"`
	SpellID  int       `json:"spellId"`
	School   string    `json:"school"`
	Source   string    `json:"source"` // player or pet name
	DPS      float64   `json:"dps"`
	Share    float64   `json:"share"`
	Casts    float64   `json:"casts"`
	Children []Ability `json:"children,omitempty"`
}

type BuffUptime struct {
	Name   string  `json:"name"`
	Uptime float64 `json:"uptime"`
}

type ProfilesetResult struct {
	Name    string   `json:"name"`
	Mean    float64  `json:"mean"`
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	StdDev  float64  `json:"stdDev"`
	Error   float64  `json:"error"`
	Changes []string `json:"changes,omitempty"`
}

// Loosely typed json2 shapes; simc adds fields over time, so we read only what we need.
type jStat struct {
	Name        string  `json:"name"`
	SpellName   string  `json:"spell_name"`
	ID          int     `json:"id"`
	School      string  `json:"school"`
	Type        string  `json:"type"`
	NumExecutes jMean   `json:"num_executes"`
	CompoundAmt float64 `json:"compound_amount"`
	PortionAps  jMean   `json:"portion_aps"`
	Children    []jStat `json:"children"`
}

type jMean struct {
	Mean float64 `json:"mean"`
}

func (m *jMean) UnmarshalJSON(b []byte) error {
	// Some fields are plain numbers, others {mean: ...}.
	var f float64
	if json.Unmarshal(b, &f) == nil {
		m.Mean = f
		return nil
	}
	var o struct {
		Mean float64 `json:"mean"`
	}
	err := json.Unmarshal(b, &o)
	m.Mean = o.Mean
	return err
}

type jDist struct {
	Mean       float64 `json:"mean"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	StdDev     float64 `json:"std_dev"`
	MeanStdDev float64 `json:"mean_std_dev"`
}

type jActor struct {
	Name           string `json:"name"`
	Race           string `json:"race"`
	Level          int    `json:"level"`
	Role           string `json:"role"`
	Specialization string `json:"specialization"`
	Talents        any    `json:"talents"`
	CollectedData  struct {
		FightLength jDist `json:"fight_length"`
		DPS         jDist `json:"dps"`
		HPS         jDist `json:"hps"`
		DTPS        jDist `json:"dtps"`
	} `json:"collected_data"`
	Stats        []jStat            `json:"stats"`
	Pets         []jActor           `json:"pets"`
	ScaleFactors map[string]float64 `json:"scale_factors"`
	Buffs        []struct {
		Name   string  `json:"name"`
		Uptime float64 `json:"uptime"`
	} `json:"buffs"`
}

type jRoot struct {
	Version string `json:"version"`
	Sim     struct {
		Options struct {
			FightStyle     string  `json:"fight_style"`
			MaxTime        float64 `json:"max_time"`
			Iterations     int     `json:"iterations"`
			DesiredTargets int     `json:"desired_targets"`
		} `json:"options"`
		Players    []jActor `json:"players"`
		Statistics struct {
			ElapsedCPU float64 `json:"elapsed_cpu_seconds"`
		} `json:"statistics"`
		Profilesets struct {
			Results []struct {
				Name       string  `json:"name"`
				Mean       float64 `json:"mean"`
				Min        float64 `json:"min"`
				Max        float64 `json:"max"`
				StdDev     float64 `json:"stddev"`
				MeanError  float64 `json:"mean_error"`
				Iterations int     `json:"iterations"`
			} `json:"results"`
		} `json:"profilesets"`
	} `json:"sim"`
	Notifications []any `json:"notifications"`
}

const ci95 = 1.96

func summarize(raw []byte) (Summary, error) {
	var r jRoot
	if err := json.Unmarshal(raw, &r); err != nil {
		return Summary{}, err
	}
	if len(r.Sim.Players) == 0 {
		return Summary{}, errors.New("no players in the results")
	}
	s := Summary{
		SimcVersion: r.Version,
		FightStyle:  r.Sim.Options.FightStyle,
		MaxTime:     r.Sim.Options.MaxTime,
		Targets:     r.Sim.Options.DesiredTargets,
		Iterations:  r.Sim.Options.Iterations,
		ElapsedCPU:  r.Sim.Statistics.ElapsedCPU,
	}
	for _, n := range r.Notifications {
		if m, ok := n.(string); ok {
			s.Notes = append(s.Notes, m)
		}
	}
	for _, p := range r.Sim.Players {
		cd := p.CollectedData
		a := ActorResult{
			Name: p.Name, Spec: p.Specialization, Race: p.Race, Level: p.Level, Role: p.Role,
			DPS: Dist{Mean: cd.DPS.Mean, Min: cd.DPS.Min, Max: cd.DPS.Max, StdDev: cd.DPS.StdDev,
				Error: cd.DPS.MeanStdDev * ci95},
			HPS: cd.HPS.Mean, DTPS: cd.DTPS.Mean, FightLength: cd.FightLength.Mean,
			ScaleFactors: p.ScaleFactors,
		}
		if t, ok := p.Talents.(string); ok {
			a.Talents = t
		}
		fl := cd.FightLength.Mean
		a.Abilities = append(a.Abilities, abilities(p.Stats, p.Name, fl)...)
		for _, pet := range p.Pets {
			pfl := fl
			a.Abilities = append(a.Abilities, abilities(pet.Stats, pet.Name, pfl)...)
		}
		var total float64
		for _, ab := range a.Abilities {
			total += ab.DPS
		}
		denom := a.DPS.Mean
		if denom <= 0 {
			denom = total
		}
		for i := range a.Abilities {
			setShare(&a.Abilities[i], denom)
		}
		sort.Slice(a.Abilities, func(i, k int) bool { return a.Abilities[i].DPS > a.Abilities[k].DPS })
		for _, b := range p.Buffs {
			if b.Uptime > 0 {
				a.Buffs = append(a.Buffs, BuffUptime{Name: b.Name, Uptime: b.Uptime})
			}
		}
		sort.Slice(a.Buffs, func(i, k int) bool { return a.Buffs[i].Uptime > a.Buffs[k].Uptime })
		s.Actors = append(s.Actors, a)
	}
	for _, ps := range r.Sim.Profilesets.Results {
		e := ps.MeanError
		if e == 0 && ps.Iterations > 0 {
			e = ps.StdDev / math.Sqrt(float64(ps.Iterations)) * ci95
		}
		s.Profilesets = append(s.Profilesets, ProfilesetResult{Name: ps.Name, Mean: ps.Mean, Min: ps.Min,
			Max: ps.Max, StdDev: ps.StdDev, Error: e})
	}
	sort.Slice(s.Profilesets, func(i, k int) bool { return s.Profilesets[i].Mean > s.Profilesets[k].Mean })
	return s, nil
}

func setShare(a *Ability, denom float64) {
	if denom > 0 {
		a.Share = a.DPS / denom
	}
	for i := range a.Children {
		setShare(&a.Children[i], denom)
	}
}

func abilities(stats []jStat, source string, fightLength float64) []Ability {
	var out []Ability
	for _, st := range stats {
		if st.Type != "" && st.Type != "damage" {
			continue
		}
		ab := statToAbility(st, source, fightLength)
		if ab.DPS <= 0 {
			continue
		}
		out = append(out, ab)
	}
	return out
}

func statToAbility(st jStat, source string, fl float64) Ability {
	name := st.SpellName
	if name == "" {
		name = prettyName(st.Name)
	}
	ab := Ability{Name: name, SpellID: st.ID, School: st.School, Source: source, Casts: st.NumExecutes.Mean}
	switch {
	case st.CompoundAmt > 0 && fl > 0:
		ab.DPS = st.CompoundAmt / fl
	default:
		ab.DPS = st.PortionAps.Mean
	}
	for _, c := range st.Children {
		if c.Type != "" && c.Type != "damage" {
			continue
		}
		ch := statToAbility(c, source, fl)
		if ch.DPS > 0 {
			ab.Children = append(ab.Children, ch)
		}
	}
	return ab
}

func prettyName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
