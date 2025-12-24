package types

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationMention  NotificationType = "mention"
	NotificationReply    NotificationType = "reply"
	NotificationReaction NotificationType = "reaction"
	NotificationRepost   NotificationType = "repost"
	NotificationZap      NotificationType = "zap"
)

// Notification represents a notification event with type information
type Notification struct {
	Event           Event
	Type            NotificationType
	TargetEventID   string // Event being reacted to/reposted/zapped
	ZapAmountSats   int64  // Zap amount (from zap request)
	ZapSenderPubkey string // Zap sender (from zap request)
}
