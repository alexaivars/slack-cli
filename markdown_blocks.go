package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var (
	bulletListPattern  = regexp.MustCompile(`^\s*[-*]\s+(.+?)\s*$`)
	orderedListPattern = regexp.MustCompile(`^\s*\d+[.)]\s+(.+?)\s*$`)
	headingPattern     = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*$`)
)

type richTextBlock struct {
	Type     string `json:"type"`
	Elements []any  `json:"elements"`
}

type richTextSectionElement struct {
	Type     string            `json:"type"`
	Elements []richTextElement `json:"elements"`
}

type richTextListElement struct {
	Type     string                   `json:"type"`
	Style    string                   `json:"style"`
	Indent   int                      `json:"indent"`
	Border   int                      `json:"border"`
	Elements []richTextSectionElement `json:"elements"`
}

type richTextElement struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	URL   string          `json:"url,omitempty"`
	Style map[string]bool `json:"style,omitempty"`
}

func markdownToSlackBlocks(input string) (json.RawMessage, string, error) {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\r\n", "\n"))
	if input == "" {
		return nil, "", errors.New("--format markdown requires message text")
	}

	blocks := markdownRichTextBlocks(input)
	payload, err := json.Marshal(blocks)
	if err != nil {
		return nil, "", err
	}
	return payload, markdownFallbackText(input), nil
}

func markdownRichTextBlocks(input string) []richTextBlock {
	lines := strings.Split(input, "\n")
	var blocks []richTextBlock
	var paragraph []string
	var listItems []string
	listStyle := ""

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(paragraph, "\n"))
		if text != "" {
			blocks = append(blocks, richTextSectionBlock(text, false))
		}
		paragraph = nil
	}
	flushList := func() {
		if len(listItems) == 0 {
			return
		}
		blocks = append(blocks, richTextListBlock(listStyle, listItems))
		listItems = nil
		listStyle = ""
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushParagraph()
			flushList()
			continue
		}
		if match := bulletListPattern.FindStringSubmatch(line); match != nil {
			flushParagraph()
			if listStyle != "" && listStyle != "bullet" {
				flushList()
			}
			listStyle = "bullet"
			listItems = append(listItems, match[1])
			continue
		}
		if match := orderedListPattern.FindStringSubmatch(line); match != nil {
			flushParagraph()
			if listStyle != "" && listStyle != "ordered" {
				flushList()
			}
			listStyle = "ordered"
			listItems = append(listItems, match[1])
			continue
		}
		flushList()
		if match := headingPattern.FindStringSubmatch(line); match != nil {
			flushParagraph()
			blocks = append(blocks, richTextSectionBlock(match[1], true))
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flushParagraph()
	flushList()
	return blocks
}

func richTextSectionBlock(text string, bold bool) richTextBlock {
	elements := markdownInlineElements(text)
	if bold {
		for index := range elements {
			if elements[index].Type == "text" {
				elements[index].Style = mergeStyle(elements[index].Style, "bold")
			}
		}
	}
	return richTextBlock{
		Type: "rich_text",
		Elements: []any{richTextSectionElement{
			Type:     "rich_text_section",
			Elements: elements,
		}},
	}
}

func richTextListBlock(style string, items []string) richTextBlock {
	sections := make([]richTextSectionElement, 0, len(items))
	for _, item := range items {
		sections = append(sections, richTextSectionElement{
			Type:     "rich_text_section",
			Elements: markdownInlineElements(item),
		})
	}
	return richTextBlock{
		Type: "rich_text",
		Elements: []any{richTextListElement{
			Type:     "rich_text_list",
			Style:    style,
			Indent:   0,
			Border:   0,
			Elements: sections,
		}},
	}
}

func markdownInlineElements(input string) []richTextElement {
	var elements []richTextElement
	for len(input) > 0 {
		if strings.HasPrefix(input, "**") {
			if text, rest, ok := takeDelimited(input[2:], "**"); ok {
				elements = append(elements, richTextText(text, "bold"))
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "__") {
			if text, rest, ok := takeDelimited(input[2:], "__"); ok {
				elements = append(elements, richTextText(text, "bold"))
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "~~") {
			if text, rest, ok := takeDelimited(input[2:], "~~"); ok {
				elements = append(elements, richTextText(text, "strike"))
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "`") {
			if text, rest, ok := takeDelimited(input[1:], "`"); ok {
				elements = append(elements, richTextText(text, "code"))
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "[") {
			if link, rest, ok := takeMarkdownLink(input); ok {
				elements = append(elements, link)
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "*") && !strings.HasPrefix(input, "**") {
			if text, rest, ok := takeDelimited(input[1:], "*"); ok {
				elements = append(elements, richTextText(text, "italic"))
				input = rest
				continue
			}
		}
		if strings.HasPrefix(input, "_") && !strings.HasPrefix(input, "__") {
			if text, rest, ok := takeDelimited(input[1:], "_"); ok {
				elements = append(elements, richTextText(text, "italic"))
				input = rest
				continue
			}
		}

		next := nextMarkupIndex(input)
		elements = append(elements, richTextText(input[:next]))
		input = input[next:]
	}
	return compactTextElements(elements)
}

