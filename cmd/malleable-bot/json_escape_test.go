package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPollContentEscaping(t *testing.T) {
	// Simulate the poll content that's failing
	pollContent := `{"layout":"card","elements":[{"type":"heading","value":"What's Your Favorite Ice Cream Flavor?"},{"type":"text","value":"Cast your vote!"}]}`

	// Method 1: Using json.Marshal
	escaped1, _ := json.Marshal(pollContent)
	t.Logf("json.Marshal result: %s", string(escaped1))

	// Method 2: Manual (what we're doing)
	escaped2 := escapeJSONString(pollContent)
	t.Logf("escapeJSONString result: %s", escaped2)

	// They should be the same
	if string(escaped1) != escaped2 {
		t.Errorf("Escaping methods differ!")
	}

	// Build a full event serialization
	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764775741,
		Kind:      1,
		Tags:      [][]string{{"e", "72e0066da10a379ee4c91bc741cc20a5d4818c83c2dd593992c708d8f75563a2", "", "reply"}, {"p", "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331"}},
		Content:   pollContent,
	}

	// Compute ID our way
	id := computeEventID(event)
	t.Logf("Computed ID: %s", id)

	// Also compute manually
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

	t.Logf("Full serialization:\n%s", serialized)

	hash := sha256.Sum256([]byte(serialized))
	manualID := hex.EncodeToString(hash[:])
	t.Logf("Manual ID: %s", manualID)

	if id != manualID {
		t.Errorf("ID mismatch! computed=%s, manual=%s", id, manualID)
	}

	// The key check: make sure contentJSON equals escapeJSONString result
	if string(contentJSON) != escapeJSONString(event.Content) {
		t.Errorf("Content escaping methods differ:\njson.Marshal: %s\nescapeJSONString: %s", string(contentJSON), escapeJSONString(event.Content))
	}
}

func TestActualFailingEvent(t *testing.T) {
	// Recreate the exact failing event from logs
	// Event ID from logs: f6ef2717b53e0234f5f9d68668f179da59d9945e393a532cf2bf165ec4665c91

	// The content would have been something like the ice cream poll
	content := `{"layout":"card","elements":[{"type":"heading","value":"What's Your Favorite Ice Cream Flavor?"},{"type":"text","value":"Cast your vote for the best ice cream flavor!"},{"type":"hr"},{"type":"container","style":"options","children":[{"type":"button","label":"Vanilla","action":"vote-vanilla"},{"type":"button","label":"Chocolate","action":"vote-chocolate"},{"type":"button","label":"Strawberry","action":"vote-strawberry"},{"type":"button","label":"Mint Chip","action":"vote-mint"}]},{"type":"hr"},{"type":"data","bind":"$.time","label":"Poll created: "}],"actions":[{"id":"vote-vanilla","publish":{"kind":7,"content":"vanilla","tags":[["e","{{$.id}}"]]}},{"id":"vote-chocolate","publish":{"kind":7,"content":"chocolate","tags":[["e","{{$.id}}"]]}},{"id":"vote-strawberry","publish":{"kind":7,"content":"strawberry","tags":[["e","{{$.id}}"]]}},{"id":"vote-mint","publish":{"kind":7,"content":"mint-chip","tags":[["e","{{$.id}}"]]}}]}`

	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764775741,
		Kind:      1,
		Tags:      [][]string{{"e", "72e0066da10a379ee4c91bc741cc20a5d4818c83c2dd593992c708d8f75563a2", "", "reply"}, {"p", "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331"}},
		Content:   content,
	}

	id := computeEventID(event)
	t.Logf("Computed ID: %s", id)

	// Check if escapeJSONString handles all special chars correctly
	escaped := escapeJSONString(content)
	t.Logf("Escaped content length: %d", len(escaped))

	// Verify it's valid JSON
	var test interface{}
	if err := json.Unmarshal([]byte(escaped), &test); err != nil {
		t.Errorf("Escaped content is not valid JSON: %v", err)
	}
}
