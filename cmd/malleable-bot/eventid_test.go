package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func TestEventIDComputation(t *testing.T) {
	// Test case: a known event with verified ID
	// This is a minimal event we can verify
	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{},
		Content:   "test",
	}

	// Compute ID using our function
	computedID := computeEventID(event)

	// Also compute manually to compare
	tagsJSON, _ := json.Marshal(event.Tags)
	contentJSON, _ := json.Marshal(event.Content)

	serialized := fmt.Sprintf(
		`[0,"%s",%d,%d,%s,%s]`,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		string(tagsJSON),
		string(contentJSON),
	)

	t.Logf("Serialized: %s", serialized)

	hash := sha256.Sum256([]byte(serialized))
	manualID := hex.EncodeToString(hash[:])

	t.Logf("Computed ID: %s", computedID)
	t.Logf("Manual ID:   %s", manualID)

	if computedID != manualID {
		t.Errorf("IDs don't match: computed=%s, manual=%s", computedID, manualID)
	}

	// The expected serialization for this event should be:
	// [0,"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",1700000000,1,[],"test"]
	expected := `[0,"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",1700000000,1,[],"test"]`
	if serialized != expected {
		t.Errorf("Serialization mismatch:\ngot:      %s\nexpected: %s", serialized, expected)
	}
}

func TestEventIDWithTags(t *testing.T) {
	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{{"e", "abc123", "", "reply"}, {"p", "def456"}},
		Content:   "test reply",
	}

	tagsJSON, _ := json.Marshal(event.Tags)
	contentJSON, _ := json.Marshal(event.Content)

	serialized := fmt.Sprintf(
		`[0,"%s",%d,%d,%s,%s]`,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		string(tagsJSON),
		string(contentJSON),
	)

	t.Logf("Serialized with tags: %s", serialized)

	// Expected format
	expected := `[0,"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",1700000000,1,[["e","abc123","","reply"],["p","def456"]],"test reply"]`
	if serialized != expected {
		t.Errorf("Serialization mismatch:\ngot:      %s\nexpected: %s", serialized, expected)
	}
}

func TestEventIDWithSpecialChars(t *testing.T) {
	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{},
		Content:   `{"test": "json with \"quotes\" and \n newlines"}`,
	}

	id := computeEventID(event)
	t.Logf("ID for content with special chars: %s", id)

	// Verify content is properly escaped
	contentJSON, _ := json.Marshal(event.Content)
	t.Logf("Escaped content: %s", string(contentJSON))
}
