package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"nostr-server/internal/config"
	"nostr-server/internal/nips"
	"nostr-server/internal/util"
)

// init registers all kind-specific data appliers.
// Each applier extracts kind-specific data from tags and applies it to HTMLEventItem.
func init() {
	// Kind 9735: Zap receipt
	RegisterKindDataApplier(9735, applyZapData)

	// Kind 30311: Live event/stream
	RegisterKindDataApplier(30311, applyLiveEventData)

	// Kind 9802: Highlight
	RegisterKindDataApplier(9802, applyHighlightData)

	// Kind 10003: Bookmark list
	RegisterKindDataApplier(10003, applyBookmarkData)

	// Kind 30402: Classified listing
	RegisterKindDataApplier(30402, applyClassifiedData)

	// Kind 22: Short-form video
	RegisterKindDataApplier(22, applyVideoData)

	// Kind 30: Long-form video
	RegisterKindDataApplier(30, applyVideoData)

	// Kind 1111: Comment (NIP-22)
	RegisterKindDataApplier(1111, applyCommentData)

	// Kind 31922: Date-based calendar event (NIP-52)
	RegisterKindDataApplier(31922, applyCalendarData)

	// Kind 31923: Time-based calendar event (NIP-52)
	RegisterKindDataApplier(31923, applyCalendarData)

	// Kind 1063: File metadata (NIP-94)
	RegisterKindDataApplier(1063, applyFileData)

	// Kind 30017: Marketplace stall (NIP-15)
	RegisterKindDataApplier(30017, applyStallData)

	// Kind 30018: Marketplace product (NIP-15)
	RegisterKindDataApplier(30018, applyProductData)

	// Kind 30315: User status (NIP-38)
	RegisterKindDataApplier(30315, applyStatusData)

	// Kind 34550: Community definition (NIP-72)
	RegisterKindDataApplier(34550, applyCommunityData)

	// Kind 30009: Badge definition (NIP-58)
	RegisterKindDataApplier(30009, applyBadgeDefinitionData)

	// Kind 8: Badge award (NIP-58)
	RegisterKindDataApplier(8, applyBadgeAwardData)

	// Kind 1984: Report (NIP-56)
	RegisterKindDataApplier(1984, applyReportData)

	// Kind 1311: Live chat message (NIP-53)
	RegisterKindDataApplier(1311, applyLiveChatData)

	// Kind 31925: Calendar RSVP (NIP-52)
	RegisterKindDataApplier(31925, applyRSVPData)

	// Kind 1985: Label (NIP-32)
	RegisterKindDataApplier(1985, applyLabelData)

	// Kind 30617: Repository announcement (NIP-34)
	RegisterKindDataApplier(30617, applyRepositoryData)

	// Kind 31989: Handler recommendation (NIP-89)
	RegisterKindDataApplier(31989, applyRecommendationData)

	// Kind 31990: Application handler (NIP-89)
	RegisterKindDataApplier(31990, applyHandlerData)

	// Kind 32123: Audio track (NOM - Nostr Open Media)
	RegisterKindDataApplier(32123, applyAudioData)
}

// applyZapData extracts zap information from tags and applies to item
func applyZapData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	zapInfo := parseZapReceipt(tags)
	if zapInfo == nil {
		return
	}

	item.ZapSenderPubkey = zapInfo.SenderPubkey
	item.ZapRecipientPubkey = zapInfo.RecipientPubkey
	item.ZapAmountSats = zapInfo.AmountMsats / 1000 // Convert msats to sats
	item.ZapComment = zapInfo.Comment
	item.ZappedEventID = zapInfo.ZappedEventID

	// Generate npubs
	if zapInfo.SenderPubkey != "" {
		senderNpub, _ := encodeBech32Pubkey(zapInfo.SenderPubkey)
		item.ZapSenderNpub = senderNpub
		item.ZapSenderNpubShort = formatNpubShort(senderNpub)
	}
	if zapInfo.RecipientPubkey != "" {
		recipientNpub, _ := encodeBech32Pubkey(zapInfo.RecipientPubkey)
		item.ZapRecipientNpub = recipientNpub
		item.ZapRecipientNpubShort = formatNpubShort(recipientNpub)
	}

	// Look up profiles from context
	if ctx != nil && ctx.Profiles != nil {
		item.ZapSenderProfile = ctx.Profiles[zapInfo.SenderPubkey]
		item.ZapRecipientProfile = ctx.Profiles[zapInfo.RecipientPubkey]
	}
}

