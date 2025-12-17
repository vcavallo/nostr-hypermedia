package main

import "strings"

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
		naddr, err := EncodeNAddr(30311, item.Pubkey, liveInfo.DTag)
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
	item.VideoDuration = videoInfo.Duration
	item.VideoDimension = videoInfo.Dimension
	item.VideoMimeType = videoInfo.MimeType
	// Also set Title for consistency
	if videoInfo.Title != "" {
		item.Title = videoInfo.Title
	}
}