func richTextText(text string, styles ...string) richTextElement {
	element := richTextElement{Type: "text", Text: text}
	for _, style := range styles {
		element.Style = mergeStyle(element.Style, style)
	}
	return element
}

func mergeStyle(existing map[string]bool, style string) map[string]bool {
	if existing == nil {
		existing = map[string]bool{}
	}
	existing[style] = true
	return existing
}

func takeDelimited(input string, delimiter string) (string, string, bool) {
	index := strings.Index(input, delimiter)
	if index <= 0 {
		return "", "", false
	}
	text := input[:index]
	if strings.TrimSpace(text) == "" {
		return "", "", false
	}
	return text, input[index+len(delimiter):], true
}

func takeMarkdownLink(input string) (richTextElement, string, bool) {
	closeLabel := strings.Index(input, "](")
	if closeLabel <= 1 {
		return richTextElement{}, "", false
	}
	closeURL := strings.Index(input[closeLabel+2:], ")")
	if closeURL <= 0 {
		return richTextElement{}, "", false
	}
	label := input[1:closeLabel]
	url := input[closeLabel+2 : closeLabel+2+closeURL]
	if strings.TrimSpace(label) == "" || strings.TrimSpace(url) == "" {
		return richTextElement{}, "", false
	}
	return richTextElement{Type: "link", Text: label, URL: url}, input[closeLabel+2+closeURL+1:], true
}

func nextMarkupIndex(input string) int {
	next := len(input)
	for _, marker := range []string{"**", "__", "~~", "`", "[", "*", "_"} {
		if index := strings.Index(input[1:], marker); index >= 0 && index+1 < next {
			next = index + 1
		}
	}
	return next
}

func compactTextElements(elements []richTextElement) []richTextElement {
	compacted := make([]richTextElement, 0, len(elements))
	for _, element := range elements {
		if element.Text == "" && element.Type == "text" {
			continue
		}
		if len(compacted) > 0 && canMergeText(compacted[len(compacted)-1], element) {
			compacted[len(compacted)-1].Text += element.Text
			continue
		}
		compacted = append(compacted, element)
	}
	return compacted
}

func canMergeText(a, b richTextElement) bool {
	if a.Type != "text" || b.Type != "text" {
		return false
	}
	if len(a.Style) != len(b.Style) {
		return false
	}
	for key, value := range a.Style {
		if b.Style[key] != value {
			return false
		}
	}
	return true
}

func markdownFallbackText(input string) string {
	lines := strings.Split(strings.TrimSpace(input), "\n")
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if match := headingPattern.FindStringSubmatch(line); match != nil {
			line = match[1]
		}
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		lines[index] = line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
