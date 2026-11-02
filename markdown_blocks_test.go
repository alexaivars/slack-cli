package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarkdownToSlackBlocksConvertsMixedMessage(t *testing.T) {
	input := "some text\n\n**BOLD TEXT**\n\nmy list\n- 1\n- 2\n- 3\n"
	blocks, fallback, err := markdownToSlackBlocks(input)
	if err != nil {
		t.Fatalf("markdownToSlackBlocks returned an error: %v", err)
	}
	if fallback == "" {
		t.Fatal("fallback text is empty")
	}

	var decoded []map[string]any
	if err := json.Unmarshal(blocks, &decoded); err != nil {
		t.Fatalf("blocks are invalid JSON: %v", err)
	}
	if len(decoded) != 4 {
		t.Fatalf("block count is %d, want 4: %s", len(decoded), string(blocks))
	}
	for index, want := range []map[string]any{
		{"type": "text", "text": "some text"},
		{"type": "text", "text": "BOLD TEXT", "style": map[string]any{"bold": true}},
		{"type": "text", "text": "my list"},
	} {
		section := decoded[index]["elements"].([]any)[0].(map[string]any)
		if got := section["elements"]; !reflect.DeepEqual(got, []any{want}) {
			t.Fatalf("paragraph %d elements = %#v, want %#v", index, got, want)
		}
	}
	list := decoded[3]["elements"].([]any)[0].(map[string]any)
	if list["type"] != "rich_text_list" || list["style"] != "bullet" {
		t.Fatalf("last block is not a bullet rich_text_list: %#v", list)
	}
	if len(list["elements"].([]any)) != 3 {
		t.Fatalf("list item count is %d, want 3", len(list["elements"].([]any)))
	}
	for index, want := range []string{"1", "2", "3"} {
		item := list["elements"].([]any)[index].(map[string]any)
		if got := item["elements"]; !reflect.DeepEqual(got, []any{map[string]any{"type": "text", "text": want}}) {
			t.Fatalf("list item %d elements = %#v, want text %q", index, got, want)
		}
	}
}

func TestMarkdownToSlackBlocksConvertsOrderedListsAndLinks(t *testing.T) {
	input := "# Heading\n\n1. first **bold** item\n2. [docs](https://docs.slack.dev)\n"
	blocks, _, err := markdownToSlackBlocks(input)
	if err != nil {
		t.Fatalf("markdownToSlackBlocks returned an error: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(blocks, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("block count = %d, want heading and ordered list", len(decoded))
	}
	heading := decoded[0]["elements"].([]any)[0].(map[string]any)
	wantHeading := []any{map[string]any{"type": "text", "text": "Heading", "style": map[string]any{"bold": true}}}
	if !reflect.DeepEqual(heading["elements"], wantHeading) {
		t.Fatalf("heading elements = %#v, want %#v", heading["elements"], wantHeading)
	}
	list := decoded[1]["elements"].([]any)[0].(map[string]any)
	if list["type"] != "rich_text_list" || list["style"] != "ordered" {
		t.Fatalf("expected ordered rich_text_list, got %#v", list)
	}
	items := list["elements"].([]any)
	if len(items) != 2 {
		t.Fatalf("ordered list has %d items, want 2", len(items))
	}
	for index, want := range [][]any{
		{map[string]any{"type": "text", "text": "first "}, map[string]any{"type": "text", "text": "bold", "style": map[string]any{"bold": true}}, map[string]any{"type": "text", "text": " item"}},
		{map[string]any{"type": "link", "text": "docs", "url": "https://docs.slack.dev"}},
	} {
		item := items[index].(map[string]any)
		if !reflect.DeepEqual(item["elements"], want) {
			t.Fatalf("ordered list item %d elements = %#v, want %#v", index, item["elements"], want)
		}
	}
}
