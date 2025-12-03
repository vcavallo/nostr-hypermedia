package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestJSONMarshalConsistency(t *testing.T) {
	// Test that json.Marshal produces consistent output
	event := &Event{
		ID:        "abc123",
		PubKey:    "pubkey123",
		CreatedAt: 12345,
		Kind:      1,
		Tags:      [][]string{{"e", "event1"}},
		Content:   "hello",
		Sig:       "sig123",
	}

	// Marshal multiple times
	json1, _ := json.Marshal(event)
	json2, _ := json.Marshal(event)
	json3, _ := json.Marshal(event)

	t.Logf("JSON 1: %s", string(json1))
	t.Logf("JSON 2: %s", string(json2))
	t.Logf("JSON 3: %s", string(json3))

	if string(json1) != string(json2) || string(json2) != string(json3) {
		t.Error("JSON marshaling is not consistent!")
	}
}

func TestNostrSerialization(t *testing.T) {
	// The Nostr serialization format for event ID is:
	// SHA256([0, pubkey, created_at, kind, tags, content])

	pubkey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	createdAt := int64(1764783557)
	kind := 6666
	tags := [][]string{{"t", "test"}}
	content := `{"key":"value"}`

	// Method 1: Using []interface{} (what we do)
	arr1 := []interface{}{0, pubkey, createdAt, kind, tags, content}
	json1, _ := json.Marshal(arr1)
	hash1 := sha256.Sum256(json1)
	id1 := hex.EncodeToString(hash1[:])

	t.Logf("Method 1 JSON: %s", string(json1))
	t.Logf("Method 1 ID: %s", id1)

	// Method 2: Using a typed struct
	type nostrEvent struct {
		V0        int        `json:"-"`
		PubKey    string     `json:"-"`
		CreatedAt int64      `json:"-"`
		Kind      int        `json:"-"`
		Tags      [][]string `json:"-"`
		Content   string     `json:"-"`
	}

	// Still need []interface{} for the array format
	arr2 := []interface{}{0, pubkey, createdAt, kind, tags, content}
	json2, _ := json.Marshal(arr2)
	hash2 := sha256.Sum256(json2)
	id2 := hex.EncodeToString(hash2[:])

	t.Logf("Method 2 JSON: %s", string(json2))
	t.Logf("Method 2 ID: %s", id2)

	if id1 != id2 {
		t.Error("IDs don't match!")
	}
}

func TestContentWithNewlines(t *testing.T) {
	// Test content that might have newlines from Claude
	content := "{\n  \"layout\": \"card\"\n}"

	// json.Marshal should preserve the newlines
	arr := []interface{}{0, "pubkey", int64(123), 1, [][]string{}, content}
	jsonBytes, _ := json.Marshal(arr)

	t.Logf("Serialization: %s", string(jsonBytes))
	t.Logf("Content in JSON has newlines: %v", string(jsonBytes))

	// The \n should be escaped as \\n in the JSON string
	expected := `[0,"pubkey",123,1,[],"{\n  \"layout\": \"card\"\n}"]`
	if string(jsonBytes) != expected {
		t.Logf("Expected: %s", expected)
		t.Logf("Got:      %s", string(jsonBytes))
	}
}

func TestActualFailingEvent(t *testing.T) {
	// Recreate the exact failing event from the logs
	// Based on the log output:
	// pubkey: bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec
	// created_at: 1764783557
	// kind: 6666

	// The request tag contains a JSON-stringified event
	requestEvent := Event{
		ID:        "20925e1a536eb6af2285394395d2d372617f201ed1914909e73afc379faf566a",
		PubKey:    "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331",
		CreatedAt: 1764783550,
		Kind:      5666,
		Tags: [][]string{
			{"i", "Create a feed that uses the NoteCard component (55522002f7aedebed64ff4300131cadcc3beb46d629af7bfb5521ebd0d3624a6) for each note", "text"},
			{"output", "application/json"},
			{"p", "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"},
		},
		Content: "",
		Sig:     "c742aed52114fff8a8853520eac5594b98dd2e56d203490aeeff7a4501ec3d5f6b992a4d2c47c2dba8599c6618b48d8f2a43b8a1b0f39327533503196986e7da",
	}

	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request JSON length: %d", len(requestJSON))
	t.Logf("Request JSON: %s", string(requestJSON))

	// Now check what our computeEventID produces
	pubkey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	createdAt := int64(1764783557)
	kind := 6666

	// Simulated UI spec (from Claude)
	uiSpec := `{"layout":"card","elements":[{"type":"heading","value":"Note Feed"}]}`

	tags := [][]string{
		{"request", string(requestJSON)},
		{"e", "20925e1a536eb6af2285394395d2d372617f201ed1914909e73afc379faf566a"},
		{"p", "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331"},
		{"alt", "Malleable UI specification"},
		{"t", "malleable-ui"},
	}

	event := &Event{
		PubKey:    pubkey,
		CreatedAt: createdAt,
		Kind:      kind,
		Tags:      tags,
		Content:   uiSpec,
	}

	id := computeEventID(event)
	t.Logf("Computed ID: %s", id)

	// Show exact serialization
	arr := []interface{}{0, pubkey, createdAt, kind, tags, uiSpec}
	jsonBytes, _ := json.Marshal(arr)
	t.Logf("Serialization length: %d", len(jsonBytes))
	t.Logf("Serialization (first 1000): %s", truncate(string(jsonBytes), 1000))

	// Try to verify: does our ID match what we'd get from SHA256?
	hash := sha256.Sum256(jsonBytes)
	verifyID := hex.EncodeToString(hash[:])
	if id != verifyID {
		t.Errorf("ID mismatch in our own code!\n  computeEventID: %s\n  direct SHA256:  %s", id, verifyID)
	}
}
