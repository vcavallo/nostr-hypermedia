package nostr

import (
	"encoding/hex"
	"log/slog"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"nostr-server/internal/types"
)

// ValidateEventSignature verifies Schnorr signature for a Nostr event
func ValidateEventSignature(evt *types.Event) bool {
	if len(evt.Sig) != 128 || len(evt.PubKey) != 64 {
		return false
	}

	sigBytes, err := hex.DecodeString(evt.Sig)
	if err != nil {
		return false
	}
	pubKeyBytes, err := hex.DecodeString(evt.PubKey)
	if err != nil {
		return false
	}
	idBytes, err := hex.DecodeString(evt.ID)
	if err != nil {
		return false
	}

	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return false
	}
	pubKey, err := schnorr.ParsePubKey(pubKeyBytes)
	if err != nil {
		return false
	}

	return sig.Verify(idBytes, pubKey)
}

// ParseEventFromInterface converts raw websocket data to Event (avoids JSON re-encoding)
func ParseEventFromInterface(data interface{}) (types.Event, bool) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return types.Event{}, false
	}

	evt := types.Event{}

	if id, ok := m["id"].(string); ok {
		evt.ID = id
	}
	if pk, ok := m["pubkey"].(string); ok {
		evt.PubKey = pk
	}
	if createdAt, ok := m["created_at"].(float64); ok {
		evt.CreatedAt = int64(createdAt)
	}
	if kind, ok := m["kind"].(float64); ok {
		evt.Kind = int(kind)
	}
	if content, ok := m["content"].(string); ok {
		evt.Content = content
	}
	if sig, ok := m["sig"].(string); ok {
		evt.Sig = sig
	}

	if tags, ok := m["tags"].([]interface{}); ok {
		evt.Tags = make([][]string, 0, len(tags))
		for _, tag := range tags {
			if tagArr, ok := tag.([]interface{}); ok {
				strTag := make([]string, 0, len(tagArr))
				for _, elem := range tagArr {
					if s, ok := elem.(string); ok {
						strTag = append(strTag, s)
					}
				}
				evt.Tags = append(evt.Tags, strTag)
			}
		}
	}

	// Validate signature if present
	if evt.Sig != "" && !ValidateEventSignature(&evt) {
		slog.Warn("event signature validation failed", "event_id", ShortID(evt.ID))
		return types.Event{}, false
	}

	return evt, evt.ID != ""
}

// ShortID truncates ID/pubkey to 12 chars for logging
func ShortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}
