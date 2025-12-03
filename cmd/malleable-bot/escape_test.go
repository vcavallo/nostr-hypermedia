package main

import (
	"encoding/json"
	"testing"
)

func TestUnicodeEscaping(t *testing.T) {
	// Test various special characters
	testCases := []struct {
		name    string
		input   string
		wantHex bool
	}{
		{"simple", "hello", false},
		{"quotes", `say "hello"`, false},
		{"backslash", `path\to\file`, false},
		{"newline", "line1\nline2", false},
		{"tab", "col1\tcol2", false},
		{"unicode_basic", "hello 🍦", false},
		{"unicode_zero_width", "test\u200btest", true}, // zero-width space
		{"unicode_nbsp", "test\u00a0test", true},       // non-breaking space
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal as part of an array (like Nostr serialization)
			arr := []interface{}{0, "pubkey", 123, 1, [][]string{}, tc.input}
			jsonBytes, err := json.Marshal(arr)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			t.Logf("Input: %q", tc.input)
			t.Logf("JSON: %s", string(jsonBytes))
			t.Logf("Bytes: %v", jsonBytes)
		})
	}
}

func TestClaudeResponseEscaping(t *testing.T) {
	// Simulate a Claude response that might have formatting
	responses := []string{
		// Normal response
		`{"layout":"card","elements":[]}`,
		// Response with literal newlines (Claude might format JSON)
		"{\n  \"layout\": \"card\",\n  \"elements\": []\n}",
		// Response with unicode
		`{"layout":"card","elements":[{"type":"text","value":"Hello 👋"}]}`,
	}

	for i, resp := range responses {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			// Show raw bytes
			t.Logf("Response bytes: %v", []byte(resp))

			// Marshal in the Nostr format
			arr := []interface{}{0, "pubkey", int64(123), 1, [][]string{}, resp}
			jsonBytes, _ := json.Marshal(arr)
			t.Logf("Serialized: %s", string(jsonBytes))
		})
	}
}

func TestControlCharacters(t *testing.T) {
	// Test that control characters are properly escaped
	// This is important because Claude might include characters we don't expect

	content := "test\x00test" // null byte
	arr := []interface{}{0, "pubkey", int64(123), 1, [][]string{}, content}
	_, err := json.Marshal(arr)
	if err != nil {
		t.Logf("Null byte causes error: %v", err)
	}

	// Test various control chars
	for i := 0; i < 32; i++ {
		content := "test" + string(rune(i)) + "test"
		arr := []interface{}{0, "pubkey", int64(123), 1, [][]string{}, content}
		jsonBytes, err := json.Marshal(arr)
		if err != nil {
			t.Logf("Control char %d (0x%02x) causes error: %v", i, i, err)
		} else {
			// Check if it's escaped as \uXXXX
			if i < 32 && i != 8 && i != 9 && i != 10 && i != 12 && i != 13 {
				t.Logf("Control char %d (0x%02x): %s", i, i, string(jsonBytes))
			}
		}
	}
}
