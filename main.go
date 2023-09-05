package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type Change struct {
	URL     string
	Date    string
	Weekday string
	Tags    []string
	Text    string
}

const userAgent = "wow-patch-notes/1.0 (+https://wow-patch-notes.github.io)"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var stopAfter string

	flag.StringVar(&stopAfter, "stop-after", "",
		"Stop parsing after the article who's URL contains this string.")

	flag.Parse()

	if args := flag.Args(); len(args) > 0 {
		changes := debug(args)

		changes = fixCasing(changes)
		sort.SliceStable(changes, func(i, j int) bool {
			return changes[i].Date > changes[j].Date
		})

		b, _ := json.MarshalIndent(changes, "", "  ")
		fmt.Println(string(b))

		return
	}

	if stopAfter == "" {
		log.Fatal("-stop-after is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	var urls []string

	urls = collectPostURLs(ctx, urls, "https://worldofwarcraft.blizzard.com/en-us/search/blog?k=Patch%20Notes", stopAfter)
	urls = collectPostURLs(ctx, urls, "https://worldofwarcraft.blizzard.com/en-us/search/blog?k=Update%20Notes", stopAfter)

	allChanges := make([]Change, 0, 5000)

	for _, u := range urls {
		allChanges = scrapeURL(ctx, allChanges, u)
	}

	allChanges = fixCasing(allChanges)

	sort.SliceStable(allChanges, func(i, j int) bool {
		return allChanges[i].Date > allChanges[j].Date
	})

	b, _ := json.MarshalIndent(struct {
		Changes []Change
	}{allChanges}, "", "  ")

	fmt.Println(string(b))
}

func collectPostURLs(ctx context.Context, urls []string, indexURL string, stopAfter string) []string {
	req, err := http.NewRequestWithContext(ctx, "GET", indexURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	defer io.Copy(io.Discard, res.Body)

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	doc.Find(".NewsBlog-link").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, _ := s.Attr("href")
		u, err := url.Parse(href)
		if err != nil {
			log.Fatal("invalid URL in href")
		}

		absURL := res.Request.URL.ResolveReference(u).String()

		if !sliceContains(urls, absURL) {
			urls = append(urls, absURL)
		}

		return !strings.Contains(href, stopAfter)
	})

	return urls
}

func sliceContains(xs []string, x string) bool {
	for _, a := range xs {
		if a == x {
			return true
		}
	}
	return false
}

func scrapeURL(ctx context.Context, dest []Change, u string) []Change {
	log.Println(u)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}
	doc.Url = res.Request.URL

	if strings.Contains(u, "/hotfixes-") {
		dest = scrapeHotfixes(dest, doc)
		return dest
	}

	if strings.Contains(u, "/23987385/") {
		dest = scrapeContentUpdate(dest, doc, "#item7", "10.1.7",
			time.Date(2023, 9, 5, 0, 0, 0, 0, time.UTC))
		return dest
	}

	if strings.Contains(u, "/23968772/") {
		dest = scrapeContentUpdate(dest, doc, "#item10", "10.1.5",
			time.Date(2023, 7, 6, 0, 0, 0, 0, time.UTC))
		return dest
	}

	if strings.Contains(u, "/23935248/") {
		dest = scrapeContentUpdate(dest, doc, "#item8", "10.1.0",
			time.Date(2023, 3, 16, 0, 0, 0, 0, time.UTC))
		return dest
	}

	log.Fatalf("Unrecognizable URL: %s", u)

	return dest
}

