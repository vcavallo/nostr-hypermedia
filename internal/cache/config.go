package cache

import "time"

// CacheConfig holds cache TTL configuration
type CacheConfig struct {
	ProfileTTL           time.Duration
	ProfileNotFoundTTL   time.Duration
	ContactTTL           time.Duration
	RelayListTTL         time.Duration
	RelayListNotFoundTTL time.Duration
	AvatarTTL            time.Duration
	AvatarFailTTL        time.Duration
	LinkPreviewTTL       time.Duration
	LinkPreviewFailTTL   time.Duration
	SessionTTL           time.Duration
	PendingConnTTL       time.Duration
	SearchResultTTL      time.Duration
	RateLimitWindow      time.Duration
	ThreadTTL            time.Duration
	NotificationReadTTL  time.Duration
	NotificationCacheTTL time.Duration
	WalletInfoTTL        time.Duration
}

// DefaultCacheConfig returns sensible defaults
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		ProfileTTL:           1 * time.Hour,   // Profiles rarely change hourly
		ProfileNotFoundTTL:   30 * time.Second, // Short TTL allows lazy-loading to retry after page load timeout
		ContactTTL:           10 * time.Minute, // Increased to reduce refetches
		RelayListTTL:         1 * time.Hour,
		RelayListNotFoundTTL: 5 * time.Minute,
		AvatarTTL:            1 * time.Hour, // Longer TTL - avatars rarely change
		AvatarFailTTL:        5 * time.Minute,
		LinkPreviewTTL:       24 * time.Hour,
		LinkPreviewFailTTL:   1 * time.Hour,
		SessionTTL:           24 * time.Hour,
		PendingConnTTL:       10 * time.Minute,
		SearchResultTTL:      2 * time.Minute,
		RateLimitWindow:      1 * time.Minute,
		ThreadTTL:            3 * time.Minute,
		NotificationReadTTL:  30 * 24 * time.Hour, // 30 days
		NotificationCacheTTL: 1 * time.Hour,       // Notifications cache (incremental updates)
		WalletInfoTTL:        5 * time.Minute,     // Balance/transactions refresh interval
	}
}
