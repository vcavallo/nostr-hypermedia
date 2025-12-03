package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func TestEmojiSerialization(t *testing.T) {
	// Test content with emojis
	content := `{"label":"🍦 Vanilla"}`

	// Method 1: json.Marshal
	escaped1, _ := json.Marshal(content)
	t.Logf("json.Marshal: %s", string(escaped1))
	t.Logf("json.Marshal bytes: %v", escaped1)

	// Check if emoji is being escaped as \uXXXX
	// 🍦 is U+1F366, which in JSON should be \ud83c\udf66 (surrogate pair)
	// or it could be kept as literal UTF-8 bytes

	// Build an event with emoji content
	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764776086,
		Kind:      1,
		Tags:      [][]string{{"e", "abc", "", "reply"}},
		Content:   content,
	}

	// Compute ID
	id := computeEventID(event)
	t.Logf("Event ID: %s", id)

	// Show the serialization
	tagsJSON, _ := json.Marshal(event.Tags)
	contentJSON := escapeJSONString(event.Content)

	serialized := fmt.Sprintf(
		`[0,"%s",%d,%d,%s,%s]`,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		string(tagsJSON),
		contentJSON,
	)
	t.Logf("Serialization: %s", serialized)
	t.Logf("Serialization bytes: %v", []byte(serialized))
}

func TestEmojiVsNoEmoji(t *testing.T) {
	// Compare with and without emoji
	contentWithEmoji := `{"label":"🍦 Vanilla"}`
	contentNoEmoji := `{"label":"Vanilla"}`

	escaped1, _ := json.Marshal(contentWithEmoji)
	escaped2, _ := json.Marshal(contentNoEmoji)

	t.Logf("With emoji: %s (len=%d)", string(escaped1), len(escaped1))
	t.Logf("Without emoji: %s (len=%d)", string(escaped2), len(escaped2))

	// The emoji 🍦 is 4 bytes in UTF-8
	// In JSON, it could be:
	// 1. Literal UTF-8 bytes (4 bytes)
	// 2. Escaped as \ud83c\udf66 (12 bytes)

	// Check what Go's json.Marshal does
	if string(escaped1) == `"{\"label\":\"🍦 Vanilla\"}"` {
		t.Log("Go keeps emoji as literal UTF-8")
	} else {
		t.Log("Go escapes emoji somehow")
	}
}

func TestManualVsJsonMarshal(t *testing.T) {
	// The real test: does our computeEventID match what we'd get with just json.Marshal?
	content := `{"label":"🍦 Vanilla"}`

	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764776086,
		Kind:      1,
		Tags:      [][]string{},
		Content:   content,
	}

	// Method 1: Our computeEventID
	id1 := computeEventID(event)

	// Method 2: Using json.Marshal on the array directly
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	hash := sha256.Sum256(jsonBytes)
	id2 := hex.EncodeToString(hash[:])

	t.Logf("Our method:    %s", id1)
	t.Logf("json.Marshal:  %s", id2)
	t.Logf("Our serialization uses: %s", escapeJSONString(content))
	t.Logf("json.Marshal produces:  %s", string(jsonBytes))

	if id1 != id2 {
		t.Errorf("IDs don't match! This is the bug.")
	}
}