func fixCasing(changes []Change) []Change {
	tagSet := map[string]string{ // upper -> mixed
		"PLAYER VERSUS PLAYER":                "PvP",
		"RATED SOLO SHUFFLE":                  "Solo Shuffle",
		"OPTIONS":                             "Options",
		"NEW RECIPES":                         "New Recipes",
		"TAILORING":                           "Tailoring",
		"COOKING":                             "Cooking",
		"BLACKSMITHING":                       "Blacksmithing",
		"FREEHOLD":                            "Freehold",
		"VORTEX PINNACLE":                     "Vortex Pinnacle",
		"ULDAMAN":                             "Uldaman",
		"EDIT MODE":                           "Edit Mode",
		"SNIFFENSEEKING":                      "Sniffenseeking",
		"PUBLIC OBJECTIVES":                   "Public Objectives",
		"RESEARCHERS UNDER FIRE PUBLIC EVENT": "Researchers Under Fire",
		"CROSS-REALM TRADING":                 "Cross-Realm Trading",
		"CHROMIE TIME":                        "Chromie Time",
		"TRACKING APPEARANCES":                "Tracking Appearances",
		"CHALLENGE COURSE":                    "Challenge Course",
		"NEW CAMPAIGN CHAPTERS":               "Campaign",
		"A SINGLE WING":                       "A Single Wing",
		"NO LIMITS":                           "No Limits",
		"REFORGING TYR PART 3":                "Reforging Tyr Part 3",
		"PING SYSTEM":                         "Ping System",
		"REAL TIME CHAT MODERATION":           "Real Time Chat Moderation",
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

		json.NewDecoder(f).Decode(&old)
		for _, c := range old.Changes {
			for _, t := range c.Tags {
				uc := strings.ToUpper(t)
				if uc == t {
					continue
				}
				tagSet[uc] = t
			}
		}
	}

	var hasInvalidTag bool
	for i, c := range changes {
		for j, t := range c.Tags {
			if mc, ok := tagSet[t]; ok {
				t = mc
				changes[i].Tags[j] = mc
			}

			if strings.IndexFunc(t, unicode.IsLetter) >= 0 && t == strings.ToUpper(t) {
				hasInvalidTag = true
				log.Println("Upper case tag: " + t)
			}
		}
	}

	if hasInvalidTag {
		log.Fatal("There is at least one invalid tag")
	}

	return changes
}

func scrapeContentUpdate(dest []Change, doc *goquery.Document, firstHeader, version string, date time.Time) []Change {
	changeSets := doc.Find(".Blog .detail " + firstHeader)

	if len(changeSets.Nodes) == 0 {
		log.Fatal("missing .Blog .detail " + firstHeader)
	}
	if len(changeSets.Nodes) > 1 {
		log.Fatal("multiple .Blog .detail " + firstHeader)
	}

	header := changeSets.Nodes[0]
	root := header.Parent
	for header.PrevSibling != nil {
		header.Parent.RemoveChild(header.PrevSibling)
	}

	return scrapeHTML(dest, root, doc, date, []string{version})
}

func scrapeHotfixes(dest []Change, doc *goquery.Document) []Change {
	changeSets := doc.Find(".Blog .detail")

	if len(changeSets.Nodes) == 0 {
		log.Fatal("missing .Blog .detail")
	}
	if len(changeSets.Nodes) > 1 {
		log.Fatal("multiple .Blog .detail")
	}

	return scrapeHTML(dest, changeSets.Nodes[0], doc, time.Time{}, nil)
}

func scrapeHTML(dest []Change, root *html.Node, doc *goquery.Document, date time.Time, tags []string) []Change {
	tree := buildTree(root)

	var uStr string
	if srcURL := doc.Url; srcURL != nil {
		u := new(url.URL)
		*u = *srcURL
		u.Path = filepath.Dir(u.Path)
		uStr = u.String()
	}

	var category string
	for _, n := range tree.Children {
		if !date.IsZero() && category != "" {
			switch n.Type {
			case TypeTag, TypeChange, TypeUnclassified:
				dest = collectChanges(dest, n, append(tags, cleanTag(category)...), date, uStr)
				category = ""
			default:
				log.Fatalf("unexpected %s; want one of 'tag', 'change', 'unclassified'", n.Type.String())
			}
		}

		switch n.Type {
		case TypeDate:
			var err error
			date, err = time.Parse("January 2, 2006", n.Text)
			if err != nil {
				panic(fmt.Sprintf("date miss-classified: %s: %v", n.Text, err))
			}
		case TypeTag:
			if !date.IsZero() {
				category = n.Text
			}
		}
	}

	return dest
}

