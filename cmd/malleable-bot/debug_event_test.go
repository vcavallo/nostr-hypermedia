package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestVerifyWithNak(t *testing.T) {
	// Create an event exactly like the bot does and verify with nak

	// Simulate a request event
	requestEvent := Event{
		ID:        "2a73d74d8fd71fb27b90612d12206ea51bcf14ee6b5618f1411225d0a49183c8",
		PubKey:    "2efaa715bbb46dd5be6b7da8d7700266d11674b913b8178addb5c2e63d987331",
		CreatedAt: 1764785079,
		Kind:      5666,
		Tags: [][]string{
			{"i", "Create a feed", "text"},
			{"output", "application/json"},
			{"p", "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"},
		},
		Content: "",
		Sig:     "ebffc54ea3405f39117266d17a097a7520d574137eb5916f3235d13fd56cc53a6cc79c32f8aa4fc90e28de34f35dca6295eccac6ce9a1edf984b37a5eab1aa88",
	}

	requestJSON, _ := json.Marshal(requestEvent)
	t.Logf("Request JSON: %s", string(requestJSON))

	// Build tags exactly like createJobResult
	tags := [][]string{
		{"request", string(requestJSON)},
		{"e", requestEvent.ID},
		{"p", requestEvent.PubKey},
		{"alt", "Malleable UI specification"},
		{"t", "malleable-ui"},
	}

	// Simple content
	content := `{"layout":"card"}`

	// Create event
	event := Event{
		PubKey:    "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",
		CreatedAt: 1764785086,
		Kind:      6666,
		Tags:      tags,
		Content:   content,
	}

	// Compute ID
	event.ID = computeEventID(&event)
	event.Sig = "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"

	t.Logf("Computed ID: %s", event.ID)

	// Marshal the event
	eventJSON, _ := json.Marshal(event)
	t.Logf("Event JSON: %s", truncate(string(eventJSON), 500))

	// Use nak to verify what ID it computes
	cmd := exec.Command("nak", "verify")
	cmd.Stdin = strings.NewReader(string(eventJSON))
	output, err := cmd.CombinedOutput()
	t.Logf("nak verify output: %s (err: %v)", string(output), err)

	// Now manually compute what nak should get
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	jsonBytes, _ := json.Marshal(serialized)
	hash := sha256.Sum256(jsonBytes)
	manualID := hex.EncodeToString(hash[:])

	t.Logf("Our serialization: %s", truncate(string(jsonBytes), 500))
	t.Logf("Manual ID: %s", manualID)

	// Now create with nak and compare
	nsecBech := "nsec1ahys6ph7u9mp2g5us5ndcqzajk0y4uaacz6gc4mke9gmetldajzsy4y8e6"

	// Build tag args
	tagArgs := []string{"event", "--sec", nsecBech, "-k", "6666", "-c", content, "--ts", "1764785086"}
	for _, tag := range tags {
		tagArgs = append(tagArgs, "--tag", fmt.Sprintf("%s=%s", tag[0], strings.Join(tag[1:], ";")))
	}

	t.Logf("nak command args: %v", tagArgs)

	cmd2 := exec.Command("nak", tagArgs...)
	nakOutput, err := cmd2.CombinedOutput()
	if err != nil {
		t.Logf("nak event error: %v, output: %s", err, string(nakOutput))
	} else {
		t.Logf("nak event output: %s", truncate(string(nakOutput), 500))

		// Parse nak's event to get its ID
		var nakEvent map[string]interface{}
		if json.Unmarshal(nakOutput, &nakEvent) == nil {
			nakID := nakEvent["id"].(string)
			t.Logf("nak computed ID: %s", nakID)

			if nakID != event.ID {
				t.Errorf("ID MISMATCH! our=%s nak=%s", event.ID, nakID)
			}
		}
	}
}
