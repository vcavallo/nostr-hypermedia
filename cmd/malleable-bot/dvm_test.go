package main

import (
	"encoding/json"
	"testing"
)

func TestDVMEventSerialization(t *testing.T) {
	// Simulate a kind:6666 DVM response with a "request" tag containing JSON
	requestEvent := &Event{
		ID:        "20925e1a536eb6af2285394395d2d372617f201ed1914909e73afc379faf566a",
		PubKey:    "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331",
		CreatedAt: 1764783550,
		Kind:      5666,
		Tags: [][]string{
			{"i", "Create a feed", "text"},
			{"output", "application/json"},
		},
		Content: "",
		Sig:     "abc123",
	}

	// Stringify the request event as we do in createJobResult
	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request JSON: %s", string(requestJSON))

	// Build the response event
	uiSpec := `{"layout":"card","elements":[{"type":"text","value":"test"}]}`

	tags := [][]string{
		{"request", string(requestJSON)},
		{"e", requestEvent.ID},
		{"p", requestEvent.PubKey},
		{"alt", "Malleable UI specification"},
		{"t", "malleable-ui"},
	}

	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764783557,
		Kind:      6666,
		Tags:      tags,
		Content:   uiSpec,
	}

	// Compute our event ID
	ourID := computeEventID(event)
	t.Logf("Our computed ID: %s", ourID)

	// Show the serialization
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Serialization: %s", string(jsonBytes))

	// Now let's verify using nak if available
	// First, create the event JSON
	event.ID = ourID
	event.Sig = "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000" // Placeholder

	eventJSON, _ := json.Marshal(event)
	t.Logf("Full event JSON: %s", string(eventJSON))
}

func TestTagsWithEscapedJSON(t *testing.T) {
	// Test that tags containing JSON strings are serialized correctly
	innerJSON := `{"key":"value","nested":{"a":1}}`

	tags := [][]string{
		{"request", innerJSON},
		{"e", "abc123"},
	}

	// Serialize the tags array
	tagsJSON, _ := json.Marshal(tags)
	t.Logf("Tags serialized: %s", string(tagsJSON))

	// The inner JSON should be escaped as a string, not parsed as JSON
	// Expected: [["request","{\"key\":\"value\",\"nested\":{\"a\":1}}"],["e","abc123"]]
	expected := `[["request","{\"key\":\"value\",\"nested\":{\"a\":1}}"],["e","abc123"]]`
	if string(tagsJSON) != expected {
		t.Errorf("Tags serialization mismatch:\n  got:      %s\n  expected: %s", string(tagsJSON), expected)
	} else {
		t.Log("Tags serialization is correct!")
	}
}
