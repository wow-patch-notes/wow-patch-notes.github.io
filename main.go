package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	// https://worldofwarcraft.blizzard.com/en-us/news/23892230/hotfixes-march-7-2023
	// https://worldofwarcraft.blizzard.com/en-us/news/23885941/hotfixes-january-23-2023

	// "datePublished":"2023-03-08T02:52:47+00:00","dateModified":"2023-03-08T02:52:47+00:00"
	// "datePublished":"2023-01-24T02:53:00+00:00","dateModified":"2023-01-26T01:32:59+00:00"

	var allChanges []Change

	for _, fname := range []string{"2.html", "1.html", "0.html"} {
		b, err := os.ReadFile(fname)
		if err != nil {
			log.Fatal(err)
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
		if err != nil {
			log.Fatal(err)
		}

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

				changes := category.NextFiltered("ul")
				if changes.Size() == 0 {
					log.Fatalf("No changes in category %q for change set %d (%s)", category.Text(), i, date.Format(time.DateOnly))
				}

				for _, c := range flattenChanges(changes, cleanTag(category.Text())) {
					c.Date = date.Format(time.DateOnly)
					c.Weekday = date.Weekday().String()
					allChanges = append(allChanges, c)
				}

				patchNotes = changes
			}
			if nCategories == 0 {
				log.Fatalf("No categories in change set %d (%s)", i, date.Format(time.DateOnly))
			}
		})
	}

	{
		b, _ := json.MarshalIndent(allChanges, "", "  ")
		fmt.Println(string(b))
	}
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

type Change struct {
	Date    string
	Weekday string
	Tags    []string
	Change  string
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
