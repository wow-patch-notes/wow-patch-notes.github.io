package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/exp/maps"
)

func cleanTag(t string) []string {
	var tags []string

	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}

	metaTags := []string{
		"[with weekly restarts]",
		"[with weekly maintenance]",
		"[with weekly realm maintenance]",
		"[with weekly maintenance in each region]",
	}

	for _, mt := range metaTags {
		if strings.HasSuffix(strings.ToLower(t), mt) {
			suffix := t[len(t)-len(mt):]
			tags = append(tags, "__text_prefix "+suffix+" ")

			t = t[:len(t)-len(mt)]
			t = strings.TrimSpace(t)
		}
	}

	if strings.HasSuffix(t, "Tuskar") {
		t += "r"
	}

	t = strings.TrimPrefix(t, "The ")
	t = strings.TrimPrefix(t, "THE ")

	t = strings.TrimSuffix(t, " (Raidfinder)")
	t = strings.TrimSuffix(t, " (Normal)")
	t = strings.TrimSuffix(t, " (Heroic)")
	t = strings.TrimSuffix(t, " (Mythic)")

	t = strings.ReplaceAll(t, "’", "'")

	m := map[string][]string{
		"Aberrus the Shadowed Crucible":             {"Aberrus"},
		"Aberrus, the Shadowed Crucible":            {"Aberrus"},
		"Amirdrassil, the Dreams Hope":              {"Amirdrassil"},
		"Amirdrassil, the Dream's Hope":             {"Amirdrassil"},
		"Alegeth'ar Academy":                        {"Algeth'ar Academy"},
		"Alegeth'ar Acadmey":                        {"Algeth'ar Academy"},
		"Asaad, Caliph of Zephyrs":                  {"Asaad"},
		"Azure Vaults":                              {"Azure Vault"},
		"Blackrook Hold":                            {"Black Rook Hold"},
		"Brakenhide Hollow":                         {"Brackenhide Hollow"},
		"Chargath":                                  {"Chargath, Bane of Scales"},
		"Class":                                     {"Classes"},
		"Discipline, Shadow":                        {"Discipline", "Shadow"},
		"Dungeons and Raids":                        {"Dungeons and Raids"}, // don't split on "and"
		"Dungeons":                                  {"Dungeons and Raids"},
		"Enhancement, Elemental":                    {"Enhancement", "Elemental"},
		"Erkheart Stormvein":                        {"Erkhart Stormvein"},
		"Fyrakk the Blazing":                        {"Fyrakk"},
		"Hackclaw's War-Band":                       {"Hackclaw's Warband"},
		"Kassara":                                   {"Kazzara"},
		"Mining/Herbalism":                          {"Mining", "Herbalism"},
		"Ner'Zul":                                   {"Ner'zhul"},
		"Non-player Characters":                     {"NPCs"},
		"Player versus Player":                      {"PvP"},
		"Players versus Player":                     {"PvP"},
		"Rashok, the Elder":                         {"Rashok"},
		"Sentinel Talondrus":                        {"Sentinel Talondras"},
		"Temple of Ahn'qiraj":                       {"Temple of Ahn'Qiraj"},
		"Thaldrazsus":                               {"Thaldraszus"},
		"Uldaman, Legacy of Tyr":                    {"Uldaman: Legacy of Tyr"},
		"Wrath of the Lich King":                    {"WotLK"},
		"Transmogrification":                        {"Transmog"},
		"Aberrus, the Shadow Crucible 2-piece set":  {},
		"Amirdrassil, the Dream's Hope 4-piece set": {},
		"Dawn of the Infinite: Galakrond's Fall":    {"Dawn of the Infinite", "Galakrond's Fall"},
		"Dawn of the Infinite: Rise of Murozond":    {"Dawn of the Infinite", "Murozond's Rise"},
		"Dawn of the Infinite: Murozond's Rise":     {"Dawn of the Infinite", "Murozond's Rise"},
		"per character":                             {},
		"Every week":                                {},
		"Earn 1 Antique Bronze Bullion a week":      {},
	}
	for k, vs := range m {
		m[strings.ToUpper(k)] = vs
	}

	if replacements, ok := m[t]; ok {
		return append(tags, replacements...)
	} else if a, b, ok := strings.Cut(t, " and "); ok {
		return append(tags, a, b)
	} else if a, b, ok := strings.Cut(t, " AND "); ok {
		return append(tags, a, b)
	} else {
		return append(tags, t)
	}
}

