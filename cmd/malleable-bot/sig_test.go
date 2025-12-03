package main

import (
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func TestSignatureVerification(t *testing.T) {
	// Use the same key as our bot
	privKeyHex := "edc90d06fee17615229c8526dc005d959e4af3bdc0b48c5776c951bcafedec85"
	privKeyBytes, _ := hex.DecodeString(privKeyHex)

	privateKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	publicKey := privateKey.PubKey()

	// Get the pubkey in the format Nostr uses (x-only, no prefix)
	pubKeyBytes := publicKey.SerializeCompressed()[1:] // Remove 02/03 prefix
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	t.Logf("Private key: %s", privKeyHex)
	t.Logf("Public key (from SerializeCompressed[1:]): %s", pubKeyHex)

	// Expected pubkey from nak
	expectedPubKey := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"
	if pubKeyHex != expectedPubKey {
		t.Errorf("Pubkey mismatch!\n  got:      %s\n  expected: %s", pubKeyHex, expectedPubKey)
	}

	// Sign a known message
	message := []byte("7f431bf32dcabd8630b529e25754bfb37b84b1e2a2bf01531b5db0d21180ba9f")
	messageBytes, _ := hex.DecodeString(string(message))

	sig, err := schnorr.Sign(privateKey, messageBytes)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	sigHex := hex.EncodeToString(sig.Serialize())
	t.Logf("Signature: %s", sigHex)

	// Verify the signature
	verified := sig.Verify(messageBytes, publicKey)
	if !verified {
		t.Error("Signature verification failed!")
	} else {
		t.Log("Signature verification passed!")
	}

	// Also verify using parsed signature
	parsedSig, err := schnorr.ParseSignature(sig.Serialize())
	if err != nil {
		t.Fatalf("ParseSignature failed: %v", err)
	}
	verified2 := parsedSig.Verify(messageBytes, publicKey)
	if !verified2 {
		t.Error("Parsed signature verification failed!")
	} else {
		t.Log("Parsed signature verification passed!")
	}
}

func TestCompareSignatureWithNak(t *testing.T) {
	// nak produced this signature for the same event:
	// "sig":"ca1ad40f52d92c011452f76a24c760b24cd69db3d70839db32e44c61f3fbc98d0a9363a6666ec061b97167f13a19715eaeda22fef60694c78335f0644dfcd912"

	// Verify we can verify nak's signature
	nakSig := "ca1ad40f52d92c011452f76a24c760b24cd69db3d70839db32e44c61f3fbc98d0a9363a6666ec061b97167f13a19715eaeda22fef60694c78335f0644dfcd912"
	eventID := "7f431bf32dcabd8630b529e25754bfb37b84b1e2a2bf01531b5db0d21180ba9f"
	pubKeyHex := "bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec"

	// Parse pubkey
	pubKeyBytes, _ := hex.DecodeString(pubKeyHex)
	pubKey, err := schnorr.ParsePubKey(pubKeyBytes)
	if err != nil {
		t.Fatalf("Failed to parse pubkey: %v", err)
	}

	// Parse signature
	sigBytes, _ := hex.DecodeString(nakSig)
	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		t.Fatalf("Failed to parse signature: %v", err)
	}

	// Parse message
	msgBytes, _ := hex.DecodeString(eventID)

	// Verify
	verified := sig.Verify(msgBytes, pubKey)
	if !verified {
		t.Error("Failed to verify nak's signature!")
	} else {
		t.Log("Successfully verified nak's signature!")
	}
}