// applyLiveEventData extracts live event information from tags and applies to item
func applyLiveEventData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	liveInfo := parseLiveEvent(tags)
	if liveInfo == nil {
		return
	}

	item.LiveTitle = liveInfo.Title
	item.LiveSummary = liveInfo.Summary
	item.LiveImage = liveInfo.Image
	item.LiveStatus = liveInfo.Status
	item.LiveStreamingURL = liveInfo.StreamingURL
	item.LiveRecordingURL = liveInfo.RecordingURL
	item.LiveStarts = liveInfo.Starts
	item.LiveEnds = liveInfo.Ends
	item.LiveCurrentCount = liveInfo.CurrentCount
	item.LiveTotalCount = liveInfo.TotalCount
	item.LiveHashtags = liveInfo.Hashtags
	item.LiveDTag = liveInfo.DTag

	// Generate zap.stream embed URL if the streaming URL is from zap.stream
	if strings.Contains(liveInfo.StreamingURL, "zap.stream") || strings.Contains(liveInfo.RecordingURL, "zap.stream") {
		naddr, err := nips.EncodeNAddr(30311, item.Pubkey, liveInfo.DTag)
		if err == nil {
			item.LiveEmbedURL = "https://zap.stream/" + naddr
		}
	}

	// Build participant list with profiles from context
	participants := make([]LiveParticipant, 0, len(liveInfo.ParticipantPubkeys))
	for _, pk := range liveInfo.ParticipantPubkeys {
		npub, _ := encodeBech32Pubkey(pk)
		participant := LiveParticipant{
			Pubkey:    pk,
			Npub:      npub,
			NpubShort: formatNpubShort(npub),
			Role:      liveInfo.ParticipantRoles[pk],
		}
		if ctx != nil && ctx.Profiles != nil {
			participant.Profile = ctx.Profiles[pk]
		}
		participants = append(participants, participant)
	}
	item.LiveParticipants = participants
}

// applyHighlightData extracts highlight information from tags and applies to item
func applyHighlightData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	highlightInfo := parseHighlight(tags)
	if highlightInfo == nil {
		return
	}

	item.HighlightContext = highlightInfo.Context
	item.HighlightComment = highlightInfo.Comment
	item.HighlightSourceURL = highlightInfo.SourceURL
	item.HighlightSourceRef = highlightInfo.SourceRef
}

// applyBookmarkData extracts bookmark information from tags and applies to item
func applyBookmarkData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	bookmarkInfo := parseBookmarks(tags)
	if bookmarkInfo == nil {
		return
	}

	item.BookmarkEventIDs = bookmarkInfo.EventIDs
	item.BookmarkArticleRefs = bookmarkInfo.ArticleRefs
	item.BookmarkHashtags = bookmarkInfo.Hashtags
	item.BookmarkURLs = bookmarkInfo.URLs
	item.BookmarkCount = len(bookmarkInfo.EventIDs) + len(bookmarkInfo.ArticleRefs) + len(bookmarkInfo.Hashtags) + len(bookmarkInfo.URLs)
}

