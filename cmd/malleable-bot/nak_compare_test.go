package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCompareWithNakOutput(t *testing.T) {
	// nak produced this event:
	// {"kind":1,"id":"7f431bf32dcabd8630b529e25754bfb37b84b1e2a2bf01531b5db0d21180ba9f",
	//  "pubkey":"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
	//  "created_at":1764775888,"tags":[],
	//  "content":"{\"layout\":\"card\",\"elements\":[{\"type\":\"heading\",\"value\":\"Test\"}]}",
	//  "sig":"ca1ad40f52d92c011452f76a24c760b24cd69db3d70839db32e44c61f3fbc98d0a9363a6666ec061b97167f13a19715eaeda22fef60694c78335f0644dfcd912"}

	// Let's verify we compute the same ID
	nakID := "7f431bf32dcabd8630b529e25754bfb37b84b1e2a2bf01531b5db0d21180ba9f"
	pubkey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	createdAt := int64(1764775888)
	content := `{"layout":"card","elements":[{"type":"heading","value":"Test"}]}`

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

	if ourID != nakID {
		// Debug: show what we're hashing
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
		t.Logf("Our serialization: %s", serialized)

		// What nak probably uses
		nakSerialized := fmt.Sprintf(
			`[0,"%s",%d,%d,[],%s]`,
			pubkey,
			createdAt,
			1,
			contentJSON,
		)
		t.Logf("Assumed nak serialization: %s", nakSerialized)

		// Verify hash of nak's assumed serialization
		nakHash := sha256.Sum256([]byte(nakSerialized))
		t.Logf("Hash of assumed nak serialization: %s", hex.EncodeToString(nakHash[:]))

		t.Errorf("ID mismatch!")
	} else {
		t.Log("IDs match!")
	}
}
