package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestCompareWithNak(t *testing.T) {
	// Create a simple event and compare our ID with nak's ID
	pubkey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	createdAt := int64(1764783557)
	kind := 6666
	content := `{"layout":"card","elements":[{"type":"text","value":"Hello"}]}`
	tags := [][]string{
		{"t", "malleable-ui"},
		{"alt", "Test"},
	}

	// Our computation
	event := &Event{
		PubKey:    pubkey,
		CreatedAt: createdAt,
		Kind:      kind,
		Tags:      tags,
		Content:   content,
	}
	ourID := computeEventID(event)
	t.Logf("Our ID: %s", ourID)

	// Build the serialization array manually
	serialized := []interface{}{
		0,
		pubkey,
		createdAt,
		kind,
		tags,
		content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Our serialization: %s", string(jsonBytes))

	// Hash it
	hash := sha256.Sum256(jsonBytes)
	manualID := hex.EncodeToString(hash[:])
	t.Logf("Manual ID from serialization: %s", manualID)

	// Use nak to create the same event (if available)
	// nak event --sec <key> -k 6666 -c '...' --tag t=malleable-ui --tag alt=Test
	cmd := exec.Command("nak", "event",
		"--sec", "nsec1ahepgxalc9wcjj9eppykwqpwkt84snmaqkjxzwhku2rwjllhke9sghpvnv",
		"-k", "6666",
		"-c", content,
		"--tag", "t=malleable-ui",
		"--tag", "alt=Test",
		"--created-at", "1764783557",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Logf("nak not available or failed: %v", err)
		return
	}

	t.Logf("nak output: %s", string(output))

	// Parse nak's output to get the ID
	var nakEvent map[string]interface{}
	if err := json.Unmarshal(output, &nakEvent); err != nil {
		t.Logf("Failed to parse nak output: %v", err)
		return
	}

	nakID := nakEvent["id"].(string)
	t.Logf("nak ID: %s", nakID)

	if ourID != nakID {
		t.Errorf("ID mismatch!\n  our ID: %s\n  nak ID: %s", ourID, nakID)

		// Debug: show what nak serializes
		nakPubkey := nakEvent["pubkey"].(string)
		t.Logf("nak pubkey: %s", nakPubkey)
		t.Logf("our pubkey: %s", pubkey)

		nakTags := nakEvent["tags"]
		nakTagsJSON, _ := json.Marshal(nakTags)
		t.Logf("nak tags: %s", string(nakTagsJSON))

		ourTagsJSON, _ := json.Marshal(tags)
		t.Logf("our tags: %s", string(ourTagsJSON))
	}
}

func TestSerializationOrder(t *testing.T) {
	// Test that tag order matches
	tags := [][]string{
		{"t", "malleable-ui"},
		{"alt", "Test"},
	}

	tagsJSON, _ := json.Marshal(tags)
	t.Logf("Tags JSON: %s", string(tagsJSON))

	// Ensure it's: [["t","malleable-ui"],["alt","Test"]]
	expected := `[["t","malleable-ui"],["alt","Test"]]`
	if string(tagsJSON) != expected {
		t.Errorf("Tag order wrong:\n  got:      %s\n  expected: %s", string(tagsJSON), expected)
	}
}

func TestContentEscaping(t *testing.T) {
	// Test content with special characters
	content := `{"layout":"card","elements":[{"type":"text","value":"Hello \"world\""}]}`

	serialized := []interface{}{
		0,
		"pubkey",
		int64(123),
		1,
		[][]string{},
		content,
	}

	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Serialization: %s", string(jsonBytes))

	// The content should be escaped properly
	// Inside the JSON, the quotes are double-escaped: \\\"world\\\"
	if !strings.Contains(string(jsonBytes), `\\\"world\\\"`) {
		t.Error("Content escaping looks wrong")
	}
}