// applyClassifiedData extracts classified listing information from tags and applies to item
func applyClassifiedData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	classifiedInfo := parseClassified(tags)
	if classifiedInfo == nil {
		return
	}

	item.Title = classifiedInfo.Title
	item.Summary = classifiedInfo.Summary
	item.ClassifiedLocation = classifiedInfo.Location
	item.ClassifiedGeohash = classifiedInfo.Geohash
	item.ClassifiedStatus = classifiedInfo.Status
	item.ClassifiedPublishedAt = classifiedInfo.PublishedAt
	item.ClassifiedPriceAmount = classifiedInfo.PriceAmount
	item.ClassifiedCurrency = classifiedInfo.Currency
	item.ClassifiedFrequency = classifiedInfo.Frequency
	item.ClassifiedPrice = formatClassifiedPrice(classifiedInfo.PriceAmount, classifiedInfo.Currency, classifiedInfo.Frequency)
	item.ClassifiedImages = classifiedInfo.Images
}

// applyVideoData extracts video information from tags and applies to item
func applyVideoData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	videoInfo := parseVideo(tags)
	if videoInfo == nil {
		return
	}

	item.VideoTitle = videoInfo.Title
	item.VideoURL = videoInfo.URL
	item.VideoThumbnail = videoInfo.Thumbnail
	item.VideoDuration = formatDuration(videoInfo.Duration)
	item.VideoDimension = videoInfo.Dimension
	item.VideoMimeType = videoInfo.MimeType
	item.VideoHashtags = videoInfo.Hashtags
	// Also set Title for consistency
	if videoInfo.Title != "" {
		item.Title = videoInfo.Title
	}
}

// applyCommentData extracts NIP-22 comment information from tags and applies to item
// NIP-22 uses uppercase tags (E, A, I, K, P) for root scope and lowercase (e, a, i, k, p) for parent
func applyCommentData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	var rootKind, parentKind int
	var rootID, parentID, rootURL string

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "K": // Root event kind (uppercase)
			if k, err := parseInt(tag[1]); err == nil {
				rootKind = k
			}
		case "k": // Parent event kind (lowercase)
			if k, err := parseInt(tag[1]); err == nil {
				parentKind = k
			}
		case "E": // Root event ID (uppercase)
			rootID = tag[1]
		case "e": // Parent event ID (lowercase)
			parentID = tag[1]
		case "A": // Root addressable event (uppercase) - format: kind:pubkey:d-tag
			rootID = tag[1]
		case "a": // Parent addressable event (lowercase)
			parentID = tag[1]
		case "I": // External identifier/URL (uppercase - root only)
			rootURL = tag[1]
		}
	}

	item.CommentRootKind = rootKind
	item.CommentRootID = rootID
	item.CommentRootURL = rootURL
	item.CommentParentKind = parentKind
	item.CommentParentID = parentID

	// Determine if this is a nested reply (root != parent)
	if parentID != "" && rootID != "" && parentID != rootID {
		item.CommentIsNested = true
	}

	// Generate human-readable label for the root kind
	item.CommentRootLabel = kindLabel(rootKind)
}

// kindLabel returns a human-readable label for common event kinds
func kindLabel(kind int) string {
	switch kind {
	case 1:
		return "note"
	case 20:
		return "photo"
	case 22:
		return "video"
	case 1063:
		return "file"
	case 1111:
		return "comment"
	case 30023:
		return "article"
	case 30311:
		return "live event"
	case 30402:
		return "listing"
	default:
		if kind >= 30000 && kind < 40000 {
			return "event"
		}
		return "event"
	}
}

// parseInt is a helper to parse string to int, returns error if invalid
func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

var errInvalidInt = &parseError{"invalid integer"}

type parseError struct {
	msg string
}

func (e *parseError) Error() string {
	return e.msg
}

