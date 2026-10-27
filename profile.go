package main

import (
	"regexp"
	"strings"
)

// ProfileInfo is what we can read from a /simc addon string or simc profile.
type ProfileInfo struct {
	Name   string     `json:"name"`
	Class  string     `json:"class"`
	Spec   string     `json:"spec"`
	Level  string     `json:"level"`
	Race   string     `json:"race"`
	Region string     `json:"region"`
	Server string     `json:"server"`
	Items  []GearItem `json:"items"`
}

// GearItem is one equipped or bag item from the addon string.
type GearItem struct {
	Index    int    `json:"index"`
	Slot     string `json:"slot"`  // normalized slot, e.g. "finger1"
	Group    string `json:"group"` // slot group: "finger", "trinket" or the slot
	Value    string `json:"value"` // everything after "slot="
	Label    string `json:"label"` // "Item Name (639)"
	Equipped bool   `json:"equipped"`
	Source   string `json:"source"` // "equipped", "bags", "vault"...
}

var (
	classLineRe = regexp.MustCompile(`^(deathknight|demonhunter|druid|evoker|hunter|mage|monk|paladin|priest|rogue|shaman|warlock|warrior)\s*=\s*"?([^"]*)"?\s*$`)
	kvRe        = regexp.MustCompile(`^([a-z_]+)\s*=\s*(.*)$`)
	slotRe      = regexp.MustCompile(`^(head|neck|shoulder|shoulders|back|chest|shirt|tabard|wrist|wrists|hands|waist|legs|feet|finger1|finger2|trinket1|trinket2|main_hand|off_hand)\s*=\s*(.*)$`)
	labelRe     = regexp.MustCompile(`^#\s*(.+\(\d+\))\s*$`)
)

var slotAlias = map[string]string{"shoulders": "shoulder", "wrists": "wrist"}

func slotGroup(slot string) string {
	switch slot {
	case "finger1", "finger2":
		return "finger"
	case "trinket1", "trinket2":
		return "trinket"
	}
	return slot
}

func parseProfile(text string) ProfileInfo {
	p := ProfileInfo{Items: []GearItem{}}
	label := ""
	source := "equipped"
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			low := strings.ToLower(line)
			switch {
			case strings.Contains(low, "gear from bags"):
				source = "bags"
			case strings.Contains(low, "weekly reward"):
				source = "vault"
			case strings.Contains(low, "additional character info"), strings.Contains(low, "checksum"):
				source = "ignore"
			}
			if m := labelRe.FindStringSubmatch(line); m != nil {
				label = m[1]
				continue
			}
			body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if m := slotRe.FindStringSubmatch(body); m != nil && source != "equipped" && source != "ignore" {
				p.addItem(m[1], m[2], label, false, source)
				label = ""
			}
			continue
		}
		if m := classLineRe.FindStringSubmatch(line); m != nil && p.Class == "" {
			p.Class, p.Name = m[1], m[2]
			continue
		}
		if m := slotRe.FindStringSubmatch(line); m != nil {
			p.addItem(m[1], m[2], label, true, "equipped")
			label = ""
			continue
		}
		if m := kvRe.FindStringSubmatch(line); m != nil {
			v := strings.Trim(m[2], `"`)
			switch m[1] {
			case "spec":
				p.Spec = v
			case "level":
				p.Level = v
			case "race":
				p.Race = v
			case "region":
				p.Region = v
			case "server":
				p.Server = v
			}
		}
	}
	return p
}

func (p *ProfileInfo) addItem(slot, value, label string, equipped bool, source string) {
	if s, ok := slotAlias[slot]; ok {
		slot = s
	}
	if slot == "shirt" || slot == "tabard" || strings.TrimSpace(value) == "" {
		return
	}
	if label == "" {
		label = slot
		if m := regexp.MustCompile(`id=(\d+)`).FindStringSubmatch(value); m != nil {
			label = "Item " + m[1]
		}
	}
	p.Items = append(p.Items, GearItem{Index: len(p.Items), Slot: slot, Group: slotGroup(slot),
		Value: value, Label: label, Equipped: equipped, Source: source})
}

// primaryStat guesses the main stat used for stat weights.
func primaryStat(class, spec string) string {
	switch class {
	case "deathknight", "warrior":
		return "strength"
	case "paladin":
		if spec == "holy" {
			return "intellect"
		}
		return "strength"
	case "demonhunter", "rogue", "hunter":
		return "agility"
	case "druid":
		if spec == "feral" || spec == "guardian" {
			return "agility"
		}
	case "monk":
		if spec != "mistweaver" {
			return "agility"
		}
	case "shaman":
		if spec == "enhancement" {
			return "agility"
		}
	}
	return "intellect"
}
