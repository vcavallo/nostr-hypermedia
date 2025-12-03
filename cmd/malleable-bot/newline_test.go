package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/gorilla/websocket"
)

func TestPublishEventWithNewlines(t *testing.T) {
	// Test publishing an event where the content has newlines (like Claude's JSON output)

	nsecHex := "edc90d06fee17615229c8526dc005d959e4af3bdc0b48c5776c951bcafedec85"
	privKeyBytes, _ := hex.DecodeString(nsecHex)
	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	pubKeyBytes := privKey.PubKey().SerializeCompressed()
	pubKey := hex.EncodeToString(pubKeyBytes[1:])

	// Content with actual newlines (like Claude's formatted JSON)
	content := "{\n  \"layout\": \"card\",\n  \"title\": \"Test\"\n}"

	event := &Event{
		PubKey:    pubKey,
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   content,
	}

	// Compute event ID
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
	t.Logf("Serialization bytes: %v", jsonBytes)

	hash := sha256.Sum256(jsonBytes)
	event.ID = hex.EncodeToString(hash[:])
	t.Logf("Event ID: %s", event.ID)

	// Sign
	idBytes, _ := hex.DecodeString(event.ID)
	sig, _ := schnorr.Sign(privKey, idBytes)
	event.Sig = hex.EncodeToString(sig.Serialize())

	// Connect to relay
	relay := "wss://relay.damus.io"
	conn, _, err := websocket.DefaultDialer.Dial(relay, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send
	eventJSON, _ := json.Marshal(event)
	msg := fmt.Sprintf(`["EVENT",%s]`, eventJSON)
	t.Logf("Sending: %s", msg)

	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, response, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	t.Logf("Response: %s", string(response))

	var resp []interface{}
	json.Unmarshal(response, &resp)
	if len(resp) >= 3 && resp[0] == "OK" {
		if success, ok := resp[2].(bool); ok && success {
			t.Log("Event with newlines accepted!")
		} else {
			reason := ""
			if len(resp) >= 4 {
				reason = resp[3].(string)
			}
			t.Errorf("Event rejected: %s", reason)
		}
	}
}

func TestPublishEventWithRequestTag(t *testing.T) {
	// Test publishing an event with a request tag containing JSON (like DVM events)

	nsecHex := "edc90d06fee17615229c8526dc005d959e4af3bdc0b48c5776c951bcafedec85"
	privKeyBytes, _ := hex.DecodeString(nsecHex)
	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	pubKeyBytes := privKey.PubKey().SerializeCompressed()
	pubKey := hex.EncodeToString(pubKeyBytes[1:])

	// Create a fake request event and serialize it
	requestEvent := &Event{
		ID:        "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		PubKey:    "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		CreatedAt: 1700000000,
		Kind:      5666,
		Tags:      [][]string{{"i", "test input", "text"}},
		Content:   "",
		Sig:       "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
	}
	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request JSON: %s", string(requestJSON))

	// Content with newlines
	content := "{\n  \"layout\": \"card\"\n}"

	event := &Event{
		PubKey:    pubKey,
		CreatedAt: time.Now().Unix(),
		Kind:      6666,
		Tags: [][]string{
			{"request", string(requestJSON)},
			{"e", requestEvent.ID},
			{"p", requestEvent.PubKey},
			{"t", "test"},
		},
		Content: content,
	}

	// Compute event ID
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Serialization (first 500): %s", truncate(string(jsonBytes), 500))

	hash := sha256.Sum256(jsonBytes)
	event.ID = hex.EncodeToString(hash[:])
	t.Logf("Event ID: %s", event.ID)

	// Sign
	idBytes, _ := hex.DecodeString(event.ID)
	sig, _ := schnorr.Sign(privKey, idBytes)
	event.Sig = hex.EncodeToString(sig.Serialize())

	// Connect to relay
	relay := "wss://relay.damus.io"
	conn, _, err := websocket.DefaultDialer.Dial(relay, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send
	eventJSON, _ := json.Marshal(event)
	msg := fmt.Sprintf(`["EVENT",%s]`, eventJSON)
	t.Logf("Sending (first 500): %s", truncate(msg, 500))

	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, response, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	t.Logf("Response: %s", string(response))

	var resp []interface{}
	json.Unmarshal(response, &resp)
	if len(resp) >= 3 && resp[0] == "OK" {
		if success, ok := resp[2].(bool); ok && success {
			t.Log("DVM event accepted!")
		} else {
			reason := ""
			if len(resp) >= 4 {
				reason = resp[3].(string)
			}
			t.Errorf("DVM event rejected: %s", reason)
		}
	}
}