// applyCalendarData extracts calendar event information from tags and applies to item (NIP-52)
func applyCalendarData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	var startTimestamp, endTimestamp int64
	var location, geohash, image string
	var hashtags []string
	var participantPubkeys []string
	participantRoles := make(map[string]string)

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "start":
			if ts, err := parseInt(tag[1]); err == nil {
				startTimestamp = int64(ts)
			}
		case "end":
			if ts, err := parseInt(tag[1]); err == nil {
				endTimestamp = int64(ts)
			}
		case "location":
			location = tag[1]
		case "g":
			geohash = tag[1]
		case "image":
			image = tag[1]
		case "t":
			hashtags = append(hashtags, tag[1])
		case "p":
			pubkey := tag[1]
			participantPubkeys = append(participantPubkeys, pubkey)
			// Role is in tag[3] if present (format: ["p", pubkey, relay, role])
			if len(tag) > 3 && tag[3] != "" {
				participantRoles[pubkey] = tag[3]
			}
		}
	}

	// Format dates and times
	if startTimestamp > 0 {
		item.CalendarStartDate, item.CalendarStartMonth, item.CalendarStartDay, item.CalendarStartTime = formatCalendarDateTime(startTimestamp, item.Kind)
	}
	if endTimestamp > 0 {
		item.CalendarEndDate, item.CalendarEndMonth, item.CalendarEndDay, item.CalendarEndTime = formatCalendarDateTime(endTimestamp, item.Kind)
	}

	// Date-based events (kind 31922) are all-day events
	item.CalendarIsAllDay = item.Kind == 31922

	item.CalendarLocation = location
	item.CalendarGeohash = geohash
	item.CalendarImage = image
	item.CalendarHashtags = hashtags

	// Build participant list with profiles from context
	participants := make([]CalendarParticipant, 0, len(participantPubkeys))
	for _, pk := range participantPubkeys {
		npub, _ := encodeBech32Pubkey(pk)
		participant := CalendarParticipant{
			Pubkey:    pk,
			Npub:      npub,
			NpubShort: formatNpubShort(npub),
			Role:      participantRoles[pk],
		}
		if ctx != nil && ctx.Profiles != nil {
			participant.Profile = ctx.Profiles[pk]
		}
		participants = append(participants, participant)
	}
	item.CalendarParticipants = participants
}

// unixToTime converts a Unix timestamp to a time.Time
func unixToTime(timestamp int64) time.Time {
	return time.Unix(timestamp, 0).UTC()
}

// formatCalendarDateTime formats a timestamp into date/time components for calendar display
func formatCalendarDateTime(timestamp int64, kind int) (date, month, day, timeStr string) {
	t := unixToTime(timestamp)

	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

	date = t.Format("2006-01-02")
	month = months[t.Month()-1]
	day = t.Format("2")

	// Only include time for time-based events (kind 31923)
	if kind == 31923 {
		timeStr = t.Format("15:04")
	}

	return
}

// formatDuration formats seconds into a human-readable duration string
func formatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return strings.Join([]string{
			itoa(hours),
			padZero(minutes),
			padZero(secs),
		}, ":")
	}
	return strings.Join([]string{
		itoa(minutes),
		padZero(secs),
	}, ":")
}

// itoa converts int to string without importing strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// padZero returns a zero-padded two-digit string
func padZero(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// applyFileData extracts file metadata from tags and applies to item (NIP-94)
func applyFileData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// Clear binary content - some events incorrectly put file data in content field
	if util.IsBinaryContent(item.Content) {
		item.Content = ""
		item.ContentHTML = ""
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "url":
			item.FileURL = tag[1]
		case "m":
			item.FileMimeType = tag[1]
			// Determine file type from MIME type
			if strings.HasPrefix(tag[1], "image/") {
				item.FileIsImage = true
			} else if strings.HasPrefix(tag[1], "video/") {
				item.FileIsVideo = true
			} else if strings.HasPrefix(tag[1], "audio/") {
				item.FileIsAudio = true
			}
		case "size":
			if size, err := parseInt(tag[1]); err == nil {
				item.FileSize = formatFileSize(int64(size))
			}
		case "dim":
			item.FileDimensions = tag[1]
		case "thumb":
			item.FileThumbnail = tag[1]
		case "alt":
			item.FileAlt = tag[1]
		case "summary":
			item.FileTitle = tag[1]
		}
	}
	// Use content as title if no summary tag
	if item.FileTitle == "" && item.Content != "" {
		// Use first line of content as title
		if idx := strings.Index(item.Content, "\n"); idx > 0 {
			item.FileTitle = item.Content[:idx]
		} else if len(item.Content) < 100 {
			item.FileTitle = item.Content
		}
	}
}

