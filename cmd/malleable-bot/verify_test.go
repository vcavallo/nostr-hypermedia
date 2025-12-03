package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func TestFullEventCreationAndVerification(t *testing.T) {
	// Use a test private key
	privKeyHex := "edc90d06fee17615229c8526dc005d959e4af3bdc0b48c5776c951bcafedec85"
	privKeyBytes, _ := hex.DecodeString(privKeyHex)
	privateKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	publicKey := privateKey.PubKey()
	pubKeyBytes := publicKey.SerializeCompressed()[1:]
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	t.Logf("Public key: %s", pubKeyHex)

	// Create an event
	event := &Event{
		PubKey:    pubKeyHex,
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{{"e", "abc123", "", "reply"}, {"p", "def456"}},
		Content:   `{"test":"json content"}`,
	}

	// Compute ID
	event.ID = computeEventID(event)
	t.Logf("Event ID: %s", event.ID)

	// Sign
	idBytes, _ := hex.DecodeString(event.ID)
	sig, err := schnorr.Sign(privateKey, idBytes)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	event.Sig = hex.EncodeToString(sig.Serialize())
	t.Logf("Signature: %s", event.Sig)

	// Verify the signature
	sigBytes, _ := hex.DecodeString(event.Sig)
	parsedSig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		t.Fatalf("Failed to parse signature: %v", err)
	}

	verified := parsedSig.Verify(idBytes, publicKey)
	if !verified {
		t.Error("Signature verification failed!")
	} else {
		t.Log("Signature verification passed!")
	}

	// Now verify the event ID computation matches what relays expect
	// Serialize manually
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

	// Compute hash
	hash := sha256.Sum256([]byte(serialized))
	computedID := hex.EncodeToString(hash[:])

	if computedID != event.ID {
		t.Errorf("ID mismatch: computed=%s, event.ID=%s", computedID, event.ID)
	}

	// Output the full event JSON for inspection
	eventJSON, _ := json.MarshalIndent(event, "", "  ")
	t.Logf("Full event:\n%s", string(eventJSON))
}

func TestVerifyAgainstNakFormat(t *testing.T) {
	// This test verifies our serialization matches what nak/other clients expect
	// The key thing is: tags must be [] not null when empty

	event := &Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{}, // Empty tags
		Content:   "hello",
	}

	// Compute ID
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

	// Tags should be [] not null
	if string(tagsJSON) != "[]" {
		t.Errorf("Empty tags should serialize to [], got: %s", string(tagsJSON))
	}

	// The serialization should look exactly like:
	// [0,"pubkey",1700000000,1,[],"hello"]
	expected := `[0,"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",1700000000,1,[],"hello"]`
	if serialized != expected {
		t.Errorf("Serialization mismatch:\ngot:      %s\nexpected: %s", serialized, expected)
	}
}
