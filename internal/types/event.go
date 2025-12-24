// Package types provides shared type definitions used across internal packages.
package types

// Event represents a Nostr event (NIP-01)
type Event struct {
	ID         string     `json:"id"`
	PubKey     string     `json:"pubkey"`
	CreatedAt  int64      `json:"created_at"`
	Kind       int        `json:"kind"`
	Tags       [][]string `json:"tags"`
	Content    string     `json:"content"`
	Sig        string     `json:"sig"`
	RelaysSeen []string   `json:"-"`
}

// Filter represents a Nostr subscription filter (NIP-01)
type Filter struct {
	IDs     []string
	Authors []string
	Kinds   []int
	Limit   int
	Since   *int64
	Until   *int64
	PTags   []string // #p tag filter (mentions)
	ATags   []string // #a tag filter (addressable events)
	DTags   []string // #d tag filter (d-tag for addressable events)
	KTags   []string // #k tag filter (kind references, used for NIP-89)
	TTags   []string // #t tag filter (hashtags/topics)
	Search  string   // NIP-50 search query
}

// NostrMessage represents a raw Nostr protocol message
type NostrMessage []interface{}