// formatFileSize formats bytes into human-readable size
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return itoa(int(bytes)) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	val := float64(bytes) / float64(div)
	// Format with one decimal place
	whole := int(val)
	frac := int((val - float64(whole)) * 10)
	if frac > 0 {
		return itoa(whole) + "." + itoa(frac) + " " + units[exp]
	}
	return itoa(whole) + " " + units[exp]
}

// applyStallData extracts marketplace stall data from tags and content (NIP-15)
func applyStallData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// NIP-15 stores stall data in content as JSON
	// For now, extract what we can from tags and basic content parsing
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.DTag = tag[1]
		case "name":
			item.StallName = tag[1]
		}
	}
	// Try to parse name from content if not in tags
	if item.StallName == "" && item.Content != "" {
		// Basic JSON extraction for name field
		if idx := strings.Index(item.Content, `"name"`); idx >= 0 {
			rest := item.Content[idx+6:]
			if start := strings.Index(rest, `"`); start >= 0 {
				rest = rest[start+1:]
				if end := strings.Index(rest, `"`); end >= 0 {
					item.StallName = rest[:end]
				}
			}
		}
	}
	if item.StallName == "" {
		item.StallName = item.DTag
	}
}

// applyProductData extracts marketplace product data from tags and content (NIP-15)
func applyProductData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// NIP-15 stores product data in content as JSON
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.DTag = tag[1]
		case "t":
			item.ProductCategories = append(item.ProductCategories, tag[1])
		}
	}
	// Try to parse product info from content JSON
	if item.Content != "" {
		item.ProductName = extractJSONString(item.Content, "name")
		item.ProductDescription = extractJSONString(item.Content, "description")
		item.ProductPrice = extractJSONString(item.Content, "price")
		item.ProductCurrency = extractJSONString(item.Content, "currency")
		item.ProductQuantity = extractJSONString(item.Content, "quantity")
		item.ProductStallID = extractJSONString(item.Content, "stall_id")
		item.ProductImages = extractJSONStringArray(item.Content, "images")
	}
	if item.ProductName == "" {
		item.ProductName = item.DTag
	}
}

// extractJSONString extracts a string value from JSON content (simple parser)
func extractJSONString(content, key string) string {
	search := `"` + key + `"`
	idx := strings.Index(content, search)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(search):]
	// Skip : and whitespace
	rest = strings.TrimLeft(rest, ": \t\n")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	if end := strings.Index(rest, `"`); end >= 0 {
		return rest[:end]
	}
	return ""
}

// extractJSONStringArray extracts a string array from JSON content (simple parser)
func extractJSONStringArray(content, key string) []string {
	search := `"` + key + `"`
	idx := strings.Index(content, search)
	if idx < 0 {
		return nil
	}
	rest := content[idx+len(search):]
	// Skip : and whitespace
	rest = strings.TrimLeft(rest, ": \t\n")
	if len(rest) == 0 || rest[0] != '[' {
		return nil
	}
	// Find closing bracket
	end := strings.Index(rest, "]")
	if end < 0 {
		return nil
	}
	arrayContent := rest[1:end]
	// Extract strings
	var result []string
	for {
		start := strings.Index(arrayContent, `"`)
		if start < 0 {
			break
		}
		arrayContent = arrayContent[start+1:]
		end := strings.Index(arrayContent, `"`)
		if end < 0 {
			break
		}
		result = append(result, arrayContent[:end])
		arrayContent = arrayContent[end+1:]
	}
	return result
}

