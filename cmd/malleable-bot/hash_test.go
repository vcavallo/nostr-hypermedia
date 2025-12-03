package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHashComparison(t *testing.T) {
	// Test that our Go sha256 produces the same output as sha256sum
	input := `[0,"bbde6a0e8847e1cdb2ba5ec021cc949eb3cef125b8304a748fe11c0407990eec",1700000000,1,[],"hello"]`

	hash := sha256.Sum256([]byte(input))
	goHash := hex.EncodeToString(hash[:])

	// sha256sum output: 7b3e3c855486c0483791b55157b096ebcd3271b1dbc66514725256abea63bdbb
	expectedHash := "7b3e3c855486c0483791b55157b096ebcd3271b1dbc66514725256abea63bdbb"

	t.Logf("Input: %s", input)
	t.Logf("Go hash: %s", goHash)
	t.Logf("Expected: %s", expectedHash)

	if goHash != expectedHash {
		t.Errorf("Hash mismatch!")
	} else {
		t.Log("Hash matches!")
	}
}
