package main

import (
	"encoding/json"
	"testing"
)

func TestNakEmojiEvent(t *testing.T) {
	// nak produced this event with emoji:
	// {"kind":1,"id":"02c31c82cf7d8017c823bf119dbabd3ce22e9d66bb27277b064ca46b4196d93e",
	//  "pubkey":"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
	//  "created_at":1764776381,"tags":[],
	//  "content":"{\"label\":\"🍦 Vanilla\"}",
	//  "sig":"02dbd2eea940b039e81cb0a50c48de287373b3a87d76e8ff5ed47379ce7c45ab23f0d19d9bdf0f7fc02998d46b02ff0b6d52466fd0aea3124219ea7ee5e5e74c"}

	nakID := "02c31c82cf7d8017c823bf119dbabd3ce22e9d66bb27277b064ca46b4196d93e"
	pubkey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	createdAt := int64(1764776381)
	content := `{"label":"🍦 Vanilla"}`

	event := &Event{
		PubKey:    pubkey,
		CreatedAt: createdAt,
		Kind:      1,
		Tags:      [][]string{},
		Content:   content,
	}

	ourID := computeEventID(event)
	t.Logf("nak ID: %s", nakID)
	t.Logf("our ID: %s", ourID)

	// Show what we're serializing
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Our serialization: %s", string(jsonBytes))
	t.Logf("Our serialization bytes: %v", jsonBytes)

	if ourID != nakID {
		t.Errorf("ID mismatch! This is the emoji bug.")
	} else {
		t.Log("IDs match! Emoji handling is correct.")
	}
}