// applyStatusData extracts user status data from tags (NIP-38)
func applyStatusData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.StatusType = tag[1] // "general", "music", etc.
		case "r":
			item.StatusLink = tag[1]
		}
	}
	if item.StatusType == "" {
		item.StatusType = "general"
	}
}

// applyCommunityData extracts community definition data from tags (NIP-72)
func applyCommunityData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.DTag = tag[1]
			if item.CommunityName == "" {
				item.CommunityName = tag[1]
			}
		case "name":
			item.CommunityName = tag[1]
		case "description":
			item.CommunityDescription = tag[1]
		case "image":
			item.CommunityImage = tag[1]
		case "p":
			// Check if this is a moderator (role in tag[3])
			if len(tag) > 3 && tag[3] == "moderator" {
				item.CommunityModerators = append(item.CommunityModerators, tag[1])
			}
		}
	}
}

// applyBadgeDefinitionData extracts badge definition data from tags (NIP-58)
func applyBadgeDefinitionData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.DTag = tag[1]
			if item.BadgeName == "" {
				item.BadgeName = tag[1]
			}
		case "name":
			item.BadgeName = tag[1]
		case "description":
			item.BadgeDescription = tag[1]
		case "image":
			item.BadgeImage = tag[1]
		case "thumb":
			item.BadgeThumbnail = tag[1]
		}
	}
}

// applyBadgeAwardData extracts badge award data from tags (NIP-58)
func applyBadgeAwardData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "p":
			item.BadgeAwardees = append(item.BadgeAwardees, tag[1])
		}
	}
}

// applyReportData extracts report data from tags (NIP-56)
func applyReportData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 3 {
			continue
		}
		// Report type is in tag[2] for p and e tags
		switch tag[0] {
		case "p", "e":
			if item.ReportType == "" {
				item.ReportType = tag[2]
			}
		}
	}
	if item.ReportType == "" {
		item.ReportType = "other"
	}
}

// applyLiveChatData extracts live chat message data from tags (NIP-53)
func applyLiveChatData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "a":
			// Reference to live event: kind:pubkey:d-tag
			item.LiveEventRef = tag[1]
		case "e":
			// Reply to another chat message
			item.ReplyToID = tag[1]
		}
	}
}

// applyRSVPData extracts calendar RSVP data from tags (NIP-52)
func applyRSVPData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "a":
			// Reference to calendar event
			item.CalendarEventRef = tag[1]
		case "d":
			item.DTag = tag[1]
		case "status":
			item.RSVPStatus = tag[1] // accepted, declined, tentative
		case "fb":
			item.RSVPFreebusy = tag[1] // free or busy
		}
	}
	// Default to tentative if no status
	if item.RSVPStatus == "" {
		item.RSVPStatus = "tentative"
	}
}

// applyLabelData extracts label data from tags (NIP-32)
func applyLabelData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	var namespaces []string
	var labels []LabelInfo
	var targets []LabelTarget

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "L":
			// Label namespace
			namespaces = append(namespaces, tag[1])
		case "l":
			// Label value (namespace is in tag[2] if present)
			label := LabelInfo{Value: tag[1]}
			if len(tag) > 2 {
				label.Namespace = tag[2]
			}
			labels = append(labels, label)
		case "e":
			// Event target - encode to note1 format
			eventNote, _ := nips.EncodeEventID(tag[1])
			if eventNote == "" {
				eventNote = tag[1]
			}
			targets = append(targets, LabelTarget{
				Type: "event",
				URL:  "/thread/" + eventNote,
			})
		case "p":
			// Profile target
			if npub, err := encodeBech32Pubkey(tag[1]); err == nil {
				targets = append(targets, LabelTarget{
					Type: "profile",
					URL:  "/profile/" + npub,
				})
			}
		case "r":
			// Relay or URL target
			targets = append(targets, LabelTarget{
				Type: "relay",
				URL:  tag[1],
			})
		case "t":
			// Topic/hashtag target
			targets = append(targets, LabelTarget{
				Type: "topic",
				URL:  util.BuildURL("/search", map[string]string{"q": "#" + tag[1]}),
			})
		}
	}

	// If labels don't have namespaces, try to match with L tags
	if len(namespaces) > 0 {
		for i := range labels {
			if labels[i].Namespace == "" && i < len(namespaces) {
				labels[i].Namespace = namespaces[i]
			}
		}
	}

	item.Labels = labels
	item.LabelTargets = targets
}

