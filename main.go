package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Change struct {
	URL     string
	Date    string
	Weekday string
	Tags    []string
	Change  string
}

func main() {
	res, err := http.Get("https://worldofwarcraft.blizzard.com/en-us/search/blog?k=Update%20Notes")
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

		return !strings.Contains(href, "/23885941/") // January hotfixes
	})

	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	allChanges := make([]Change, 0, 5000)

	for _, u := range urls {
		allChanges = scrapeURL(allChanges, u)
	}

	allChanges = fixCasing(allChanges)

	b, _ := json.MarshalIndent(struct {
		Changes []Change
	}{allChanges}, "", "  ")

	fmt.Println(string(b))
}

func scrapeURL(dest []Change, u string) []Change {
	log.Println(u)

	res, err := http.Get(u)
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

	if strings.Contains(u, "/23892227/") {
		dest = scrapeContentUpdate(dest, doc, "#item3", "10.0.5",
			time.Date(2023, 1, 24, 0, 0, 0, 0, time.UTC))
		return dest
	}

	log.Fatalf("Unrecognizable URL: %s", u)

	return dest
}

func fixCasing(changes []Change) []Change {
	tagSet := map[string]string{} // upper -> mixed

	for _, c := range changes {
		for _, t := range c.Tags {
			uc := strings.ToUpper(t)
			if uc == t {
				continue
			}
			tagSet[uc] = t
		}
	}

	for i, c := range changes {
		for j, t := range c.Tags {
			if mc, ok := tagSet[t]; ok {
				changes[i].Tags[j] = mc
			}
		}
	}

	return changes
}

func foo() {
	var allChanges []Change

	// https://worldofwarcraft.blizzard.com/en-us/search/blog?k=Update%20Notes

	// https://worldofwarcraft.blizzard.com/en-us/news/23892227
	for _, fname := range []string{"10.0.5.html"} {
		b, err := os.ReadFile(fname)
		if err != nil {
			log.Fatal(err)
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
		if err != nil {
			log.Fatal(err)
		}

		allChanges = scrapeContentUpdate(allChanges, doc, "#item3", "10.0.5", time.Date(2023, 1, 24, 0, 0, 0, 0, time.UTC))
	}

	// https://worldofwarcraft.blizzard.com/en-us/news/23892230
	// https://worldofwarcraft.blizzard.com/en-us/news/23885941

	// "datePublished":"2023-03-08T02:52:47+00:00","dateModified":"2023-03-08T02:52:47+00:00"
	// "datePublished":"2023-01-24T02:53:00+00:00","dateModified":"2023-01-26T01:32:59+00:00"

	for _, fname := range []string{"2.html", "1.html", "0.html"} {
		b, err := os.ReadFile(fname)
		if err != nil {
			log.Fatal(err)
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
		if err != nil {
			log.Fatal(err)
		}

		allChanges = scrapeHotfixes(allChanges, doc)
	}

	// Some tags appear in uppercase in the markup (instead of using
	// text-transform: uppercase in CSS). Fix those now.

	tagSet := map[string]string{} // upper -> mixed

	for _, c := range allChanges {
		for _, t := range c.Tags {
			uc := strings.ToUpper(t)
			if uc == t {
				continue
			}
			tagSet[uc] = t
		}
	}

	for i, c := range allChanges {
		for j, t := range c.Tags {
			if mc, ok := tagSet[t]; ok {
				allChanges[i].Tags[j] = mc
			}
		}
	}

	{
		b, _ := json.MarshalIndent(allChanges, "", "  ")
		fmt.Println(string(b))
	}
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

		// tags := cleanTag(s.Text())

		// allChanges = append(allChanges, scrapePatchNotes(i, s, date, append(tags, version))...)
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
				Tags:   tags,
				Change: text,
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

	if t == "Discipline, Shadow" {
		return []string{"Discipline", "Shadow"}
	}

	return []string{t}
}
