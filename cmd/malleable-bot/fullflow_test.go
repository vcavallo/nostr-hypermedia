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

func TestFullDVMFlow(t *testing.T) {
	// This test recreates the exact flow from the failing logs

	nsecHex := "edc90d06fee17615229c8526dc005d959e4af3bdc0b48c5776c951bcafedec85"
	privKeyBytes, _ := hex.DecodeString(nsecHex)
	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	pubKeyBytes := privKey.PubKey().SerializeCompressed()
	botPubKey := hex.EncodeToString(pubKeyBytes[1:])

	t.Logf("Bot public key: %s", botPubKey)

	// Step 1: Simulate receiving a DVM request (exactly as shown in logs)
	incomingJSON := `{"id":"9d8943cf00838ad7c78a1fb449d368e7d1a3a7527eedb8964a589b9020183fd3","pubkey":"2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331","created_at":1764784573,"kind":5666,"tags":[["i","Create a feed that uses the NoteCard component (nevent1qqsypznnjmgrf3h0jq26jsx6sql7hw6htlsgm44p5pygsw7guuqv0dszyzaau6sw3pr7rndjhf0vqgwvjj0t8nh3ykurqjn53ls3cpq8ny8wcqcyqqqp5zs9au6vc) for each note","text"],["output","application/json"],["p","bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"]],"content":"","sig":"af240c4c55718ca2777b76c311ae246efe87a3958acb60c704fc8bd5e8264de357a59aa9becc50c541aea1fedd886fde09e91c02a17903c3d1f9add773dd8e59"}`

	var requestEvent Event
	if err := json.Unmarshal([]byte(incomingJSON), &requestEvent); err != nil {
		t.Fatalf("Failed to parse incoming event: %v", err)
	}
	t.Logf("Parsed request event ID: %s", requestEvent.ID)

	// Step 2: Create UI spec (simulating Claude's response with actual newlines)
	uiSpec := `{
  "layout": "card",
  "title": "Notes Feed",
  "elements": [
    {
      "type": "heading",
      "value": "Recent Notes"
    },
    {
      "type": "query",
      "filter": {
        "kinds": [1],
        "limit": 20
      },
      "as": "notes",
      "children": [
        {
          "type": "foreach",
          "items": "$.notes",
          "as": "note",
          "children": [
            {
              "type": "component",
              "ref": "nevent1qqsypznnjmgrf3h0jq26jsx6sql7hw6htlsgm44p5pygsw7guuqv0dszyzaau6sw3pr7rndjhf0vqgwvjj0t8nh3ykurqjn53ls3cpq8ny8wcqcyqqqp5zs9au6vc",
              "props": {
                "author": "$.note.pubkey",
                "time": "$.note.created_at",
                "content": "$.note.content"
              }
            }
          ]
        }
      ]
    }
  ]
}`

	t.Logf("UI spec length: %d", len(uiSpec))

	// Step 3: Create the job result (mimicking createJobResult)
	now := time.Now().Unix()

	// Marshal the request event for the "request" tag
	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request JSON for tag: %s", truncate(string(requestJSON), 200))

	tags := [][]string{
		{"request", string(requestJSON)},
		{"e", requestEvent.ID},
		{"p", requestEvent.PubKey},
		{"alt", "Malleable UI specification"},
		{"t", "malleable-ui"},
		{"t", "layout:card"},
	}

	// Copy input tags
	for _, tag := range requestEvent.Tags {
		if len(tag) >= 1 && tag[0] == "i" {
			tags = append(tags, tag)
		}
	}

	event := &Event{
		PubKey:    botPubKey,
		CreatedAt: now,
		Kind:      6666,
		Tags:      tags,
		Content:   uiSpec,
	}

	// Step 4: Compute event ID
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	t.Logf("Serialization length: %d", len(jsonBytes))
	t.Logf("Serialization (first 500): %s", truncate(string(jsonBytes), 500))

	hash := sha256.Sum256(jsonBytes)
	event.ID = hex.EncodeToString(hash[:])
	t.Logf("Computed event ID: %s", event.ID)

	// Step 5: Sign
	idBytes, _ := hex.DecodeString(event.ID)
	sig, _ := schnorr.Sign(privKey, idBytes)
	event.Sig = hex.EncodeToString(sig.Serialize())

	// Step 6: Marshal the full event (what gets sent to relay)
	eventJSON, _ := json.Marshal(event)
	t.Logf("Event JSON length: %d", len(eventJSON))

	// Step 7: Verify by parsing and re-computing (like relay does)
	var parsedEvent Event
	json.Unmarshal(eventJSON, &parsedEvent)

	relaySerialized := []interface{}{
		0,
		parsedEvent.PubKey,
		parsedEvent.CreatedAt,
		parsedEvent.Kind,
		parsedEvent.Tags,
		parsedEvent.Content,
	}
	relayJSON, _ := json.Marshal(relaySerialized)
	relayHash := sha256.Sum256(relayJSON)
	relayID := hex.EncodeToString(relayHash[:])

	t.Logf("Relay computed ID: %s", relayID)

	if event.ID != relayID {
		t.Errorf("ID MISMATCH! our=%s, relay=%s", event.ID, relayID)
		t.Logf("Our serialization:   %s", truncate(string(jsonBytes), 500))
		t.Logf("Relay serialization: %s", truncate(string(relayJSON), 500))
		return
	}

	t.Log("IDs match locally, trying to publish...")

	// Step 8: Actually publish to relay
	relay := "wss://relay.damus.io"
	conn, _, err := websocket.DefaultDialer.Dial(relay, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	msg := fmt.Sprintf(`["EVENT",%s]`, eventJSON)
	t.Logf("Sending (first 500): %s", truncate(msg, 500))

	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

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
			t.Log("SUCCESS: Full DVM flow event accepted!")
		} else {
			reason := ""
			if len(resp) >= 4 {
				reason = resp[3].(string)
			}
			t.Errorf("FAILED: Event rejected: %s", reason)
		}
	}
}
