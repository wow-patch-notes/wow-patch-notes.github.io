package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type TextType int

const (
	TypeUnclassified TextType = iota
	TypeDate
	TypeTag
	TypeChange
)

func (t TextType) String() string {
	switch t {
	case TypeUnclassified:
		return "unclassified"
	case TypeDate:
		return "date"
	case TypeTag:
		return "tag"
	case TypeChange:
		return "change"
	default:
		return fmt.Sprintf("<undefined:%d>", int(t))
	}
}

func (t TextType) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

type Tree struct {
	Text     string   `json:",omitempty"`
	Type     TextType `json:",omitempty"`
	Children []*Tree  `json:",omitempty"`
}

func CollectTexts(node *html.Node) []*Tree {
	var subTrees []*Tree
	tree := &Tree{}

	for n := node.FirstChild; n != nil; n = n.NextSibling {
		if n.Type != html.ElementNode || n.Data != "div" {
			continue
		}

		for n.FirstChild != nil {
			fc := n.FirstChild
			n.RemoveChild(n.FirstChild)
			n.Parent.InsertBefore(fc, n)
		}
	}

	for n := node.FirstChild; n != nil; n = n.NextSibling {
		if isInline(n) {
			tree.Text += text(n)
			continue
		}

		if tree.Text != "" {
			subTrees = append(subTrees, tree)
		}
		subTrees = append(subTrees, &Tree{
			Children: CollectTexts(n),
		})
		tree = &Tree{}
	}
	if tree.Text != "" {
		subTrees = append(subTrees, tree)
	}

	return subTrees
}

func isInline(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return true
	}

	switch n.Data {
	case "em", "strong", "small",
		"a", "b", "i", "tt", "code":
		return true
	default:
		return false
	}
}

func text(node *html.Node) string {
	var buf bytes.Buffer

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(node)

	return buf.String()
}

func (t *Tree) Prune(joinText bool) {
	var cs []*Tree
	for _, c := range t.Children {
		c.Prune(joinText)

		if len(c.Children) == 1 && (!joinText || c.Children[0].Text == "") {
			c = c.Children[0]
		}

		if c.Text == "" && len(c.Children) == 0 {
			continue
		}

		n := len(cs)
		if joinText && c.Text != "" && n > 0 && cs[n-1].Text != "" {
			cs[n-1].Text += c.Text
			continue
		}

		cs = append(cs, c)
	}
	t.Children = cs
}

func (t *Tree) Walk(f func(n *Tree)) {
	f(t)

	for _, c := range t.Children {
		c.Walk(f)
	}
}

func Classify(t *Tree) TextType {
	if len(t.Children) > 0 {
		return TypeUnclassified
	}

	// Both tags and dates are shorter than 50 bytes.
	if len(t.Text) >= 50 {
		return TypeChange
	}

	// Tags never seem to contain a full stop, but 99% of all change notes are
	// written as complete sentences...
	if strings.Contains(t.Text, ".") {
		return TypeChange
	}

	// ... and the few that are not happen to contain some sort of change
	// measured in percent.
	if strings.Contains(t.Text, "%") {
		return TypeChange
	}
	if strings.Contains(t.Text, "%") {
		return TypeChange
	}
	if strings.HasSuffix(t.Text, ":") {
		return TypeChange
	}
	if strings.Contains(t.Text, "/ping") {
		return TypeChange
	}

	_, err := time.Parse("January 2, 2006", t.Text)
	if err == nil {
		return TypeDate
	}

	// if changePattern.MatchString(s) {
	// 	return TypeChange
	// }

	return TypeTag
}

// var changePattern *regexp.Regexp

// func init() {
// 	phrases := []string{
// 		"(fixed|addresse[sd]|resolve[sd]) (an?|some) (issue|bug|error)s?",
// 		"(visual|audio) (issue|error)s?",
// 		"(will|is|may|are|cat) now",
// 		"no longer", "now causes", "now triggers", "should again",
// 		"adjusted", "increase[sd]", "decrease[sd]", "reduce[sd]",
// 		"properly", "correctly",
// 		"not apply", "this change",
// 		"in pvp combat",
// 		"yards", "seconds",
// 		"damage done", "cooldown", "health", "base mana", "stacks up to", "absorb shield",
// 		"developers['’]? notes?",
// 		"now spawn",
// 	}

// 	changePattern = regexp.MustCompile(fmt.Sprintf(`\b(%s)\b`,
// 		strings.Join(phrases, "|")))
// }