func fixCasing(changes []Change) {
	tagSet := map[string]string{ // upper -> mixed
		"A SINGLE WING":                       "A Single Wing",
		"BLACKSMITHING":                       "Blacksmithing",
		"CHALLENGE COURSE":                    "Challenge Course",
		"CHROMIE TIME":                        "Chromie Time",
		"COOKING":                             "Cooking",
		"CROSS-REALM TRADING":                 "Cross-Realm Trading",
		"EDIT MODE":                           "Edit Mode",
		"FREEHOLD":                            "Freehold",
		"NEW CAMPAIGN CHAPTERS":               "Campaign",
		"NEW RECIPES":                         "New Recipes",
		"NO LIMITS":                           "No Limits",
		"OPTIONS":                             "Options",
		"PING SYSTEM":                         "Ping System",
		"PLAYER VERSUS PLAYER":                "PvP",
		"PUBLIC OBJECTIVES":                   "Public Objectives",
		"RATED SOLO SHUFFLE":                  "Solo Shuffle",
		"REAL TIME CHAT MODERATION":           "Real Time Chat Moderation",
		"REFORGING TYR PART 3":                "Reforging Tyr Part 3",
		"RESEARCHERS UNDER FIRE PUBLIC EVENT": "Researchers Under Fire",
		"SNIFFENSEEKING":                      "Sniffenseeking",
		"TAILORING":                           "Tailoring",
		"TRACKING APPEARANCES":                "Tracking Appearances",
		"ULDAMAN":                             "Uldaman",
		"USER INTERFACE":                      "User Interface",
		"ACCESSIBILITY":                       "Accessibility",
		"VORTEX PINNACLE":                     "Vortex Pinnacle",
		"UPGRADE SYSTEM":                      "Upgrade System",
		"TALENTS UI":                          "Talents UI",
		"MACROS":                              "Macros",
		"MISFIT DRAGONS":                      "Misfit Dragons",
		"GREAT VAULT":                         "Great Vault",
		"REFORGING TYR PART 4":                "Reforging Tyr",
		"AMIRDRASSIL, THE DREAMS HOPE RAID REWARDS": "Amirdrassil",
		"AMIRDRASSIL, THE DREAMS HOPE":              "Amirdrassil",
		"AMIRDRASSIL THE DREAMS HOPE":               "Amirdrassil",
		"REVIVAL CATALYST":                          "Revival Catalyst",
		"DRAGONFLIGHT EPILOGUE QUESTS":              "Quests",
		"ULDUAR TIMEWALKING":                        "Ulduar Timewalking",
		"BATTLEGROUND BLITZ BRAWL":                  "Blitz Brawl",
		"DRAGONFLIGHT SEASON 4":                     "",
		"LOOKING FOR RAID":                          "LFR",
		"DUNGEON CHANGES":                           "Dungeons and Raids",
		"DUNGEONS AND RAIDS":                        "Dungeons and Raids",
		"NORTHREND CUP":                             "Northrend Cup",
		"DAWN OF THE INFINITE: HARD MODE":           "Dawn of the Infinite",
	}

	for _, c := range changes {
		for _, t := range c.Tags {
			uc := strings.ToUpper(t)
			if uc == t {
				continue
			}
			tagSet[uc] = t
		}
	}

	for _, t := range readTags() {
		uc := strings.ToUpper(t)
		if uc == t {
			continue
		}

		tagSet[uc] = t
	}

	var hasInvalidTag bool
	for i, c := range changes {
		for j, t := range c.Tags {
			if mc, ok := tagSet[t]; ok {
				t = mc
				if t == "" {
					changes[i].Tags = append(changes[i].Tags[:j], changes[i].Tags[j+1:]...)
				} else {
					changes[i].Tags[j] = mc
				}
			} else if strings.IndexFunc(t, unicode.IsLetter) >= 0 && t == strings.ToUpper(t) {
				hasInvalidTag = true
				log.Println("Upper case tag: " + t)
			}
		}
	}

	if hasInvalidTag {
		log.Fatal("There is at least one invalid tag")
	}
}

func readTags() []string {
	tags := map[string]struct{}{}

	fnames, err := filepath.Glob("site/*.json")
	if err != nil {
		log.Fatal(err)
	}

	for _, fname := range fnames {
		f, err := os.Open(fname)
		if err != nil {
			log.Fatal(err)
		}

		var old struct {
			Changes []struct {
				Tags []string
			}
		}

		err = json.NewDecoder(f).Decode(&old)
		f.Close()
		if err != nil && err != io.EOF {
			log.Fatal(fname, err)
		}
		for _, c := range old.Changes {
			for _, t := range c.Tags {
				tags[t] = struct{}{}
			}
		}
	}

	return maps.Keys(tags)
}

func checkTags(changes []Change) {
	tagSet := map[string]struct{}{}

	for _, c := range changes {
		for _, t := range c.Tags {
			tagSet[t] = struct{}{}
		}
	}

	tags := maps.Keys(tagSet)
	sort.Strings(tags)

	for i := 1; i < len(tags); i++ {
		if tags[i-0] != "" && strings.HasPrefix(tags[i-1], tags[i-0]) {
			log.Printf("WARN: tags: %q is prefix of %q\n", tags[i-0], tags[i-1])
		}
		if tags[i-1] != "" && strings.HasPrefix(tags[i-0], tags[i-1]) {
			log.Printf("WARN: tags: %q is prefix of %q\n", tags[i-1], tags[i-0])
		}
	}
}