// applyRepositoryData extracts repository announcement data from tags (NIP-34)
func applyRepositoryData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	var maintainerPubkeys []string
	var hashtags []string

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			item.RepoID = tag[1]
			item.DTag = tag[1]
		case "name":
			item.RepoName = tag[1]
		case "description":
			item.RepoDescription = tag[1]
		case "web":
			item.RepoWebURLs = append(item.RepoWebURLs, tag[1])
		case "clone":
			item.RepoCloneURLs = append(item.RepoCloneURLs, tag[1])
		case "maintainers":
			maintainerPubkeys = append(maintainerPubkeys, tag[1])
		case "t":
			hashtags = append(hashtags, tag[1])
		}
	}

	// Store hashtags for template access
	item.RepoHashtags = hashtags

	// Build maintainer profiles from context
	if len(maintainerPubkeys) > 0 {
		for _, pk := range maintainerPubkeys {
			npub, _ := encodeBech32Pubkey(pk)
			maintainer := RepoMaintainer{
				Pubkey:    pk,
				Npub:      npub,
				NpubShort: formatNpubShort(npub),
			}
			if ctx != nil && ctx.Profiles != nil {
				if p := ctx.Profiles[pk]; p != nil {
					maintainer.DisplayName = p.DisplayName
					maintainer.Picture = p.Picture
				}
			}
			if maintainer.DisplayName == "" {
				maintainer.DisplayName = maintainer.NpubShort
			}
			item.RepoMaintainers = append(item.RepoMaintainers, maintainer)
		}
	}
}

// applyHandlerData extracts NIP-89 application handler info from tags and JSON content
func applyHandlerData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// Parse JSON content for handler metadata
	if item.Content != "" {
		var handlerInfo struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			About       string `json:"about"`
			Picture     string `json:"picture"`
			Website     string `json:"website"`
		}
		if json.Unmarshal([]byte(item.Content), &handlerInfo) == nil {
			item.HandlerName = handlerInfo.Name
			if handlerInfo.DisplayName != "" {
				item.HandlerName = handlerInfo.DisplayName
			}
			item.HandlerAbout = handlerInfo.About
			item.HandlerPicture = handlerInfo.Picture
			item.HandlerWebsite = handlerInfo.Website
		}
	}

	// Extract supported kinds from k tags
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "k" {
			if kind, err := strconv.Atoi(tag[1]); err == nil {
				item.HandlerKinds = append(item.HandlerKinds, kind)
			}
		}
	}
}

// applyRecommendationData extracts NIP-89 handler recommendation info from tags
func applyRecommendationData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// Extract the kind this recommendation is for from d tag
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "d" {
			if kind, err := strconv.Atoi(tag[1]); err == nil {
				item.RecommendedForKind = kind
			}
			break
		}
	}

	// Extract handler reference from a tag
	// Format: ["a", "31990:<pubkey>:<d-tag>", "<relay-hint>"]
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "a" {
			parts := strings.Split(tag[1], ":")
			if len(parts) >= 3 && parts[0] == "31990" {
				handlerPubkey := parts[1]
				handlerDTag := parts[2]

				// Try to resolve handler info
				handler := resolveHandlerInfo(ctx, handlerPubkey, handlerDTag, tag)
				if handler != nil {
					handler.DTag = handlerDTag
					item.RecommendedHandler = handler
				} else {
					// Fallback: create minimal handler with pubkey and d-tag
					item.RecommendedHandler = &RecommendedHandler{
						Pubkey: handlerPubkey,
						DTag:   handlerDTag,
					}
				}
			}
			break
		}
	}
}

