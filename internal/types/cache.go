package types

import (
	"context"
	"time"
)

// CachedProfile wraps profile data for serialization
type CachedProfile struct {
	Profile   *ProfileInfo `json:"profile,omitempty"`
	FetchedAt int64        `json:"fetched_at"`
	NotFound  bool         `json:"not_found"`
}

// CachedContacts wraps contact list for serialization
type CachedContacts struct {
	Pubkeys   []string `json:"pubkeys"`
	FetchedAt int64    `json:"fetched_at"`
}

// CachedRelayList wraps relay list for serialization
type CachedRelayList struct {
	RelayList *RelayList `json:"relay_list,omitempty"`
	FetchedAt int64      `json:"fetched_at"`
	NotFound  bool       `json:"not_found"`
}

// CachedAvatarResult wraps avatar validation result
type CachedAvatarResult struct {
	Valid     bool  `json:"valid"`
	CheckedAt int64 `json:"checked_at"`
}

// CachedLinkPreview wraps link preview data
type CachedLinkPreview struct {
	Preview   *LinkPreview `json:"preview"`
	FetchedAt int64        `json:"fetched_at"`
}

// CachedSession wraps bunker session for storage
type CachedSession struct {
	ID                 string   `json:"id"`
	ClientPrivKey      string   `json:"client_priv_key"`       // hex encoded
	ClientPubKey       string   `json:"client_pub_key"`        // hex encoded
	RemoteSignerPubKey string   `json:"remote_signer_pub_key"` // hex encoded
	UserPubKey         string   `json:"user_pub_key"`          // hex encoded
	Relays             []string `json:"relays"`
	Secret             string   `json:"secret"`
	ConversationKey    string   `json:"conversation_key"` // hex encoded
	Connected          bool     `json:"connected"`
	CreatedAt          int64    `json:"created_at"`
	// User's NIP-65 relay list
	UserRelayListRead  []string `json:"user_relay_list_read,omitempty"`
	UserRelayListWrite []string `json:"user_relay_list_write,omitempty"`
	// Cached user data
	FollowingPubkeys   []string `json:"following_pubkeys,omitempty"`
	BookmarkedEventIDs []string `json:"bookmarked_event_ids,omitempty"`
	ReactedEventIDs    []string `json:"reacted_event_ids,omitempty"`
	RepostedEventIDs   []string `json:"reposted_event_ids,omitempty"`
	ZappedEventIDs     []string `json:"zapped_event_ids,omitempty"`
	MutedPubkeys       []string `json:"muted_pubkeys,omitempty"`
	MutedEventIDs      []string `json:"muted_event_ids,omitempty"`
	MutedHashtags      []string `json:"muted_hashtags,omitempty"`
	MutedWords         []string `json:"muted_words,omitempty"`
	// NWC wallet config
	NWCWalletPubKey    string `json:"nwc_wallet_pubkey,omitempty"`    // hex encoded
	NWCRelay           string `json:"nwc_relay,omitempty"`
	NWCSecret          string `json:"nwc_secret,omitempty"`           // hex encoded
	NWCClientPubKey    string `json:"nwc_client_pubkey,omitempty"`    // hex encoded
	NWCConversationKey string `json:"nwc_conversation_key,omitempty"` // hex encoded (NIP-44)
	NWCNip04SharedKey  string `json:"nwc_nip04_shared_key,omitempty"` // hex encoded (NIP-04)
}

// CachedPendingConnection wraps pending connection for storage
type CachedPendingConnection struct {
	Secret             string   `json:"secret"`
	ClientPrivKey      string   `json:"client_priv_key"`             // hex encoded
	ClientPubKey       string   `json:"client_pub_key"`              // hex encoded
	Relays             []string `json:"relays"`
	ConversationKey    string   `json:"conversation_key,omitempty"`  // hex encoded
	CreatedAt          int64    `json:"created_at"`
	RemoteSignerPubKey string   `json:"remote_signer_pub_key,omitempty"` // hex encoded
	UserPubKey         string   `json:"user_pub_key,omitempty"`          // hex encoded
	Connected          bool     `json:"connected"`
	// User's NIP-65 relay list
	UserRelayListRead  []string `json:"user_relay_list_read,omitempty"`
	UserRelayListWrite []string `json:"user_relay_list_write,omitempty"`
}

// CachedSearchResult wraps search results for storage
type CachedSearchResult struct {
	Query    string  `json:"query"`
	Kinds    []int   `json:"kinds,omitempty"`
	Events   []Event `json:"events"`
	CachedAt int64   `json:"cached_at"`
}

// CachedThread wraps thread data for storage
type CachedThread struct {
	RootEvent Event   `json:"root_event"`
	Replies   []Event `json:"replies"`
	CachedAt  int64   `json:"cached_at"`
}

// CachedNotifications wraps notification data for storage
type CachedNotifications struct {
	Notifications []CachedNotification `json:"notifications"`
	NewestSeen    int64                `json:"newest_seen"` // Timestamp of newest notification (for incremental fetch)
	CachedAt      int64                `json:"cached_at"`
}

