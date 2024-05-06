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

		fixCasing(changes)
		checkTags(changes)

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

	fixCasing(allChanges)
	checkTags(allChanges)

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

	if strings.Contains(u, "/24066682/") {
		dest = scrapeContentUpdate(dest, doc, "#item2", "10.3.0",
			time.Date(2024, 4, 19, 0, 0, 0, 0, time.UTC))
		return dest
	}

	if strings.Contains(u, "/24066683/") {
		// Cosmetic updates only.
		return dest
	}

	log.Fatalf("Unrecognizable URL: %s", u)

	return dest
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
		if strings.Contains(t, "Season of Discovery") {
			return nil
		}
	}

	var changes []Change

	addChange := func(n *Tree, tags []string) {
		text := n.Text

		ts := make([]string, 0, len(tags))
		for _, t := range tags {
			if strings.HasPrefix(t, "__text_prefix ") {
				text = strings.TrimPrefix(t, "__text_prefix ") + text
			} else {
				ts = append(ts, t)
			}
		}

		changes = append(changes, Change{
			Date:    date.Format(time.DateOnly),
			Weekday: date.Weekday().String(),
			URL:     srcURL,
			Tags:    ts,
			Text:    text,
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
