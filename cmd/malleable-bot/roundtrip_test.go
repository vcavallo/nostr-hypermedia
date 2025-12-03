package main

import (
	"encoding/json"
	"testing"
)

func TestEventRoundtrip(t *testing.T) {
	// Create an event
	original := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764783557,
		Kind:      6666,
		Tags: [][]string{
			{"t", "malleable-ui"},
			{"alt", "Test event"},
		},
		Content: `{"layout":"card","elements":[{"type":"text","value":"Hello"}]}`,
	}

	// Compute the ID before serialization
	originalID := computeEventID(original)
	original.ID = originalID
	original.Sig = "placeholder"

	t.Logf("Original ID: %s", originalID)

	// Serialize to JSON (what we send to relay)
	jsonBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	t.Logf("Serialized event: %s", string(jsonBytes))

	// Parse it back (what relay does)
	var parsed Event
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Relay would compute the ID from the parsed fields
	relayID := computeEventID(&parsed)
	t.Logf("Relay computed ID: %s", relayID)

	if originalID != relayID {
		t.Errorf("ID mismatch after roundtrip!\n  original: %s\n  relay:    %s", originalID, relayID)

		// Debug: show what's different
		t.Logf("Original PubKey: %q", original.PubKey)
		t.Logf("Parsed PubKey:   %q", parsed.PubKey)
		t.Logf("Original CreatedAt: %d", original.CreatedAt)
		t.Logf("Parsed CreatedAt:   %d", parsed.CreatedAt)
		t.Logf("Original Kind: %d", original.Kind)
		t.Logf("Parsed Kind:   %d", parsed.Kind)
		t.Logf("Original Content: %q", original.Content)
		t.Logf("Parsed Content:   %q", parsed.Content)

		origTagsJSON, _ := json.Marshal(original.Tags)
		parsedTagsJSON, _ := json.Marshal(parsed.Tags)
		t.Logf("Original Tags: %s", string(origTagsJSON))
		t.Logf("Parsed Tags:   %s", string(parsedTagsJSON))
	} else {
		t.Log("Roundtrip successful - IDs match!")
	}
}

func TestEventWithEmbeddedJSON(t *testing.T) {
	// Test with a "request" tag containing JSON (like DVM events)
	requestEvent := &Event{
		ID:        "abc123",
		PubKey:    "def456",
		CreatedAt: 12345,
		Kind:      5666,
		Tags:      [][]string{{"i", "test input", "text"}},
		Content:   "",
		Sig:       "sig789",
	}

	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request event JSON: %s", string(requestJSON))

	// Create the DVM response with the request embedded
	response := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764783557,
		Kind:      6666,
		Tags: [][]string{
			{"request", string(requestJSON)},
			{"e", requestEvent.ID},
		},
		Content: `{"layout":"card"}`,
	}

	// Compute ID
	originalID := computeEventID(response)
	response.ID = originalID
	response.Sig = "placeholder"

	t.Logf("Original ID: %s", originalID)

	// Serialize
	responseJSON, _ := json.Marshal(response)
	t.Logf("Response JSON: %s", string(responseJSON))

	// Parse back
	var parsed Event
	json.Unmarshal(responseJSON, &parsed)

	// Compute ID from parsed
	parsedID := computeEventID(&parsed)
	t.Logf("Parsed ID: %s", parsedID)

	if originalID != parsedID {
		t.Errorf("ID mismatch with embedded JSON!")

		// Check if the request tag survived
		for i, tag := range parsed.Tags {
			origTag := response.Tags[i]
			if len(tag) != len(origTag) {
				t.Errorf("Tag %d length mismatch: %d vs %d", i, len(tag), len(origTag))
				continue
			}
			for j, v := range tag {
				if v != origTag[j] {
					t.Errorf("Tag[%d][%d] mismatch:\n  original: %q\n  parsed:   %q", i, j, origTag[j], v)
				}
			}
		}
	} else {
		t.Log("Embedded JSON roundtrip successful!")
	}
}