func buildTree(root *html.Node) *Tree {
	tree := &Tree{
		Children: CollectTexts(root),
	}

	tree.Prune(true)
	tree.Walk(func(n *Tree) {
		if n.Text != "" {
			n.Text = strings.TrimSpace(n.Text)
		}
	})
	tree.Prune(false)
	tree.Walk(func(n *Tree) {
		n.Type = Classify(n)
	})

	return tree
}

func collectChanges(dest []Change, tree *Tree, tags []string, date time.Time, srcURL string) []Change {
	return append(dest, flattenChanges(tree, tags, date, srcURL)...)
}

func flattenChanges(root *Tree, tags []string, date time.Time, srcURL string) []Change {
	for _, t := range tags {
		if strings.Contains(t, "WotLK") {
			return nil
		}
		if strings.Contains(t, "WoW Classic Hardcore") {
			return nil
		}
	}

	var changes []Change

	addChange := func(n *Tree, tags []string) {
		t := make([]string, len(tags))
		copy(t, tags)

		changes = append(changes, Change{
			Date:    date.Format(time.DateOnly),
			Weekday: date.Weekday().String(),
			URL:     srcURL,
			Tags:    t,
			Text:    n.Text,
		})
	}

	switch root.Type {
	case TypeTag:
		tags = append(tags, cleanTag(root.Text)...)
	case TypeChange:
		addChange(root, tags)
	}

	for _, n := range root.Children {
		switch n.Type {
		case TypeTag:
			tags = append(tags, cleanTag(n.Text)...)
		case TypeUnclassified:
			changes = append(changes, flattenChanges(n, tags, date, srcURL)...)
		case TypeChange:
			addChange(n, tags)
		default:
			log.Fatalf("unexpected %s; want one of 'tag', 'change', 'unclassified'", n.Type.String())
		}
	}

	return changes
}

func cleanTag(t string) []string {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}

	t = strings.NewReplacer(
		"’", "'",
		"Alegeth'ar Academy", "Algeth'ar Academy",
		"Alegeth'ar Acadmey", "Algeth'ar Academy",
		"Azure Vaults", "Azure Vault",
		"Brakenhide Hollow", "Brackenhide Hollow",
		"Erkheart Stormvein", "Erkhart Stormvein",
		"Ner'Zul", "Ner'zhul",
		"Thaldrazsus", "Thaldraszus",
		"Player versus Player", "PvP",
		"Wrath of the Lich King", "WotLK",
		"Aberrus, the Shadowed Crucible", "Aberrus",
		"Aberrus the Shadowed Crucible", "Aberrus",
		"Kassara", "Kazzara",
	).Replace(t)

	if strings.HasSuffix(t, "Tuskar") {
		t += "r"
	}

	t = strings.TrimPrefix(t, "The ")
	t = strings.TrimPrefix(t, "THE ")

	t = strings.TrimSuffix(t, " (Raidfinder)")
	t = strings.TrimSuffix(t, " (Normal)")
	t = strings.TrimSuffix(t, " (Heroic)")
	t = strings.TrimSuffix(t, " (Mythic)")

	if t == "Discipline, Shadow" {
		return []string{"Discipline", "Shadow"}
	}
	if t == "Enhancement, Elemental" {
		return []string{"Enhancement", "Elemental"}
	}

	return []string{t}
}

func debug(args []string) []Change {
	fname := args[0]

	f, err := os.Open(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		log.Fatal(err)
	}
	dest := make([]Change, 0, 5000)

	if len(args) > 1 {
		firstHeader := args[1]
		versionTag := args[2]
		return scrapeContentUpdate(dest, doc, firstHeader, versionTag, time.Now())
	} else {
		return scrapeHotfixes(dest, doc)
	}
}
