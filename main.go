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

	// var allChanges []ChangeSet
	var allChanges []Change

	for _, fname := range []string{"2.html", "1.html"} {
		b, err := os.ReadFile(fname)
		if err != nil {
			log.Fatal(err)
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
		if err != nil {
			log.Fatal(err)
		}

		doc.Find(".Blog .detail h4").Each(func(i int, patchNotes *goquery.Selection) {
			dateEn := patchNotes.Text()
			date, err := time.Parse("January 2, 2006", dateEn)
			if err != nil {
				log.Fatal(err)
			}

			// changeSet := ChangeSet{Date: date}
			for {
				category := patchNotes.NextFiltered("p")
				if category.Size() == 0 {
					break
				}
				// fmt.Println(category.Text())

				changes := category.NextFiltered("ul")

				for _, c := range flattenChanges(changes, []string{category.Text()}) {
					c.Date = date
					allChanges = append(allChanges, c)
				}
				// changeSet.Changes = append(changeSet.Changes,
				// 	flattenChanges(changes, []string{category.Text()})...,
				// )

				patchNotes = changes
			}
			// allChanges = append(allChanges, changeSet)
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
		tag := li.ChildrenFiltered("strong").First().Text()
		if tag != "" {
			changes = append(changes, flattenChanges(li.ChildrenFiltered("ul"), append(tags, tag))...)
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

// type ChangeSet struct {
//  Date   time.Time
// 	Changes []Change
// }

type Change struct {
	Date   time.Time
	Tags   []string
	Change string
}
