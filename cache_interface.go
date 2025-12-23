package main

import (
	"context"

	"nostr-server/internal/cache"
	"nostr-server/internal/types"
)

// Type aliases for internal/cache types
type CacheBackend = cache.CacheBackend
type CacheConfig = cache.CacheConfig

// DefaultCacheConfig wraps internal/cache.DefaultCacheConfig
func DefaultCacheConfig() CacheConfig {
	return cache.DefaultCacheConfig()
}

// SessionStore defines the interface for session management
type SessionStore interface {
	Get(ctx context.Context, sessionID string) (*BunkerSession, error)
	Set(ctx context.Context, session *BunkerSession) error
	Delete(ctx context.Context, sessionID string) error
}

// PendingConnStore defines the interface for pending NIP-46 connections
type PendingConnStore interface {
	Get(ctx context.Context, secret string) (*PendingConnection, error)
	Set(ctx context.Context, secret string, conn *PendingConnection) error
	Delete(ctx context.Context, secret string) error
}

// Type aliases for internal/types cache types
type CachedProfile = types.CachedProfile
type CachedContacts = types.CachedContacts
type CachedRelayList = types.CachedRelayList
type CachedAvatarResult = types.CachedAvatarResult
type CachedLinkPreview = types.CachedLinkPreview

type CachedSession = types.CachedSession
type CachedPendingConnection = types.CachedPendingConnection

type RateLimitStore = types.RateLimitStore
type SearchCacheStore = types.SearchCacheStore
type CachedSearchResult = types.CachedSearchResult
type ThreadCacheStore = types.ThreadCacheStore
type CachedThread = types.CachedThread
type NotificationReadStore = types.NotificationReadStore
type NotificationCacheStore = types.NotificationCacheStore
type CachedNotifications = types.CachedNotifications
type CachedNotification = types.CachedNotification

type CachedWalletInfo = types.CachedWalletInfo
type CachedWalletTransaction = types.CachedWalletTransaction
type DVMCacheStore = types.DVMCacheStore
type CachedDVMResult = types.CachedDVMResult
type CachedDVMEventRef = types.CachedDVMEventRef
type DVMMetaCacheStore = types.DVMMetaCacheStore