// CachedNotification stores a notification event with its metadata
type CachedNotification struct {
	Event           Event  `json:"event"`
	Type            string `json:"type"`              // "reply", "mention", "reaction", "repost", "zap"
	TargetEventID   string `json:"target_event_id"`   // Event being replied to, reacted to, etc.
	ZapSenderPubkey string `json:"zap_sender_pubkey"` // For zaps: actual sender (not LNURL provider)
	ZapAmountSats   int64  `json:"zap_amount_sats"`   // For zaps: amount in satoshis
}

// CachedWalletInfo wraps wallet balance and transactions for storage
type CachedWalletInfo struct {
	Balance      string                    `json:"balance"`       // Formatted balance (e.g., "57,344")
	BalanceMsats int64                     `json:"balance_msats"` // Raw balance in millisatoshis
	Transactions []CachedWalletTransaction `json:"transactions"`
	CachedAt     int64                     `json:"cached_at"`
	Error        string                    `json:"error,omitempty"` // Error message if fetch failed
}

// CachedWalletTransaction represents a transaction for storage
type CachedWalletTransaction struct {
	Type        string `json:"type"`        // "incoming" or "outgoing"
	TypeIcon    string `json:"type_icon"`   // "↓" or "↑"
	Amount      string `json:"amount"`      // Formatted amount (e.g., "2,100")
	AmountMsats int64  `json:"amount_msats"`
	Description string `json:"description"`
	TimeAgo     string `json:"time_ago"`
	CreatedAt   int64  `json:"created_at"`
	// Zap context (if this is a zap transaction)
	IsZap          bool   `json:"is_zap,omitempty"`
	ZapPubkey      string `json:"zap_pubkey,omitempty"`       // Recipient (outgoing) or sender (incoming)
	ZapDisplayName string `json:"zap_display_name,omitempty"` // Display name of the pubkey
}

// CachedDVMResult wraps DVM query results for storage
type CachedDVMResult struct {
	EventRefs []CachedDVMEventRef `json:"event_refs"`
	CachedAt  int64               `json:"cached_at"`
}

// CachedDVMEventRef represents a cached event reference from DVM
type CachedDVMEventRef struct {
	Type     string `json:"type"`      // "e" or "a"
	ID       string `json:"id"`        // Event ID or address
	RelayURL string `json:"relay_url"` // Relay hint
}

// Cache store interfaces

// RateLimitStore defines the interface for rate limiting
type RateLimitStore interface {
	// Check returns (allowed, remaining, error)
	// If allowed is false, the action should be blocked
	Check(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error)
	// Increment adds a count to the rate limit bucket
	Increment(ctx context.Context, key string, window time.Duration) error
}

// SearchCacheStore defines the interface for search result caching
type SearchCacheStore interface {
	// Get retrieves cached search results
	Get(ctx context.Context, query string, kinds []int, limit int) ([]Event, bool, error)
	// Set stores search results
	Set(ctx context.Context, query string, kinds []int, limit int, events []Event, ttl time.Duration) error
}

// ThreadCacheStore defines the interface for thread caching
type ThreadCacheStore interface {
	// Get retrieves a cached thread by root event ID
	// Returns (thread, found, error)
	Get(ctx context.Context, rootEventID string) (*CachedThread, bool, error)
	// Set stores a thread in the cache
	Set(ctx context.Context, rootEventID string, thread *CachedThread, ttl time.Duration) error
}

// NotificationReadStore defines the interface for notification read state
type NotificationReadStore interface {
	// GetLastRead returns the timestamp when user last viewed notifications
	// Returns (timestamp, found, error) - timestamp is 0 if not found
	GetLastRead(ctx context.Context, pubkey string) (int64, bool, error)
	// SetLastRead updates the timestamp when user views notifications
	SetLastRead(ctx context.Context, pubkey string, timestamp int64) error
}

// NotificationCacheStore defines the interface for notification caching
// Supports incremental fetching: returns cached notifications + fetches new ones since cache time
type NotificationCacheStore interface {
	// Get retrieves cached notifications for a user
	// Returns (cached, found, error)
	Get(ctx context.Context, pubkey string) (*CachedNotifications, bool, error)
	// Set stores notifications in the cache
	Set(ctx context.Context, pubkey string, cached *CachedNotifications, ttl time.Duration) error
}

// DVMCacheStore defines the interface for DVM result caching
type DVMCacheStore interface {
	// Get retrieves cached DVM results
	// Returns (result, found, error)
	Get(ctx context.Context, key string) (*CachedDVMResult, bool, error)
	// Set stores DVM results
	Set(ctx context.Context, key string, result *CachedDVMResult, ttl time.Duration) error
}

// DVMMetaCacheStore defines the interface for DVM metadata caching
type DVMMetaCacheStore interface {
	// Get retrieves cached DVM metadata
	Get(ctx context.Context, pubkey string) (*DVMMetadata, bool, error)
	// Set stores DVM metadata
	Set(ctx context.Context, pubkey string, meta *DVMMetadata, ttl time.Duration) error
}

// WalletInfoCacheStore defines the interface for wallet info caching
type WalletInfoCacheStore interface {
	// Get retrieves cached wallet info for a session
	Get(ctx context.Context, sessionID string) (*CachedWalletInfo, bool, error)
	// Set stores wallet info
	Set(ctx context.Context, sessionID string, info *CachedWalletInfo, ttl time.Duration) error
}