// resolveHandlerInfo attempts to resolve handler name/picture from cache or fetch
// Falls back to author's profile if 31990 event has no metadata
func resolveHandlerInfo(ctx *KindProcessingContext, pubkey, dTag string, aTag []string) *RecommendedHandler {
	// Build relay list: relay hint first, then context relays, then handler relays as fallback
	var relays []string
	if len(aTag) >= 3 && aTag[2] != "" {
		relays = append(relays, aTag[2])
	}
	if ctx != nil && ctx.Relays != nil {
		relays = append(relays, ctx.Relays...)
	}
	// Always include handler relays as fallback for better discovery
	relays = append(relays, config.GetHandlerRelays()...)

	handler := &RecommendedHandler{
		Pubkey: pubkey,
	}

	// Try to fetch the 31990 handler event
	if len(relays) > 0 {
		filter := Filter{
			Kinds:   []int{31990},
			Authors: []string{pubkey},
			DTags:   []string{dTag},
			Limit:   1,
		}

		events, _ := fetchEventsFromRelaysCached(relays, filter)
		if len(events) > 0 {
			// Parse JSON content from 31990 event
			var handlerInfo struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Picture     string `json:"picture"`
			}
			if json.Unmarshal([]byte(events[0].Content), &handlerInfo) == nil {
				handler.Name = handlerInfo.Name
				if handlerInfo.DisplayName != "" {
					handler.Name = handlerInfo.DisplayName
				}
				handler.Picture = handlerInfo.Picture
			}
		}
	}

	// If still no name/picture, fall back to author's profile
	if handler.Name == "" || handler.Picture == "" {
		// Try cached profile first (fast path)
		if profile := getCachedProfile(pubkey); profile != nil {
			if handler.Name == "" {
				handler.Name = profile.DisplayName
				if handler.Name == "" {
					handler.Name = profile.Name
				}
			}
			if handler.Picture == "" {
				handler.Picture = profile.Picture
			}
		} else if len(relays) > 0 {
			// Fetch profile from relays
			profileRelays := append(relays, config.GetProfileRelays()...)
			profiles := fetchProfiles(profileRelays, []string{pubkey})
			if profile, ok := profiles[pubkey]; ok && profile != nil {
				if handler.Name == "" {
					handler.Name = profile.DisplayName
					if handler.Name == "" {
						handler.Name = profile.Name
					}
				}
				if handler.Picture == "" {
					handler.Picture = profile.Picture
				}
			}
		}
	}

	return handler
}

// applyAudioData extracts audio track data from NOM content (kind 32123)
// NOM (Nostr Open Media) stores track metadata as JSON in the event content field
func applyAudioData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) {
	// Parse NOM content from event content (JSON)
	if item.Content == "" {
		return
	}

	var nom NOMContent
	if err := json.Unmarshal([]byte(item.Content), &nom); err != nil {
		return
	}

	item.AudioTitle = nom.Title
	item.AudioCreator = nom.Creator
	item.AudioURL = nom.Enclosure
	item.AudioPageURL = nom.Link
	item.AudioMimeType = nom.Type

	if nom.Duration > 0 {
		item.AudioDuration = FormatDuration(nom.Duration)
	}

	// Set Title for consistency with other kinds
	if nom.Title != "" {
		item.Title = nom.Title
	}

	// Extract d-tag (track UUID) for addressable events
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "d" {
			item.DTag = tag[1]
			break
		}
	}
}
