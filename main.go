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
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
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

	if stopAfter == "" {
		log.Fatal("-stop-after is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://worldofwarcraft.blizzard.com/en-us/search/blog?k=Update%20Notes", nil)
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

	var urls []string

	doc.Find(".NewsBlog-link").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, _ := s.Attr("href")
		u, err := url.Parse(href)
		if err != nil {
			log.Fatal("invalid URL in href")
		}
		urls = append(urls, res.Request.URL.ResolveReference(u).String())

		return !strings.Contains(href, stopAfter)
	})

	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	allChanges := make([]Change, 0, 5000)

	for _, u := range urls {
		allChanges = scrapeURL(ctx, allChanges, u)
	}

	allChanges = fixCasing(allChanges)

	b, _ := json.MarshalIndent(struct {
		Changes []Change
	}{allChanges}, "", "  ")

	fmt.Println(string(b))
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

	if strings.Contains(u, "/23923813/") {
		dest = scrapeContentUpdate(dest, doc, "#item8", "10.0.7",
			time.Date(2023, 3, 16, 0, 0, 0, 0, time.UTC))
		return dest
	}

	if strings.Contains(u, "/23892227/") {
		dest = scrapeContentUpdate(dest, doc, "#item3", "10.0.5",
			time.Date(2023, 1, 24, 0, 0, 0, 0, time.UTC))
		return dest
	}

	log.Fatalf("Unrecognizable URL: %s", u)

	return dest
}

func fixCasing(changes []Change) []Change {
	tagSet := map[string]string{ // upper -> mixed
		"ELEMENTAL STORMS":     "Elemental Storms",
		"PLAYER VERSUS PLAYER": "PvP",
		"RATED SOLO SHUFFLE":   "Solo Shuffle",
		"CRAFTING ORDERS":      "Crafting Orders",
		"TALENT WINDOW":        "Talent Window",
		"WOW COMPANION APP":    "WoW Companion App",
		"HOLIDAYS":             "Holidays",
		"LUNAR NEW YEAR":       "Lunar New Year",
		"TRIAL OF STYLE":       "Trial of Style",
		"GROUP LOOT":           "Group Loot",
		"MAGE TOWER":           "Mage Tower",
		"CRAFTING UI PANEL":    "Crafting UI Panel",
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
	ignore := true

	changeSets := doc.Find(".Blog .detail h4")
	changeSets.Each(func(i int, category *goquery.Selection) {
		if category.Is(firstHeader) {
			ignore = false
		}

		if ignore {
			return
		}

		ul := category.NextFiltered("ul")

		dest = collectChanges(dest, ul,
			append(cleanTag(category.Text()), version),
			date, doc.Url,
		)
	})

	return dest
}

func scrapeHotfixes(dest []Change, doc *goquery.Document) []Change {
	changeSets := doc.Find(".Blog .detail h4")
	if changeSets.Size() == 0 {
		log.Fatal("No change sets in HTML doc")
	}

	changeSets.Each(func(i int, patchNotes *goquery.Selection) {
		dateEn := patchNotes.Text()
		date, err := time.Parse("January 2, 2006", dateEn)
		if err != nil {
			log.Fatal(err)
		}

		var nCategories int
		for {
			category := patchNotes.NextFiltered("p")
			if category.Size() == 0 {
				break
			}
			nCategories++

			ul := category.NextFiltered("ul")
			if ul.Size() == 0 {
				log.Fatalf("No changes in category %q for change set %d (%s)", category.Text(), i, date.Format(time.DateOnly))
			}

			dest = collectChanges(dest, ul,
				cleanTag(category.Text()),
				date, doc.Url,
			)

			patchNotes = ul
		}
		if nCategories == 0 {
			log.Fatalf("No categories in change set %d (%s)", i, date.Format(time.DateOnly))
		}
	})

	return dest
}

func collectChanges(dest []Change, ul *goquery.Selection, tags []string, date time.Time, srcURL *url.URL) []Change {
	for _, c := range flattenChanges(ul, tags) {
		c.Date = date.Format(time.DateOnly)
		c.Weekday = date.Weekday().String()
		if srcURL != nil {
			u := new(url.URL)
			*u = *srcURL
			u.Path = filepath.Dir(u.Path)
			c.URL = u.String()
		}
		dest = append(dest, c)
	}

	return dest
}

var leadingSpace = regexp.MustCompile(`\n[ \t]+`)
var repeatedSpace = regexp.MustCompile(`[ \t]+`)
var repeatedNL = regexp.MustCompile(`\n\n+`)

func flattenChanges(ul *goquery.Selection, tags []string) []Change {
	for _, t := range tags {
		if strings.Contains(t, "WotLK") {
			return nil
		}
	}

	var changes []Change

	var lis []*goquery.Selection

	ul.Children().Each(func(i int, li *goquery.Selection) {
		lis = append(lis, li)
	})

	for _, li := range lis {
		newTags := cleanTag(li.ChildrenFiltered("strong").First().Text())
		if len(newTags) > 0 {
			changes = append(changes, flattenChanges(li.ChildrenFiltered("ul"), append(tags, newTags...))...)
		} else {
			text := strings.TrimSpace(li.Text())
			text = leadingSpace.ReplaceAllString(text, "\n")
			text = repeatedSpace.ReplaceAllString(text, " ")
			text = repeatedNL.ReplaceAllString(text, "\n\n")

			changes = append(changes, Change{
				Tags: tags,
				Text: text,
			})
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
	).Replace(t)

	if strings.HasSuffix(t, "Tuskar") {
		t = strings.TrimSuffix(t, "Tuskar") + "Tuskarr"
	}

	t = strings.TrimPrefix(t, "The ")

	t = strings.TrimSuffix(t, " (Raidfinder)")
	t = strings.TrimSuffix(t, " (Normal)")
	t = strings.TrimSuffix(t, " (Heroic)")
	t = strings.TrimSuffix(t, " (Mythic)")

	if t == "Discipline, Shadow" {
		return []string{"Discipline", "Shadow"}
	}

	return []string{t}
}
