package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"

	"nostr-hypermedia/templates"
)

// markdownSanitizer is a bluemonday policy for sanitizing markdown-rendered HTML.
// UGCPolicy allows common formatting (links, images, bold, italic, lists, tables, code)
// while blocking dangerous elements (scripts, event handlers, javascript: URLs).
var markdownSanitizer = bluemonday.UGCPolicy()

// Template name constants - use these instead of string literals to catch typos at compile time
const (
	tmplBase                 = "base"
	tmplFragment             = "fragment"
	tmplEventDispatcher      = "event-dispatcher"
	tmplAppendFragment       = "append-fragment"
	tmplFooterFragment       = "footer-fragment"
	tmplFollowButton         = "follow-button"
	tmplPostResponse         = "post-response"
	tmplReplyResponse        = "reply-response"
	tmplProfileAppend        = "profile-append"
	tmplNotificationsAppend  = "notifications-append"
	tmplSearchAppend         = "search-append"
	tmplSearchResults        = "search-results"
	tmplGifPanel             = "gif-panel"
	tmplGifResults           = "gif-results"
	tmplGifAttachment        = "gif-attachment"
)

// Cached compiled templates - initialized at startup via init()
var (
	cachedHTMLTemplate          *template.Template
	cachedThreadTemplate        *template.Template
	cachedProfileTemplate       *template.Template
	cachedNotificationsTemplate *template.Template
	cachedMutesTemplate         *template.Template
	cachedSearchTemplate        *template.Template
	cachedQuoteTemplate         *template.Template
	cachedWalletTemplate        *template.Template
	// Fragment templates for HelmJS partial updates
	cachedTimelineFragment      *template.Template
	cachedThreadFragment        *template.Template
	cachedProfileFragment       *template.Template
	cachedNotificationsFragment *template.Template
	cachedMutesFragment         *template.Template
	cachedSearchFragment        *template.Template
	cachedWalletFragment        *template.Template
	cachedWalletInfoFragment       *template.Template
	cachedNewNotesIndicator        *template.Template
	cachedLinkPreview              *template.Template
	cachedOOBFlash                 *template.Template
	// Action fragment templates for HelmJS inline updates
	cachedFooterFragment            *template.Template
	cachedFollowButtonFragment      *template.Template
	cachedAppendFragment            *template.Template
	cachedNotificationsAppend       *template.Template
	cachedSearchAppend              *template.Template
	cachedProfileAppend             *template.Template
	cachedPostResponse              *template.Template
	cachedReplyResponse             *template.Template
	// GIF picker templates
	cachedGifsTemplate              *template.Template
	cachedGifPanel                  *template.Template
	cachedGifResults                *template.Template
	cachedGifAttachment             *template.Template
	cachedComposeTemplate           *template.Template
	templateFuncMap                 template.FuncMap
)

// isHelmRequest checks if the request was made by HelmJS for partial update
func isHelmRequest(r *http.Request) bool {
	return r.Header.Get("H-Request") == "true"
}

// renderFooterFragment renders just the note footer for HelmJS partial updates after actions
// userReaction is the reaction the user just made (e.g., "❤️") - empty string if not a react action
// relays is used to fetch existing reactions and reply count for the event
func renderFooterFragment(eventID string, eventPubkey string, kind int, loggedIn bool, csrfToken, returnURL string, isBookmarked bool, isReacted bool, isReposted bool, isZapped bool, hasWallet bool, userReaction string, relays []string) (string, error) {
	// Fetch existing reactions and reply count for this event FIRST
	// so we can include reply count in the action context
	var reactions *ReactionsSummary
	var replyCount int
	var reactionCount int
	if len(relays) > 0 {
		reactionsMap := fetchReactions(relays, []string{eventID})
		if r, ok := reactionsMap[eventID]; ok {
			reactions = r
			reactionCount = r.Total
		}
		// Fetch reply count
		replyCountMap := fetchReplyCounts(relays, []string{eventID})
		if count, ok := replyCountMap[eventID]; ok {
			replyCount = count
		}
	}

	// If user just reacted, ensure their reaction is counted
	// (in case it hasn't propagated to relays yet)
	if userReaction != "" {
		if reactions == nil {
			reactions = &ReactionsSummary{
				Total:  1,
				ByType: map[string]int{userReaction: 1},
			}
			reactionCount = 1
		} else {
			// Check if this reaction type already exists
			if _, exists := reactions.ByType[userReaction]; !exists {
				// Add the new reaction type
				reactions.ByType[userReaction] = 1
				reactions.Total++
				reactionCount = reactions.Total
			}
			// Note: We don't increment existing count since it might already include user's reaction
		}
	}

	// Build action context with counts
	ctx := ActionContext{
		EventID:       eventID,
		EventPubkey:   eventPubkey,
		Kind:          kind,
		IsBookmarked:  isBookmarked,
		IsReacted:     isReacted,
		IsReposted:    isReposted,
		IsZapped:      isZapped,
		HasWallet:     hasWallet,
		ReplyCount:    replyCount,
		ReactionCount: reactionCount,
		LoggedIn:      loggedIn,
		CSRFToken:     csrfToken,
		ReturnURL:     returnURL,
	}

	// Get actions for this event (no tags available in footer fragment context)
	entity := BuildHypermediaEntity(ctx, nil, nil)
	actionGroups := GroupActionsForKind(entity.Actions, kind)

	// Build minimal data for footer template
	// Note: Uses ID (not EventID) to match HTMLEventItem struct
	data := struct {
		ID           string
		ActionGroups []HTMLActionGroup
		Reactions    *ReactionsSummary
		ReplyCount   int
		LoggedIn     bool
	}{
		ID:           eventID,
		ActionGroups: actionGroups,
		Reactions:    reactions,
		ReplyCount:   replyCount,
		LoggedIn:     loggedIn,
	}

	var buf strings.Builder
	if err := cachedFooterFragment.ExecuteTemplate(&buf, tmplFooterFragment, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderFollowButtonFragment renders just the follow button for HelmJS partial updates
func renderFollowButtonFragment(pubkey, csrfToken, returnURL string, isFollowing bool) (string, error) {
	data := struct {
		Pubkey      string
		CSRFToken   string
		ReturnURL   string
		IsFollowing bool
	}{
		Pubkey:      pubkey,
		CSRFToken:   csrfToken,
		ReturnURL:   returnURL,
		IsFollowing: isFollowing,
	}

	var buf strings.Builder
	if err := cachedFollowButtonFragment.ExecuteTemplate(&buf, tmplFollowButton, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderPostResponse renders the cleared post form plus the new note as OOB prepend
func renderPostResponse(csrfToken string, newNote *HTMLEventItem) (string, error) {
	data := struct {
		CSRFToken     string
		NewNote       *HTMLEventItem
		ShowGifButton bool
	}{
		CSRFToken:     csrfToken,
		NewNote:       newNote,
		ShowGifButton: GiphyEnabled(),
	}

	var buf strings.Builder
	if err := cachedPostResponse.ExecuteTemplate(&buf, tmplPostResponse, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderReplyResponse renders the cleared reply form plus the new reply as OOB prepend
func renderReplyResponse(csrfToken, replyTo, replyToPubkey, userDisplayName, userAvatarURL, userNpub string, newReply *HTMLEventItem, replyCount int) (string, error) {
	data := struct {
		CSRFToken       string
		ReplyTo         string
		ReplyToPubkey   string
		UserDisplayName string
		UserAvatarURL   string
		UserNpub        string
		NewReply        *HTMLEventItem
		ReplyCount      int
		ShowGifButton   bool
	}{
		CSRFToken:       csrfToken,
		ReplyTo:         replyTo,
		ReplyToPubkey:   replyToPubkey,
		UserDisplayName: userDisplayName,
		UserAvatarURL:   userAvatarURL,
		UserNpub:        userNpub,
		NewReply:        newReply,
		ReplyCount:      replyCount,
		ShowGifButton:   GiphyEnabled(),
	}

	var buf strings.Builder
	if err := cachedReplyResponse.ExecuteTemplate(&buf, tmplReplyResponse, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderGifPanel renders the inline GIF picker panel for HelmJS
func renderGifPanel(targetID string) (string, error) {
	data := struct {
		TargetID string
	}{
		TargetID: targetID,
	}

	var buf strings.Builder
	if err := cachedGifPanel.ExecuteTemplate(&buf, tmplGifPanel, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderGifResults renders the GIF search results grid
func renderGifResults(results []GifResult, query, targetID string) (string, error) {
	data := struct {
		Results  []GifResult
		Query    string
		TargetID string
	}{
		Results:  results,
		Query:    query,
		TargetID: targetID,
	}

	var buf strings.Builder
	if err := cachedGifResults.ExecuteTemplate(&buf, tmplGifResults, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderGifAttachment renders the selected GIF preview with hidden input
func renderGifAttachment(url, thumbURL, targetID string) (string, error) {
	data := struct {
		URL      string
		ThumbURL string
		TargetID string
	}{
		URL:      url,
		ThumbURL: thumbURL,
		TargetID: targetID,
	}

	var buf strings.Builder
	if err := cachedGifAttachment.ExecuteTemplate(&buf, tmplGifAttachment, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// formatRelativeTime returns a human-readable relative time string
func formatRelativeTime(ts int64) string {
	t := time.Unix(ts, 0)
	now := time.Now()
	diff := now.Sub(t)

	// Handle future timestamps (shouldn't happen but just in case)
	if diff < 0 {
		return "just now"
	}

	seconds := int(diff.Seconds())
	minutes := int(diff.Minutes())
	hours := int(diff.Hours())
	days := hours / 24
	weeks := days / 7
	months := days / 30
	years := days / 365

	switch {
	case seconds < 60:
		return "just now"
	case minutes == 1:
		return "1 min ago"
	case minutes < 60:
		return fmt.Sprintf("%d mins ago", minutes)
	case hours == 1:
		return "1 hour ago"
	case hours < 24:
		return fmt.Sprintf("%d hours ago", hours)
	case days == 1:
		return "yesterday"
	case days < 7:
		return fmt.Sprintf("%d days ago", days)
	case weeks == 1:
		return "1 week ago"
	case weeks < 4:
		return fmt.Sprintf("%d weeks ago", weeks)
	case months == 1:
		return "1 month ago"
	case months < 12:
		return fmt.Sprintf("%d months ago", months)
	case years == 1:
		return "1 year ago"
	default:
		return fmt.Sprintf("%d years ago", years)
	}
}

// extractDomain extracts the domain from a URL (e.g., "wss://nwc.primal.net/path" -> "nwc.primal.net")
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // Return as-is if parsing fails
	}
	return parsed.Host
}

// initTemplates compiles all templates once at startup for performance
func initTemplates() {
	templateFuncMap = template.FuncMap{
		"formatTime": func(ts int64) string {
			return formatRelativeTime(ts)
		},
		"displayName": func(profile *ProfileInfo, fallback string) string {
			if profile == nil {
				return fallback
			}
			if profile.DisplayName != "" {
				return profile.DisplayName
			}
			if profile.Name != "" {
				return profile.Name
			}
			return fallback
		},
		"slice": func(s string, start, end int) string {
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		},
		"join": func(arr []string, sep string) string {
			return strings.Join(arr, sep)
		},
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},
		"linkName": func(s string) string {
			if strings.Contains(s, "/profiles/") {
				return "profile"
			}
			if strings.Contains(s, "/threads/") {
				return "thread"
			}
			return "link"
		},
		"title": func(s string) string {
			return strings.Title(strings.ReplaceAll(s, "_", " "))
		},
		"gt": func(a, b int) bool {
			return a > b
		},
		"timeAgo":   formatTimeAgo,
		"avatarURL": GetValidatedAvatarURL,
		"isoTime": func(ts int64) string {
			return time.Unix(ts, 0).UTC().Format(time.RFC3339)
		},
		"i18n": I18n,
		"truncateURL": func(url string, maxLen int) string {
			if len(url) <= maxLen {
				return url
			}
			// Remove protocol prefix for display
			display := url
			if strings.HasPrefix(display, "https://") {
				display = display[8:]
			} else if strings.HasPrefix(display, "http://") {
				display = display[7:]
			}
			if len(display) <= maxLen {
				return display
			}
			return display[:maxLen-3] + "..."
		},
		"urlquery": func(s string) string {
			return template.URLQueryEscaper(s)
		},
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires even number of arguments")
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"siteConfig": func() *SiteConfig {
			return GetSiteConfig()
		},
	}

	var err error

	// Get shared templates from templates package
	baseTemplates := templates.GetBaseTemplates()
	kindTemplates := templates.GetKindTemplates()
	fragmentTemplate := templates.GetFragmentTemplate()

	// Compile timeline template: base + timeline content + kind sub-templates
	cachedHTMLTemplate, err = template.New("timeline").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetTimelineTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile timeline template", "error", err); os.Exit(1)
	}

	// Compile thread template: base + thread content + kind sub-templates
	cachedThreadTemplate, err = template.New("thread").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetThreadTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile thread template", "error", err); os.Exit(1)
	}

	// Compile profile template: base + profile content + kind sub-templates
	cachedProfileTemplate, err = template.New("profile").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetProfileTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile profile template", "error", err); os.Exit(1)
	}

	// Compile notifications template: base + notifications content + kind sub-templates (for pagination, quoted-note)
	cachedNotificationsTemplate, err = template.New("notifications").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetNotificationsTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile notifications template", "error", err); os.Exit(1)
	}

	// Compile mutes template: base + mutes content
	cachedMutesTemplate, err = template.New("mutes").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetMutesTemplate())
	if err != nil {
		slog.Error("failed to compile mutes template", "error", err); os.Exit(1)
	}

	// Compile search template: base + search content + kind sub-templates (for author-header)
	cachedSearchTemplate, err = template.New("search").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetSearchTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile search template", "error", err); os.Exit(1)
	}

	// Compile quote template: base + quote content + kind templates (for flash-messages)
	cachedQuoteTemplate, err = template.New("quote").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetQuoteTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile quote template", "error", err); os.Exit(1)
	}

	// Compile wallet template: base + wallet content + kind templates (for flash-messages)
	cachedWalletTemplate, err = template.New("wallet").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetWalletTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile wallet template", "error", err); os.Exit(1)
	}

	// Compile fragment templates for HelmJS partial updates
	// These render just the content block without the base wrapper
	cachedTimelineFragment, err = template.New("timeline-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetTimelineTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile timeline fragment template", "error", err); os.Exit(1)
	}

	cachedThreadFragment, err = template.New("thread-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetThreadTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile thread fragment template", "error", err); os.Exit(1)
	}

	cachedProfileFragment, err = template.New("profile-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetProfileTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile profile fragment template", "error", err); os.Exit(1)
	}

	cachedNotificationsFragment, err = template.New("notifications-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetNotificationsTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile notifications fragment template", "error", err); os.Exit(1)
	}

	cachedMutesFragment, err = template.New("mutes-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetMutesTemplate())
	if err != nil {
		slog.Error("failed to compile mutes fragment template", "error", err); os.Exit(1)
	}

	cachedSearchFragment, err = template.New("search-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetSearchTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile search fragment template", "error", err); os.Exit(1)
	}

	cachedWalletFragment, err = template.New("wallet-fragment").Funcs(templateFuncMap).Parse(
		fragmentTemplate + templates.GetWalletTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile wallet fragment template", "error", err); os.Exit(1)
	}

	// Compile wallet info fragment template for lazy-loaded balance/transactions
	cachedWalletInfoFragment, err = template.New("wallet-info").Funcs(templateFuncMap).Parse(
		templates.GetWalletInfoTemplate())
	if err != nil {
		slog.Error("failed to compile wallet info fragment template", "error", err); os.Exit(1)
	}

	// Compile new notes indicator template for timeline polling
	cachedNewNotesIndicator, err = template.New("new-notes-indicator").Funcs(templateFuncMap).Parse(
		templates.GetNewNotesIndicatorTemplate())
	if err != nil {
		slog.Error("failed to compile new notes indicator template", "error", err); os.Exit(1)
	}

	// Compile link preview template for Open Graph cards
	cachedLinkPreview, err = template.New("link-preview").Funcs(templateFuncMap).Parse(
		templates.GetLinkPreviewTemplate())
	if err != nil {
		slog.Error("failed to compile link preview template", "error", err); os.Exit(1)
	}

	// Compile OOB flash message template for HelmJS error responses
	cachedOOBFlash, err = template.New("oob-flash").Funcs(templateFuncMap).Parse(
		templates.GetOOBFlashTemplate())
	if err != nil {
		slog.Error("failed to compile OOB flash template", "error", err); os.Exit(1)
	}

	// Compile footer fragment template for HelmJS action responses (react, bookmark)
	cachedFooterFragment, err = template.New("footer-fragment").Funcs(templateFuncMap).Parse(
		templates.GetFooterFragmentTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile footer fragment template", "error", err); os.Exit(1)
	}

	// Compile follow button fragment template for HelmJS action responses
	cachedFollowButtonFragment, err = template.New("follow-button").Funcs(templateFuncMap).Parse(
		templates.GetFollowButtonTemplate())
	if err != nil {
		slog.Error("failed to compile follow button fragment template", "error", err); os.Exit(1)
	}

	// Compile append fragment template for HelmJS "Load More" responses
	cachedAppendFragment, err = template.New("append-fragment").Funcs(templateFuncMap).Parse(
		templates.GetAppendFragmentTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile append fragment template", "error", err); os.Exit(1)
	}

	// Compile notifications append fragment template
	cachedNotificationsAppend, err = template.New("notifications-append").Funcs(templateFuncMap).Parse(
		templates.GetNotificationsAppendTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile notifications append fragment template", "error", err); os.Exit(1)
	}

	// Compile search append fragment template
	cachedSearchAppend, err = template.New("search-append").Funcs(templateFuncMap).Parse(
		templates.GetSearchAppendTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile search append fragment template", "error", err); os.Exit(1)
	}

	// Compile profile append fragment template
	cachedProfileAppend, err = template.New("profile-append").Funcs(templateFuncMap).Parse(
		templates.GetProfileAppendTemplate())
	if err != nil {
		slog.Error("failed to compile profile append fragment template", "error", err); os.Exit(1)
	}

	// Compile post response template for HelmJS OOB updates
	cachedPostResponse, err = template.New("post-response").Funcs(templateFuncMap).Parse(
		templates.GetPostResponseTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile post response template", "error", err); os.Exit(1)
	}

	// Compile reply response template for HelmJS OOB updates
	cachedReplyResponse, err = template.New("reply-response").Funcs(templateFuncMap).Parse(
		templates.GetReplyResponseTemplate() + kindTemplates)
	if err != nil {
		slog.Error("failed to compile reply response template", "error", err); os.Exit(1)
	}

	// Compile GIF picker templates
	cachedGifsTemplate, err = template.New("gifs").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetGifsPageTemplate())
	if err != nil {
		slog.Error("failed to compile gifs page template", "error", err); os.Exit(1)
	}

	cachedGifPanel, err = template.New("gif-panel").Funcs(templateFuncMap).Parse(
		templates.GetGifPanelTemplate())
	if err != nil {
		slog.Error("failed to compile gif panel template", "error", err); os.Exit(1)
	}

	cachedGifResults, err = template.New("gif-results").Funcs(templateFuncMap).Parse(
		templates.GetGifResultsTemplate())
	if err != nil {
		slog.Error("failed to compile gif results template", "error", err); os.Exit(1)
	}

	cachedGifAttachment, err = template.New("gif-attachment").Funcs(templateFuncMap).Parse(
		templates.GetGifAttachmentTemplate())
	if err != nil {
		slog.Error("failed to compile gif attachment template", "error", err); os.Exit(1)
	}

	cachedComposeTemplate, err = template.New("compose").Funcs(templateFuncMap).Parse(
		baseTemplates + templates.GetComposeTemplate())
	if err != nil {
		slog.Error("failed to compile compose template", "error", err); os.Exit(1)
	}

	slog.Info("all HTML templates compiled successfully")

	// Validate that all template name constants reference templates that exist
	validateTemplateReferences()
}

// validateTemplateReferences checks that all template name constants actually exist
// in the compiled templates. This catches typos like "kind-dispatcher" vs "event-dispatcher"
// at startup rather than at runtime.
func validateTemplateReferences() {
	// Map of template name constant -> template that should contain it
	references := []struct {
		name     string
		template *template.Template
		desc     string
	}{
		{tmplBase, cachedHTMLTemplate, "timeline"},
		{tmplFragment, cachedTimelineFragment, "timeline fragment"},
		{tmplEventDispatcher, cachedAppendFragment, "append fragment"},
		{tmplAppendFragment, cachedAppendFragment, "append fragment"},
		{tmplFooterFragment, cachedFooterFragment, "footer fragment"},
		{tmplFollowButton, cachedFollowButtonFragment, "follow button fragment"},
		{tmplPostResponse, cachedPostResponse, "post response"},
		{tmplReplyResponse, cachedReplyResponse, "reply response"},
		{tmplProfileAppend, cachedProfileAppend, "profile append"},
		{tmplNotificationsAppend, cachedNotificationsAppend, "notifications append"},
		{tmplSearchAppend, cachedSearchAppend, "search append"},
		{tmplSearchResults, cachedSearchTemplate, "search"},
	}

	for _, ref := range references {
		if ref.template.Lookup(ref.name) == nil {
			slog.Error("template validation failed",
				"template", ref.name,
				"parent", ref.desc)
			os.Exit(1)
		}
	}

	slog.Info("template references validated", "count", len(references))
}

// getThemeFromRequest reads the theme cookie and returns (themeClass, themeLabel)
// themeClass is used on <html> element, themeLabel shows the current theme (i18n)
func getThemeFromRequest(r *http.Request) (string, string) {
	theme := ""
	if cookie, err := r.Cookie("theme"); err == nil {
		theme = cookie.Value
	}

	switch theme {
	case "dark":
		return "dark", I18n("theme.dark")
	case "light":
		return "light", I18n("theme.light")
	default:
		return "", I18n("theme.auto")
	}
}


type HTMLPageData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	Meta                   *MetaInfo
	Items                  []HTMLEventItem
	Pagination             *HTMLPagination
	Actions                []HTMLAction
	Links                  []string
	LoggedIn               bool
	UserPubKey             string
	UserDisplayName        string   // Display name from profile (falls back to @npubShort)
	Error                  string
	Success                string
	FeedMode               string   // "follows" or "global" (legacy, use FeedModes instead)
	KindFilter             string   // Current kind filter (legacy, use KindFilters instead)
	ActiveRelays           []string // Relays being used for this request
	CurrentURL             string   // Current page URL for reaction redirects
	ThemeClass             string   // "dark", "light", or "" for system default
	ThemeLabel             string   // Label for theme toggle button
	CSRFToken              string   // CSRF token for form submission
	HasUnreadNotifications bool // Whether there are notifications newer than last seen
	ShowPostForm           bool // Show the post form in header (timeline only)
	ShowGifButton          bool // Show GIF button in post form (depends on GIPHY_API_KEY)
	NewestTimestamp        int64    // Timestamp of newest item (for polling new notes)
	KindsParam             string   // Current kinds as URL param (e.g., "1,6") for new notes polling
	// Navigation (NATEOAS)
	FeedModes     []FeedMode     // Available feed modes
	KindFilters   []KindFilter   // Available kind filters
	NavItems       []NavItem       // Navigation items (search, notifications)
	SettingsItems  []SettingsItem  // Settings dropdown items
	SettingsToggle SettingsToggle  // Settings toggle button config
}

type HTMLEventItem struct {
	ID             string
	Kind           int
	Tags           [][]string // Raw event tags for action discovery
	TemplateName   string // Template to use for rendering (from KindRegistry)
	RenderTemplate string // Full template name for dispatch (e.g., "render-note")
	Pubkey        string
	Npub          string // Bech32-encoded npub format
	NpubShort     string // Short display format (npub1abc...xyz)
	CreatedAt     int64
	Content       string
	ContentHTML   template.HTML
	ImagesHTML    template.HTML // Pre-rendered images from imeta tags (kind 20)
	Title         string        // Title from title tag (kind 20, 30023)
	Summary       string        // Summary from summary tag (kind 30023)
	HeaderImage   string        // Header image URL from image tag (kind 30023)
	PublishedAt   int64         // Published timestamp from published_at tag (kind 30023)
	RelaysSeen    []string
	Links         []string
	AuthorProfile *ProfileInfo
	Reactions     *ReactionsSummary
	ReplyCount    int
	ParentID      string         // ID of parent event if this is a reply
	ReplyToName   string         // Display name of parent author (for "replying to" context)
	ReplyToNpub   string         // Npub of parent author (for link)
	RepostedEvent  *HTMLEventItem // For kind 6 reposts: the embedded original event
	QuotedEvent    *HTMLEventItem // For quote posts: the quoted note (from q tag)
	QuotedEventID  string         // Event ID from q tag (used to fetch quoted event)
	// Kind 9735 zap receipt fields
	ZapSenderPubkey    string       // Pubkey of who sent the zap
	ZapSenderNpub      string       // Npub of sender
	ZapSenderNpubShort string       // Short npub of sender
	ZapSenderProfile   *ProfileInfo // Profile of sender
	ZapRecipientPubkey string       // Pubkey of who received the zap
	ZapRecipientNpub   string       // Npub of recipient
	ZapRecipientNpubShort string    // Short npub of recipient
	ZapRecipientProfile *ProfileInfo // Profile of recipient
	ZapAmountSats      int64        // Amount in sats
	ZapComment         string       // Optional zap comment
	ZappedEventID      string       // Event ID that was zapped (if any)
	// Kind 30311 live event fields
	LiveTitle         string              // Event title
	LiveSummary       string              // Event summary/description
	LiveImage         string              // Preview image URL
	LiveStatus        string              // "planned", "live", or "ended"
	LiveStreamingURL  string              // Streaming URL
	LiveRecordingURL  string              // Recording URL (after event ends)
	LiveStarts        int64               // Start timestamp
	LiveEnds          int64               // End timestamp
	LiveParticipants  []LiveParticipant   // List of participants with roles
	LiveCurrentCount  int                 // Current participant count
	LiveTotalCount    int                 // Total participant count
	LiveHashtags      []string            // Hashtags for the event
	LiveDTag          string              // d-tag identifier for addressable events
	LiveEmbedURL      string              // Embed URL for iframe (e.g., zap.stream)
	// Kind 9802 highlight fields
	HighlightContext    string        // Surrounding context text
	HighlightComment    string        // User's comment on the highlight
	HighlightSourceURL  string        // Source URL (from r tag)
	HighlightSourceRef  string        // Nostr reference (from a tag) - naddr or nevent
	// Kind 10003 bookmark list fields
	BookmarkEventIDs    []string      // Bookmarked event IDs (from e tags)
	BookmarkArticleRefs []string      // Bookmarked article references (from a tags)
	BookmarkHashtags    []string      // Bookmarked hashtags (from t tags)
	BookmarkURLs        []string      // Bookmarked URLs (from r tags)
	BookmarkCount       int           // Total bookmark count
	// User state for current user
	IsBookmarked        bool          // Whether logged-in user has bookmarked this item
	IsReacted           bool          // Whether logged-in user has reacted to this item
	IsReposted          bool          // Whether logged-in user has reposted this item
	IsZapped            bool          // Whether logged-in user has zapped this item
	IsMuted             bool          // Whether the event's author is in user's mute list
	// Kind 30402 classified listing fields (NIP-99)
	ClassifiedPrice       string   // Formatted price display (e.g., "€15/month")
	ClassifiedPriceAmount string   // Numeric price amount
	ClassifiedCurrency    string   // Currency code (EUR, USD, btc, etc.)
	ClassifiedFrequency   string   // Price frequency (hour, day, week, month, year, etc.)
	ClassifiedLocation    string   // Location from location tag
	ClassifiedGeohash     string   // Geohash from g tag
	ClassifiedStatus      string   // "active" or "sold"
	ClassifiedPublishedAt int64    // Published timestamp
	ClassifiedImages      []string // Image URLs from image tags
	// Kind 22 short-form video fields (NIP-71)
	VideoURL       string // Video URL from imeta tag
	VideoThumbnail string // Thumbnail image URL from imeta tag (image field)
	VideoDuration  int    // Duration in seconds from imeta tag
	VideoDimension string // Dimensions (e.g., "1080x1920") from imeta tag
	VideoMimeType  string // MIME type from imeta tag
	VideoTitle     string // Title from title tag
	// Actions available for this event (populated by BuildHypermediaEntity)
	ActionGroups []HTMLActionGroup // Grouped actions for pill layout
	// Login state for rendering in sub-templates
	LoggedIn            bool          // Whether user is logged in (needed for sub-templates)
	// Used for new notes feature - marks the oldest new note for scrolling
	IsScrollTarget      bool          // Whether to add scroll target ID
}

// LiveParticipant represents a participant in a live event
type LiveParticipant struct {
	Pubkey    string
	Npub      string
	NpubShort string
	Role      string       // Host, Speaker, Participant, etc.
	Profile   *ProfileInfo
}

// computeRenderTemplate returns the template name for rendering an event.
// Priority: render-hint tag > KindRegistry > default
func computeRenderTemplate(templateName string, tags [][]string) string {
	// Check for render-hint tag on the event
	if hint := ParseRenderHintFromTags(tags); hint != "" {
		return "render-" + hint
	}
	// Use KindRegistry template name
	if templateName != "" {
		return "render-" + templateName
	}
	// Fallback
	return "render-default"
}

type HTMLPagination struct {
	Prev string
	Next string
}

type HTMLAction struct {
	Name      string      // Action identifier (reply, react, etc.)
	Title     string      // Display text
	Href      string      // Target URL
	Method    string      // GET or POST
	Class     string      // CSS class(es) for styling
	Rel       string      // Link relation (e.g., "reply", "bookmark", "author")
	Icon      string      // Optional icon
	IconOnly  string      // "always", "mobile", "desktop", or "" (never) - controls icon-only display
	CSRFToken string      // CSRF token (extracted from Fields for explicit rendering)
	Fields    []HTMLField // Form fields for POST actions (excludes csrf_token)
	Disabled  bool        // If true, render as non-interactive text (deprecated, use Completed)
	Completed bool        // If true, action already performed (filled pill style)
	Count     int         // Count to display (if HasCount is true)
	HasCount  bool        // Whether to show count
	GroupWith string      // If set, this action appears in another action's dropdown
}

type HTMLField struct {
	Name  string
	Value string
}

// Image extension regex
var imageExtRegex = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|webp)(\?.*)?$`)
// Video extension regex
var videoExtRegex = regexp.MustCompile(`(?i)\.(mp4|webm|mov|m4v)(\?.*)?$`)
// Audio extension regex
var audioExtRegex = regexp.MustCompile(`(?i)\.(mp3|wav|ogg|flac|m4a|aac)(\?.*)?$`)
// YouTube URL regex - matches youtube.com/watch?v=ID, youtu.be/ID, youtube.com/shorts/ID
var youtubeRegex = regexp.MustCompile(`(?i)(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)

// YouTube playlist URL regex - matches youtube.com/playlist?list=PLAYLIST_ID
var youtubePlaylistRegex = regexp.MustCompile(`(?i)youtube\.com/playlist\?list=([a-zA-Z0-9_-]+)`)
var urlRegex = regexp.MustCompile(`https?://[^\s<>"]+`)

// cleanURLTrailing removes trailing punctuation that's likely not part of the URL
// Handles common cases like URLs in parentheses "(https://example.com)" or markdown links
func cleanURLTrailing(url string) string {
	// Remove trailing punctuation that's unlikely to be part of a URL
	// Be careful: ) can be part of URLs (e.g., Wikipedia), so only strip if unbalanced
	for len(url) > 0 {
		last := url[len(url)-1]
		switch last {
		case ',', ';', ':', '!', '?':
			// These are almost never valid at the end of a URL
			url = url[:len(url)-1]
		case '.':
			// Period at end is usually sentence punctuation, not part of URL
			// But be careful: .com. vs .com/path.
			// Only strip if there's no path component after the last /
			if !strings.HasSuffix(url, "/") {
				// Check if it looks like "domain.tld." with no path
				lastSlash := strings.LastIndex(url, "/")
				afterSlash := url[lastSlash+1:]
				if !strings.Contains(afterSlash, ".") || strings.HasSuffix(afterSlash, ".") {
					url = url[:len(url)-1]
				} else {
					return url
				}
			} else {
				return url
			}
		case ')':
			// Only strip ) if parentheses are unbalanced
			opens := strings.Count(url, "(")
			closes := strings.Count(url, ")")
			if closes > opens {
				url = url[:len(url)-1]
			} else {
				return url
			}
		default:
			return url
		}
	}
	return url
}

// Regex to collapse multiple newlines before media URLs (images, videos, audio, youtube)
var mediaURLRegex = regexp.MustCompile(`(?i)(\n\s*)+\n(https?://[^\s<>"]+\.(jpg|jpeg|png|gif|webp|mp4|webm|mov|m4v|mp3|wav|ogg|flac|m4a|aac)(\?[^\s<>"]*)?|https?://(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)[a-zA-Z0-9_-]{11})`)

// consecutiveImgRegex matches 2+ consecutive <img> tags (with optional whitespace between)
var consecutiveImgRegex = regexp.MustCompile(`(<img [^>]+>)(\s*<img [^>]+>)+`)

// imgTagRegex matches individual <img> tags for counting
var imgTagRegex = regexp.MustCompile(`<img [^>]+>`)

// wrapConsecutiveImages wraps groups of 2+ consecutive images in a gallery div
// Uses "image-gallery-odd" class when there's an odd number of images (first image full width)
func wrapConsecutiveImages(html string) string {
	return consecutiveImgRegex.ReplaceAllStringFunc(html, func(match string) string {
		imgCount := len(imgTagRegex.FindAllString(match, -1))
		if imgCount%2 == 1 {
			return `<div class="image-gallery image-gallery-odd">` + match + `</div>`
		}
		return `<div class="image-gallery">` + match + `</div>`
	})
}

// Nostr reference regex - matches nostr:nevent1..., nostr:note1..., nostr:nprofile1..., nostr:naddr1..., nostr:npub1...
var nostrRefRegex = regexp.MustCompile(`nostr:(nevent1[a-z0-9]+|note1[a-z0-9]+|nprofile1[a-z0-9]+|naddr1[a-z0-9]+|npub1[a-z0-9]+)`)

// ResolvedRef holds a pre-resolved nostr reference
type ResolvedRef struct {
	HTML string
}

// extractNostrRefs extracts all nostr: identifiers from content strings
func extractNostrRefs(contents []string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, content := range contents {
		matches := nostrRefRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				identifier := match[1]
				if !seen[identifier] {
					seen[identifier] = true
					refs = append(refs, identifier)
				}
			}
		}
	}
	return refs
}

// batchResolveNostrRefs pre-fetches all nostr references in parallel
// Returns a map of identifier -> rendered HTML
func batchResolveNostrRefs(identifiers []string, relays []string) map[string]string {
	if len(identifiers) == 0 || len(relays) == 0 {
		return nil
	}

	resolved := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, id := range identifiers {
		wg.Add(1)
		go func(identifier string) {
			defer wg.Done()
			html := resolveNostrReference(identifier, relays)
			mu.Lock()
			resolved[identifier] = html
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	return resolved
}

// formatNpubShort creates a shortened npub display like "npub1abc...xyz"
func formatNpubShort(npub string) string {
	if len(npub) <= 16 {
		return npub
	}
	return npub[:9] + "..." + npub[len(npub)-4:]
}

// ImetaImage represents a parsed image from an imeta tag (NIP-68)
type ImetaImage struct {
	URL      string
	MimeType string
	Alt      string
	Dim      string // e.g., "1920x1080"
	Blurhash string
}

// parseImetaTag parses an imeta tag into an ImetaImage struct
// imeta format: ["imeta", "url https://...", "m image/jpeg", "dim 1920x1080", "alt description", "blurhash LEHV6n..."]
func parseImetaTag(tag []string) *ImetaImage {
	if len(tag) < 2 || tag[0] != "imeta" {
		return nil
	}

	img := &ImetaImage{}
	for _, field := range tag[1:] {
		// Each field is "key value" format
		parts := strings.SplitN(field, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		switch key {
		case "url":
			img.URL = value
		case "m":
			img.MimeType = value
		case "alt":
			img.Alt = value
		case "dim":
			img.Dim = value
		case "blurhash":
			img.Blurhash = value
		}
	}

	// URL is required
	if img.URL == "" {
		return nil
	}
	return img
}

// extractImetaImages extracts all imeta tags from event tags and renders them as HTML
func extractImetaImages(tags [][]string) template.HTML {
	var images []*ImetaImage
	for _, tag := range tags {
		if img := parseImetaTag(tag); img != nil {
			images = append(images, img)
		}
	}

	if len(images) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, img := range images {
		alt := img.Alt
		if alt == "" {
			alt = "image"
		}
		sb.WriteString(`<img src="`)
		sb.WriteString(html.EscapeString(img.URL))
		sb.WriteString(`" alt="`)
		sb.WriteString(html.EscapeString(alt))
		sb.WriteString(`" loading="lazy" class="picture-image">`)
	}

	return template.HTML(sb.String())
}

// extractTitle extracts the title tag value from event tags
func extractTitle(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "title" {
			return tag[1]
		}
	}
	return ""
}

// extractSummary extracts the summary tag value from event tags (kind 30023)
func extractSummary(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "summary" {
			return tag[1]
		}
	}
	return ""
}

// extractHeaderImage extracts the image tag value from event tags (kind 30023)
func extractHeaderImage(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "image" {
			return tag[1]
		}
	}
	return ""
}

// extractPublishedAt extracts the published_at tag value from event tags (kind 30023)
func extractPublishedAt(tags [][]string) int64 {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "published_at" {
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				return ts
			}
		}
	}
	return 0
}

// renderMarkdown converts markdown content to HTML using goldmark
// The output is sanitized with bluemonday to prevent XSS attacks
func renderMarkdown(content string) template.HTML {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(content), &buf); err != nil {
		// Fallback to escaped plain text if markdown parsing fails
		return template.HTML(html.EscapeString(content))
	}
	// Sanitize HTML to prevent XSS from malicious markdown/HTML in articles
	sanitized := markdownSanitizer.Sanitize(buf.String())
	return template.HTML(sanitized)
}

// extractRepostEventIDs extracts referenced event IDs from kind 6 reposts that have empty content.
// These are "reference-only" reposts per NIP-18 that need the referenced event fetched from relays.
// Returns a map of repost event ID -> referenced event ID for reposts needing fetch.
func extractRepostEventIDs(events []Event) map[string]string {
	result := make(map[string]string)
	for _, evt := range events {
		if evt.Kind == 6 && strings.TrimSpace(evt.Content) == "" {
			// Find the e tag (referenced event ID)
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					result[evt.ID] = tag[1]
					break
				}
			}
		}
	}
	return result
}

// extractRepostEventIDsFromItems extracts referenced event IDs from EventItem slice
func extractRepostEventIDsFromItems(items []EventItem) map[string]string {
	result := make(map[string]string)
	for _, item := range items {
		if item.Kind == 6 && strings.TrimSpace(item.Content) == "" {
			// Find the e tag (referenced event ID)
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					result[item.ID] = tag[1]
					break
				}
			}
		}
	}
	return result
}

// parseRepostedEvent parses the embedded event JSON from a kind 6 repost's content field.
// If content is empty (reference-only repost), it looks up the event from repostEvents map.
func parseRepostedEvent(content string, tags [][]string, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview, profiles map[string]*ProfileInfo, repostEvents map[string]*Event) *HTMLEventItem {
	// Try to parse embedded JSON from content first
	if content != "" {
		var embeddedEvent struct {
			ID        string     `json:"id"`
			PubKey    string     `json:"pubkey"`
			CreatedAt int64      `json:"created_at"`
			Kind      int        `json:"kind"`
			Tags      [][]string `json:"tags"`
			Content   string     `json:"content"`
			Sig       string     `json:"sig"`
		}

		if err := json.Unmarshal([]byte(content), &embeddedEvent); err == nil {
			return buildRepostedEventItem(embeddedEvent.ID, embeddedEvent.PubKey, embeddedEvent.CreatedAt,
				embeddedEvent.Kind, embeddedEvent.Tags, embeddedEvent.Content,
				relays, resolvedRefs, linkPreviews, profiles)
		}
	}

	// Content is empty or invalid JSON - this is a reference-only repost
	// Look up the referenced event from pre-fetched events
	if repostEvents != nil {
		for _, tag := range tags {
			if len(tag) >= 2 && tag[0] == "e" {
				if evt, ok := repostEvents[tag[1]]; ok {
					return buildRepostedEventItem(evt.ID, evt.PubKey, evt.CreatedAt,
						evt.Kind, evt.Tags, evt.Content,
						relays, resolvedRefs, linkPreviews, profiles)
				}
				break
			}
		}
	}

	return nil
}

// buildRepostedEventItem creates an HTMLEventItem from event data
func buildRepostedEventItem(id, pubkey string, createdAt int64, kind int, tags [][]string, content string, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview, profiles map[string]*ProfileInfo) *HTMLEventItem {
	npub, _ := encodeBech32Pubkey(pubkey)
	kindDef := GetKindDefinition(kind)

	reposted := &HTMLEventItem{
		ID:             id,
		Kind:           kind,
		Tags:           tags,
		TemplateName:   kindDef.TemplateName,
		RenderTemplate: computeRenderTemplate(kindDef.TemplateName, tags),
		Pubkey:         pubkey,
		Npub:           npub,
		NpubShort:      formatNpubShort(npub),
		CreatedAt:      createdAt,
		Content:        content,
		ContentHTML:    processContentToHTMLFull(content, relays, resolvedRefs, linkPreviews),
		AuthorProfile:  profiles[pubkey],
	}

	// Extract kind-specific metadata
	if kindDef.ExtractImages {
		reposted.ImagesHTML = extractImetaImages(tags)
	}
	if kindDef.ExtractTitle {
		reposted.Title = extractTitle(tags)
	}

	return reposted
}

// ZapInfo holds parsed information from a kind 9735 zap receipt
type ZapInfo struct {
	SenderPubkey    string
	RecipientPubkey string
	AmountMsats     int64
	Comment         string
	ZappedEventID   string
}

// parseZapReceipt extracts zap information from a kind 9735 event's tags
func parseZapReceipt(tags [][]string) *ZapInfo {
	info := &ZapInfo{}

	var descriptionJSON string

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "p":
			info.RecipientPubkey = tag[1]
		case "P":
			info.SenderPubkey = tag[1]
		case "e":
			info.ZappedEventID = tag[1]
		case "description":
			descriptionJSON = tag[1]
		}
	}

	// Parse the description (zap request) to get sender and amount
	if descriptionJSON != "" {
		var zapRequest struct {
			PubKey  string     `json:"pubkey"`
			Content string     `json:"content"`
			Tags    [][]string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(descriptionJSON), &zapRequest); err == nil {
			// Sender is the author of the zap request
			if info.SenderPubkey == "" {
				info.SenderPubkey = zapRequest.PubKey
			}
			// Comment is the content of the zap request
			info.Comment = zapRequest.Content
			// Look for amount tag in zap request
			for _, tag := range zapRequest.Tags {
				if len(tag) >= 2 && tag[0] == "amount" {
					if msats, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
						info.AmountMsats = msats
					}
				}
			}
		}
	}

	return info
}

// LiveEventInfo holds parsed information from a kind 30311 live event
type LiveEventInfo struct {
	DTag             string // d-tag identifier for addressable events
	Title            string
	Summary          string
	Image            string
	Status           string // "planned", "live", "ended"
	StreamingURL     string
	RecordingURL     string
	Starts           int64
	Ends             int64
	CurrentCount     int
	TotalCount       int
	Hashtags         []string
	ParticipantPubkeys []string // Pubkeys of participants
	ParticipantRoles   map[string]string // Pubkey -> Role mapping
}

// parseLiveEvent extracts live event information from a kind 30311 event's tags
func parseLiveEvent(tags [][]string) *LiveEventInfo {
	info := &LiveEventInfo{
		ParticipantRoles: make(map[string]string),
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "d":
			info.DTag = tag[1]
		case "title":
			info.Title = tag[1]
		case "summary":
			info.Summary = tag[1]
		case "image":
			info.Image = tag[1]
		case "status":
			info.Status = tag[1]
		case "streaming":
			info.StreamingURL = tag[1]
		case "recording":
			info.RecordingURL = tag[1]
		case "starts":
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				info.Starts = ts
			}
		case "ends":
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				info.Ends = ts
			}
		case "current_participants":
			if count, err := strconv.Atoi(tag[1]); err == nil {
				info.CurrentCount = count
			}
		case "total_participants":
			if count, err := strconv.Atoi(tag[1]); err == nil {
				info.TotalCount = count
			}
		case "t":
			info.Hashtags = append(info.Hashtags, tag[1])
		case "p":
			// p tag format: ["p", pubkey, relay, role, proof]
			pubkey := tag[1]
			info.ParticipantPubkeys = append(info.ParticipantPubkeys, pubkey)
			if len(tag) >= 4 && tag[3] != "" {
				info.ParticipantRoles[pubkey] = tag[3]
			}
		}
	}

	return info
}

// HighlightInfo holds parsed data from a kind 9802 highlight event
type HighlightInfo struct {
	Context    string // Surrounding text context
	Comment    string // User's commentary on the highlight
	SourceURL  string // Source URL (from r tag)
	SourceRef  string // Nostr reference (from a tag) - naddr or nevent
}

// parseHighlight extracts highlight information from a kind 9802 event's tags
func parseHighlight(tags [][]string) *HighlightInfo {
	info := &HighlightInfo{}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "context":
			info.Context = tag[1]
		case "comment":
			info.Comment = tag[1]
		case "r":
			// Source URL - only take the first one if multiple
			if info.SourceURL == "" {
				info.SourceURL = tag[1]
			}
		case "a":
			// Nostr article/event reference (naddr format)
			if info.SourceRef == "" {
				info.SourceRef = tag[1]
			}
		}
	}

	return info
}

// BookmarkInfo holds parsed data from a kind 10003 bookmark list event
type BookmarkInfo struct {
	EventIDs    []string // Bookmarked event IDs (from e tags)
	ArticleRefs []string // Bookmarked article references (from a tags)
	Hashtags    []string // Bookmarked hashtags (from t tags)
	URLs        []string // Bookmarked URLs (from r tags)
}

// parseBookmarks extracts bookmark information from a kind 10003 event's tags
func parseBookmarks(tags [][]string) *BookmarkInfo {
	info := &BookmarkInfo{
		EventIDs:    []string{},
		ArticleRefs: []string{},
		Hashtags:    []string{},
		URLs:        []string{},
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "e":
			info.EventIDs = append(info.EventIDs, tag[1])
		case "a":
			info.ArticleRefs = append(info.ArticleRefs, tag[1])
		case "t":
			info.Hashtags = append(info.Hashtags, tag[1])
		case "r":
			info.URLs = append(info.URLs, tag[1])
		}
	}

	return info
}

// ClassifiedInfo holds parsed data from a kind 30402 classified listing event (NIP-99)
type ClassifiedInfo struct {
	Title       string   // From title tag
	Summary     string   // From summary tag
	Location    string   // From location tag
	Geohash     string   // From g tag
	Status      string   // From status tag ("active" or "sold")
	PublishedAt int64    // From published_at tag
	PriceAmount string   // Price amount from price tag
	Currency    string   // Currency from price tag
	Frequency   string   // Frequency from price tag (optional)
	Images      []string // From image tags
}

// parseClassified extracts classified listing information from a kind 30402 event's tags
func parseClassified(tags [][]string) *ClassifiedInfo {
	info := &ClassifiedInfo{
		Images: []string{},
		Status: "active", // Default status
	}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "title":
			info.Title = tag[1]
		case "summary":
			info.Summary = tag[1]
		case "location":
			info.Location = tag[1]
		case "g":
			info.Geohash = tag[1]
		case "status":
			info.Status = tag[1]
		case "published_at":
			if ts, err := strconv.ParseInt(tag[1], 10, 64); err == nil {
				info.PublishedAt = ts
			}
		case "price":
			// Format: ["price", "amount", "currency", "frequency"]
			info.PriceAmount = tag[1]
			if len(tag) >= 3 {
				info.Currency = tag[2]
			}
			if len(tag) >= 4 {
				info.Frequency = tag[3]
			}
		case "image":
			info.Images = append(info.Images, tag[1])
		}
	}

	return info
}

// formatClassifiedPrice formats a price for display (e.g., "100 EUR" or "50 USD/month")
func formatClassifiedPrice(amount, currency, frequency string) string {
	if amount == "" {
		return ""
	}
	price := amount
	if currency != "" {
		price += " " + currency
	}
	if frequency != "" {
		price += "/" + frequency
	}
	return price
}

// VideoInfo holds parsed data from a kind 22 short-form video event (NIP-71)
type VideoInfo struct {
	Title     string // From title tag
	URL       string // Video URL from imeta tag
	Thumbnail string // Thumbnail image from imeta tag (image field)
	Duration  int    // Duration in seconds from imeta tag
	Dimension string // Dimensions (e.g., "1080x1920") from imeta tag
	MimeType  string // MIME type from imeta tag
}

// parseVideo extracts video information from a kind 22 event's tags (NIP-71)
// imeta format: ["imeta", "url <url>", "image <thumbnail>", "dim <WxH>", "duration <secs>", "m <mime>"]
func parseVideo(tags [][]string) *VideoInfo {
	info := &VideoInfo{}

	for _, tag := range tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "title":
			info.Title = tag[1]
		case "imeta":
			// Parse imeta tag fields (each field is "key value" format)
			for _, field := range tag[1:] {
				parts := strings.SplitN(field, " ", 2)
				if len(parts) < 2 {
					continue
				}
				key, value := parts[0], parts[1]
				switch key {
				case "url":
					info.URL = value
				case "image":
					info.Thumbnail = value
				case "dim":
					info.Dimension = value
				case "duration":
					if dur, err := strconv.Atoi(value); err == nil {
						info.Duration = dur
					}
				case "m":
					info.MimeType = value
				}
			}
		}
	}

	return info
}

// processContentToHTML converts plain text content to HTML with images and links
// This version does not resolve nostr: references (for backward compatibility)
func processContentToHTML(content string) template.HTML {
	return processContentToHTMLFull(content, nil, nil, nil)
}

// processContentToHTMLFull converts plain text content to HTML with images, links,
// pre-resolved nostr: references, and link previews.
func processContentToHTMLFull(content string, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview) template.HTML {
	// Trim leading/trailing whitespace
	content = strings.TrimSpace(content)

	// Collapse multiple newlines before media URLs to just a single newline
	content = mediaURLRegex.ReplaceAllString(content, "\n$2")

	// Use placeholders for nostr: references to avoid URL regex matching their HTML
	type placeholder struct {
		key   string
		value string
	}
	var placeholders []placeholder
	placeholderIndex := 0

	// First, extract nostr: references and replace with placeholders (before escaping)
	processedContent := nostrRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		identifier := strings.TrimPrefix(match, "nostr:")

		var resolved string
		if resolvedRefs != nil {
			// Use pre-resolved HTML if available
			if html, ok := resolvedRefs[identifier]; ok {
				resolved = html
			} else {
				// Fallback to simple link if not pre-resolved
				resolved = nostrRefToLink(identifier)
			}
		} else if relays != nil && len(relays) > 0 {
			// Fetch synchronously (slow path - avoid in loops)
			resolved = resolveNostrReference(identifier, relays)
		} else {
			// No relays, just render as link
			resolved = nostrRefToLink(identifier)
		}

		key := fmt.Sprintf("\x00NOSTR_%d\x00", placeholderIndex)
		placeholderIndex++
		placeholders = append(placeholders, placeholder{key: key, value: resolved})
		return key
	})

	// Now escape the content (placeholders will be escaped but that's fine - they're unique)
	escaped := html.EscapeString(processedContent)

	// Find all URLs and replace them
	result := urlRegex.ReplaceAllStringFunc(escaped, func(rawURL string) string {
		// Unescape the URL (it was escaped above)
		rawURL = html.UnescapeString(rawURL)

		// Clean trailing punctuation (e.g., from markdown links or prose)
		url := cleanURLTrailing(rawURL)
		trailing := rawURL[len(url):] // Preserve removed chars to append after

		if imageExtRegex.MatchString(url) {
			return fmt.Sprintf(`<img src="%s" alt="image" loading="lazy">`, html.EscapeString(url)) + html.EscapeString(trailing)
		}
		if videoExtRegex.MatchString(url) {
			return fmt.Sprintf(`<video src="%s" controls preload="metadata" class="note-video"></video>`, html.EscapeString(url)) + html.EscapeString(trailing)
		}
		if audioExtRegex.MatchString(url) {
			return fmt.Sprintf(`<audio src="%s" controls preload="metadata" class="note-audio"></audio>`, html.EscapeString(url)) + html.EscapeString(trailing)
		}
		if match := youtubeRegex.FindStringSubmatch(url); len(match) > 1 {
			videoID := match[1]
			return fmt.Sprintf(`<iframe class="youtube-embed" src="https://www.youtube-nocookie.com/embed/%s" allow="accelerometer; clipboard-write; encrypted-media; gyroscope; picture-in-picture" allowfullscreen></iframe>`, html.EscapeString(videoID)) + html.EscapeString(trailing)
		}
		if match := youtubePlaylistRegex.FindStringSubmatch(url); len(match) > 1 {
			playlistID := match[1]
			return fmt.Sprintf(`<iframe class="youtube-embed" src="https://www.youtube-nocookie.com/embed/videoseries?list=%s" allow="accelerometer; clipboard-write; encrypted-media; gyroscope; picture-in-picture" allowfullscreen></iframe>`, html.EscapeString(playlistID)) + html.EscapeString(trailing)
		}
		// Check for link preview (try both cleaned and raw URL for cache lookup)
		if linkPreviews != nil {
			if preview, ok := linkPreviews[url]; ok && !preview.Failed && preview.Title != "" {
				return renderLinkPreview(url, preview) + html.EscapeString(trailing)
			}
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener">%s</a>`, html.EscapeString(url), html.EscapeString(url)) + html.EscapeString(trailing)
	})

	// Now replace placeholders with actual HTML (placeholders got escaped, so unescape them first)
	for _, p := range placeholders {
		escapedKey := html.EscapeString(p.key)
		result = strings.Replace(result, escapedKey, p.value, 1)
	}

	// Wrap consecutive images in a gallery div for better layout
	result = wrapConsecutiveImages(result)

	return template.HTML(result)
}

// LinkPreviewData holds data for the link preview template.
type LinkPreviewData struct {
	URL         string
	Image       string
	SiteName    string
	Title       string
	Description string
}

// renderLinkPreview creates an HTML preview card for a URL using compiled template
func renderLinkPreview(url string, preview *LinkPreview) string {
	// Truncate description if too long
	desc := preview.Description
	if len(desc) > 150 {
		desc = desc[:147] + "..."
	}

	data := LinkPreviewData{
		URL:         url,
		Image:       preview.Image,
		SiteName:    preview.SiteName,
		Title:       preview.Title,
		Description: desc,
	}

	var buf strings.Builder
	if err := cachedLinkPreview.ExecuteTemplate(&buf, "link-preview", data); err != nil {
		slog.Error("failed to render link preview", "error", err, "url", url)
		return ""
	}
	return buf.String()
}

// ExtractMentionedPubkeys extracts all pubkeys from npub/nprofile references in content
func ExtractMentionedPubkeys(contents []string) []string {
	seen := make(map[string]bool)
	var pubkeys []string

	for _, content := range contents {
		matches := nostrRefRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			identifier := match[1]

			var pubkey string
			if strings.HasPrefix(identifier, "npub1") {
				pk, err := decodeBech32Pubkey(identifier)
				if err == nil {
					pubkey = pk
				}
			} else if strings.HasPrefix(identifier, "nprofile1") {
				np, err := DecodeNProfile(identifier)
				if err == nil {
					pubkey = np.Pubkey
				}
			}

			if pubkey != "" && !seen[pubkey] {
				seen[pubkey] = true
				pubkeys = append(pubkeys, pubkey)
			}
		}
	}
	return pubkeys
}

// getCachedUsername returns @username if profile is cached, otherwise @npubShort
func getCachedUsername(pubkey string) string {
	// Check profile cache first (no network fetch)
	if profile, _, inCache := profileCache.Get(pubkey); inCache && profile != nil {
		// Prefer display_name, then name
		if profile.DisplayName != "" {
			return "@" + profile.DisplayName
		}
		if profile.Name != "" {
			return "@" + profile.Name
		}
	}
	// Fall back to short npub
	if npub, err := encodeBech32Pubkey(pubkey); err == nil {
		return "@" + formatNpubShort(npub)
	}
	// Last fallback: truncate pubkey (with bounds check)
	if len(pubkey) >= 12 {
		return "@" + pubkey[:12] + "..."
	}
	return "@" + pubkey
}

// QuotedRef represents a parsed quoted event reference from a q tag
type QuotedRef struct {
	Original string // Original q tag value
	EventID  string // Hex event ID (for regular events or resolved from nevent/note)
	ATag     string // For addressable events: "kind:pubkey:d-tag"
	IsNAddr  bool   // True if this is an naddr reference
}

// parseQuotedRef parses a q tag value and returns a QuotedRef
// Handles: hex event IDs, note1..., nevent1..., naddr1..., and raw a-tag format (kind:pubkey:d-tag)
func parseQuotedRef(qTagValue string) QuotedRef {
	ref := QuotedRef{Original: qTagValue}

	switch {
	case strings.HasPrefix(qTagValue, "naddr1"):
		if na, err := DecodeNAddr(qTagValue); err == nil {
			ref.IsNAddr = true
			ref.ATag = fmt.Sprintf("%d:%s:%s", na.Kind, na.Author, na.DTag)
		}
	case strings.HasPrefix(qTagValue, "nevent1"):
		if ne, err := DecodeNEvent(qTagValue); err == nil {
			ref.EventID = ne.EventID
		}
	case strings.HasPrefix(qTagValue, "note1"):
		if id, err := DecodeNote(qTagValue); err == nil {
			ref.EventID = id
		}
	default:
		// Check if it's a raw a-tag format: "kind:pubkey:d-tag"
		// e.g., "30023:b7ed68b062de6b4a12e51fd5285c1e1e0ed0e5128cda93ab11b4150b55ed32fc:0cdda9a44c6e161f"
		parts := strings.SplitN(qTagValue, ":", 3)
		if len(parts) == 3 && len(parts[1]) == 64 {
			// Validate kind is a number
			if _, err := fmt.Sscanf(parts[0], "%d", new(int)); err == nil {
				ref.IsNAddr = true
				ref.ATag = qTagValue
			}
		} else if len(qTagValue) == 64 {
			// Assume it's a hex event ID
			ref.EventID = qTagValue
		}
	}

	return ref
}

// fetchQuotedEvents fetches quoted events from q tags, handling both regular event IDs and naddr references
// Returns maps keyed by the original q tag value for easy lookup
// Note: Uses defaultRelays (aggregators) since quoted events can reference events from anywhere on Nostr
func fetchQuotedEvents(qTagValues []string) (map[string]*Event, map[string]*ProfileInfo) {
	quotedEvents := make(map[string]*Event)
	quotedEventProfiles := make(map[string]*ProfileInfo)

	if len(qTagValues) == 0 {
		return quotedEvents, quotedEventProfiles
	}

	// Use defaultRelays (aggregators) for quoted events since they can reference any event
	relays := ConfigGetDefaultRelays()

	// Parse all q tag values and categorize them
	var eventIDs []string
	var aTags []string
	refsByEventID := make(map[string][]string)  // eventID -> original q tag values
	refsByATag := make(map[string][]string)     // aTag -> original q tag values

	for _, qVal := range qTagValues {
		ref := parseQuotedRef(qVal)
		if ref.IsNAddr && ref.ATag != "" {
			aTags = append(aTags, ref.ATag)
			refsByATag[ref.ATag] = append(refsByATag[ref.ATag], qVal)
		} else if ref.EventID != "" {
			eventIDs = append(eventIDs, ref.EventID)
			refsByEventID[ref.EventID] = append(refsByEventID[ref.EventID], qVal)
		}
	}

	pubkeys := make(map[string]bool)

	// Fetch regular events by ID
	if len(eventIDs) > 0 {
		filter := Filter{IDs: eventIDs, Limit: len(eventIDs)}
		fetchedEvents, _ := fetchEventsFromRelays(relays, filter)
		for i := range fetchedEvents {
			ev := &fetchedEvents[i]
			// Map back to original q tag values
			for _, qVal := range refsByEventID[ev.ID] {
				quotedEvents[qVal] = ev
			}
			pubkeys[ev.PubKey] = true
		}
	}

	// Fetch addressable events by kind + author + d-tag
	// The #a tag filter returns events that REFERENCE the address, not the address itself
	// So we need to parse the a-tag and query by kind, author, then match d-tag
	if len(aTags) > 0 {
		for _, aTag := range aTags {
			// Parse a-tag: "kind:pubkey:d-tag"
			parts := strings.SplitN(aTag, ":", 3)
			if len(parts) != 3 {
				continue
			}
			kind := 0
			fmt.Sscanf(parts[0], "%d", &kind)
			author := parts[1]
			dTag := parts[2]

			// Query for the specific addressable event
			// Fetch more than 1 in case the author has multiple articles with same kind
			filter := Filter{
				Kinds:   []int{kind},
				Authors: []string{author},
				Limit:   10,
			}
			fetchedEvents, _ := fetchEventsFromRelays(relays, filter)

			// Find the event with matching d-tag
			for i := range fetchedEvents {
				ev := &fetchedEvents[i]
				evDTag := ""
				for _, tag := range ev.Tags {
					if len(tag) >= 2 && tag[0] == "d" {
						evDTag = tag[1]
						break
					}
				}
				if evDTag == dTag {
					// Map back to original q tag values
					for _, qVal := range refsByATag[aTag] {
						quotedEvents[qVal] = ev
					}
					pubkeys[ev.PubKey] = true
					break
				}
			}
		}
	}

	// Fetch profiles for quoted event authors
	if len(pubkeys) > 0 {
		pks := make([]string, 0, len(pubkeys))
		for pk := range pubkeys {
			pks = append(pks, pk)
		}
		quotedEventProfiles = fetchProfiles(ConfigGetProfileRelays(), pks)
	}

	return quotedEvents, quotedEventProfiles
}

// buildQuotedEventItem creates an HTMLEventItem from a fetched quoted event
func buildQuotedEventItem(ev *Event, profile *ProfileInfo, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview) *HTMLEventItem {
	npub, _ := encodeBech32Pubkey(ev.PubKey)
	kindDef := GetKindDefinition(ev.Kind)

	item := &HTMLEventItem{
		ID:             ev.ID,
		Kind:           ev.Kind,
		Tags:           ev.Tags,
		TemplateName:   kindDef.TemplateName,
		RenderTemplate: computeRenderTemplate(kindDef.TemplateName, ev.Tags),
		Pubkey:         ev.PubKey,
		Npub:           npub,
		NpubShort:      formatNpubShort(npub),
		CreatedAt:      ev.CreatedAt,
		Content:        ev.Content,
		ContentHTML:    processContentToHTMLFull(ev.Content, relays, resolvedRefs, linkPreviews),
		AuthorProfile:  profile,
	}

	// Extract kind-specific metadata
	if kindDef.ExtractTitle {
		item.Title = extractTitle(ev.Tags)
	}
	if kindDef.ExtractSummary {
		item.Summary = extractSummary(ev.Tags)
		item.HeaderImage = extractHeaderImage(ev.Tags)
	}
	if kindDef.ExtractImages {
		item.ImagesHTML = extractImetaImages(ev.Tags)
	}

	return item
}

// stripQuotedNostrRef removes nostr references that point to the quoted event
// Handles nevent1, note1, and naddr1 references
// quotedRef is the original q tag value (could be event ID, nevent, note, or naddr)
func stripQuotedNostrRef(content string, quotedRef string) string {
	// Parse the quoted reference to understand what we're looking for
	qRef := parseQuotedRef(quotedRef)

	// Match nostr:nevent1..., nostr:note1..., or nostr:naddr1... patterns
	nostrRefPattern := regexp.MustCompile(`nostr:(nevent1[a-z0-9]+|note1[a-z0-9]+|naddr1[a-z0-9]+)`)
	return nostrRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		identifier := strings.TrimPrefix(match, "nostr:")

		// Check if this reference matches our quoted event
		if strings.HasPrefix(identifier, "nevent1") {
			if ne, err := DecodeNEvent(identifier); err == nil {
				if ne.EventID == qRef.EventID {
					return ""
				}
			}
		} else if strings.HasPrefix(identifier, "note1") {
			if id, err := DecodeNote(identifier); err == nil {
				if id == qRef.EventID {
					return ""
				}
			}
		} else if strings.HasPrefix(identifier, "naddr1") {
			if na, err := DecodeNAddr(identifier); err == nil {
				aTag := fmt.Sprintf("%d:%s:%s", na.Kind, na.Author, na.DTag)
				// Compare full a-tag if d-tag present, otherwise compare kind:author prefix
				if aTag == qRef.ATag {
					return ""
				}
				// Also match if the naddr doesn't have a d-tag but kind:author matches
				if na.DTag == "" && qRef.ATag != "" {
					prefix := fmt.Sprintf("%d:%s:", na.Kind, na.Author)
					if strings.HasPrefix(qRef.ATag, prefix) {
						return ""
					}
				}
			}
		}

		// Keep other references
		return match
	})
}

// nostrRefToLink converts a nostr identifier to a descriptive link
func nostrRefToLink(identifier string) string {
	switch {
	case strings.HasPrefix(identifier, "nevent1"):
		if ne, err := DecodeNEvent(identifier); err == nil {
			return fmt.Sprintf(`<a href="/html/thread/%s" class="nostr-ref nostr-ref-event">View quoted note →</a>`,
				html.EscapeString(ne.EventID))
		}
	case strings.HasPrefix(identifier, "note1"):
		if eventID, err := DecodeNote(identifier); err == nil {
			return fmt.Sprintf(`<a href="/html/thread/%s" class="nostr-ref nostr-ref-event">View quoted note →</a>`,
				html.EscapeString(eventID))
		}
	case strings.HasPrefix(identifier, "nprofile1"):
		if np, err := DecodeNProfile(identifier); err == nil {
			username := getCachedUsername(np.Pubkey)
			return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref nostr-ref-profile">%s</a>`,
				html.EscapeString(np.Pubkey), html.EscapeString(username))
		}
	case strings.HasPrefix(identifier, "npub1"):
		if pubkey, err := decodeBech32Pubkey(identifier); err == nil {
			username := getCachedUsername(pubkey)
			return fmt.Sprintf(`<a href="/html/profile/%s" class="nostr-ref nostr-ref-profile">%s</a>`,
				html.EscapeString(pubkey), html.EscapeString(username))
		}
	case strings.HasPrefix(identifier, "naddr1"):
		// naddr references replaceable events (often long-form articles)
		if na, err := DecodeNAddr(identifier); err == nil {
			// Determine content type label from kind registry
			kindDef := GetKindDefinition(int(na.Kind))
			label := "View " + kindDef.Label() + " →"
			// Link directly to thread with naddr - handler will decode and fetch
			return fmt.Sprintf(`<a href="/html/thread/%s" class="nostr-ref nostr-ref-addr" title="kind:%d">%s</a>`,
				html.EscapeString(identifier), na.Kind, label)
		}
	}
	// Fallback - return as-is
	return "nostr:" + html.EscapeString(identifier)
}

// resolveNostrReference renders a nostr reference as a styled link
// NOTE: Does NOT fetch events/profiles to keep rendering fast - just creates navigable links
func resolveNostrReference(identifier string, relays []string) string {
	// Use the fast link-only approach for all reference types
	return nostrRefToLink(identifier)
}

// kindsToString converts a slice of kind integers to a comma-separated string
func kindsToString(kinds []int) string {
	if len(kinds) == 0 {
		return ""
	}
	strs := make([]string, len(kinds))
	for i, k := range kinds {
		strs[i] = strconv.Itoa(k)
	}
	return strings.Join(strs, ",")
}

// computeKindFilter determines the active kind filter from the kinds parameter
// Returns: "all", "notes", "photos", "reads", "bookmarks", "highlights", or "livestreams"
func computeKindFilter(kinds []int) string {
	if len(kinds) == 0 {
		return "all"
	}
	// Check for specific filter patterns
	if len(kinds) == 2 && ((kinds[0] == 1 && kinds[1] == 6) || (kinds[0] == 6 && kinds[1] == 1)) {
		return "notes"
	}
	if len(kinds) == 1 {
		switch kinds[0] {
		case 1:
			return "notes" // Also match single kind=1
		case 20:
			return "photos"
		case 30023:
			return "reads"
		case 10003:
			return "bookmarks"
		case 9802:
			return "highlights"
		case 30311:
			return "livestreams"
		}
	}
	return "all" // Unknown filter pattern, default to all
}

func renderHTML(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int, session *BunkerSession, errorMsg, successMsg string, feedMode string, currentURL string, themeClass, themeLabel string, csrfToken string, hasUnreadNotifs bool, isFragment bool, isAppend bool, newestTimestamp int64, repostEvents map[string]*Event) (string, error) {
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, len(resp.Items))
	for i, item := range resp.Items {
		contents[i] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

	// Pre-fetch profiles for live event participants from profile relays
	// Live events (kind 30311) have participant pubkeys in p tags
	liveParticipantPubkeys := make(map[string]bool)
	livestreamKind := GetKindDefinition(30311)
	for _, item := range resp.Items {
		if item.Kind == livestreamKind.Kind {
			liveInfo := parseLiveEvent(item.Tags)
			if liveInfo != nil {
				for _, pk := range liveInfo.ParticipantPubkeys {
					liveParticipantPubkeys[pk] = true
				}
			}
		}
	}
	var liveParticipantProfiles map[string]*ProfileInfo
	if len(liveParticipantPubkeys) > 0 {
		pubkeys := make([]string, 0, len(liveParticipantPubkeys))
		for pk := range liveParticipantPubkeys {
			pubkeys = append(pubkeys, pk)
		}
		// Fetch from profile relays for better profile coverage
		liveParticipantProfiles = fetchProfiles(ConfigGetProfileRelays(), pubkeys)
	}

	// Build profiles map for kind processing context (combines author and participant profiles)
	allProfiles := make(map[string]*ProfileInfo)
	for _, it := range resp.Items {
		if it.AuthorProfile != nil {
			allProfiles[it.Pubkey] = it.AuthorProfile
		}
	}
	for pk, profile := range liveParticipantProfiles {
		allProfiles[pk] = profile
	}

	// Pre-fetch quoted events for quote posts (kinds that support q tags)
	// Collect all q tag values (can be hex IDs, note1, nevent1, or naddr1)
	var qTagValues []string
	for _, item := range resp.Items {
		itemKindDef := GetKindDefinition(item.Kind)
		if itemKindDef.SupportsQuotePosts {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValues = append(qTagValues, tag[1])
					break // Only one q tag per event
				}
			}
		}
	}

	// Batch fetch quoted events (handles both regular IDs and naddr references)
	quotedEvents, quotedEventProfiles := fetchQuotedEvents(qTagValues)

	// Convert to HTML page data
	items := make([]HTMLEventItem, len(resp.Items))
	for i, item := range resp.Items {
		// Generate npub from hex pubkey
		npub, _ := encodeBech32Pubkey(item.Pubkey)

		// Get kind definition for this event
		kindDef := GetKindDefinition(item.Kind)

		items[i] = HTMLEventItem{
			ID:             item.ID,
			Kind:           item.Kind,
			Tags:           item.Tags,
			TemplateName:   kindDef.TemplateName,
			RenderTemplate: computeRenderTemplate(kindDef.TemplateName, item.Tags),
			Pubkey:         item.Pubkey,
			Npub:           npub,
			NpubShort:      formatNpubShort(npub),
			CreatedAt:      item.CreatedAt,
			Content:        item.Content,
			ContentHTML:    processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
			RelaysSeen:     item.RelaysSeen,
			Links:          []string{},
			AuthorProfile:  item.AuthorProfile,
			Reactions:      item.Reactions,
			ReplyCount:     item.ReplyCount,
		}

		// Check if logged-in user has bookmarked, reacted, reposted, or muted this event's author
		if session != nil && session.Connected {
			items[i].IsBookmarked = session.IsEventBookmarked(item.ID)
			items[i].IsReacted = session.IsEventReacted(item.ID)
			items[i].IsReposted = session.IsEventReposted(item.ID)
			items[i].IsZapped = session.IsEventZapped(item.ID)
			items[i].IsMuted = session.IsPubkeyMuted(item.Pubkey)
		}

		// Extract kind-specific metadata using KindDefinition hints
		if kindDef.ExtractImages {
			items[i].ImagesHTML = extractImetaImages(item.Tags)
		}
		if kindDef.ExtractTitle {
			items[i].Title = extractTitle(item.Tags)
		}
		if kindDef.ExtractSummary {
			items[i].Summary = extractSummary(item.Tags)
			items[i].HeaderImage = extractHeaderImage(item.Tags)
			items[i].PublishedAt = extractPublishedAt(item.Tags)
		}
		// Render markdown for kinds that support it
		if kindDef.RenderMarkdown {
			items[i].ContentHTML = renderMarkdown(item.Content)
		}

		// Parse embedded event for reposts (handles both embedded JSON and reference-only reposts)
		if kindDef.IsRepost {
			// Build profiles map from response items for reposted author lookup
			profilesMap := make(map[string]*ProfileInfo)
			for _, it := range resp.Items {
				if it.AuthorProfile != nil {
					profilesMap[it.Pubkey] = it.AuthorProfile
				}
			}
			items[i].RepostedEvent = parseRepostedEvent(item.Content, item.Tags, relays, resolvedRefs, linkPreviews, profilesMap, repostEvents)
			// Check if logged-in user has bookmarked, reacted, reposted, or muted the reposted event's author
			if items[i].RepostedEvent != nil && session != nil && session.Connected {
				items[i].RepostedEvent.IsBookmarked = session.IsEventBookmarked(items[i].RepostedEvent.ID)
				items[i].RepostedEvent.IsReacted = session.IsEventReacted(items[i].RepostedEvent.ID)
				items[i].RepostedEvent.IsReposted = session.IsEventReposted(items[i].RepostedEvent.ID)
				items[i].RepostedEvent.IsZapped = session.IsEventZapped(items[i].RepostedEvent.ID)
				items[i].RepostedEvent.IsMuted = session.IsPubkeyMuted(items[i].RepostedEvent.Pubkey)
			}
		}

		// Attach quoted event for quote posts (kinds that support q tags)
		if kindDef.SupportsQuotePosts {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValue := tag[1]
					items[i].QuotedEventID = qTagValue
					// Always strip the nostr reference from content since we render the quoted box
					strippedContent := stripQuotedNostrRef(item.Content, qTagValue)
					items[i].ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
					// Check if we fetched this event (keyed by original q tag value)
					if qev, ok := quotedEvents[qTagValue]; ok {
						items[i].QuotedEvent = buildQuotedEventItem(qev, quotedEventProfiles[qev.PubKey], relays, resolvedRefs, linkPreviews)
					}
					break
				}
			}
		}

		// Apply kind-specific data using registered data appliers
		// This replaces all the hardcoded kind checks (9735 zap, 30311 live, 9802 highlight, etc.)
		kindDef.ApplyKindData(&items[i], item.Tags, &KindProcessingContext{
			Profiles: allProfiles,
			Relays:   relays,
		})

		// Add thread link if reply
		for _, tag := range item.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				items[i].Links = append(items[i].Links, fmt.Sprintf("/html/threads/%s", tag[1]))
				break
			}
		}
	}

	// Populate actions for each item
	loggedIn := session != nil && session.Connected
	hasWallet := loggedIn && session.HasWallet()
	var userPubkeyHex string
	if loggedIn {
		userPubkeyHex = hex.EncodeToString(session.UserPubKey)
	}
	loginURL := "/html/login?return_url=" + currentURL

	for i := range items {
		item := &items[i]

		// For reposts (kind 6), actions target the reposted event
		var targetID, targetPubkey string
		var targetKind int
		var targetReplyCount int
		var targetIsBookmarked, targetIsReacted, targetIsReposted, targetIsZapped, targetIsMuted bool

		itemKindDef := GetKindDefinition(item.Kind)
		if itemKindDef.IsRepost && item.RepostedEvent != nil {
			targetID = item.RepostedEvent.ID
			targetPubkey = item.RepostedEvent.Pubkey
			targetKind = item.RepostedEvent.Kind
			targetReplyCount = item.RepostedEvent.ReplyCount
			targetIsBookmarked = item.RepostedEvent.IsBookmarked
			targetIsReacted = item.RepostedEvent.IsReacted
			targetIsReposted = item.RepostedEvent.IsReposted
			targetIsZapped = item.RepostedEvent.IsZapped
			targetIsMuted = item.RepostedEvent.IsMuted
		} else {
			targetID = item.ID
			targetPubkey = item.Pubkey
			targetKind = item.Kind
			targetReplyCount = item.ReplyCount
			targetIsBookmarked = item.IsBookmarked
			targetIsReacted = item.IsReacted
			targetIsReposted = item.IsReposted
			targetIsZapped = item.IsZapped
			targetIsMuted = item.IsMuted
		}

		// Get reaction count from summary
		var targetReactionCount int
		if itemKindDef.IsRepost && item.RepostedEvent != nil && item.RepostedEvent.Reactions != nil {
			targetReactionCount = item.RepostedEvent.Reactions.Total
		} else if item.Reactions != nil {
			targetReactionCount = item.Reactions.Total
		}

		ctx := ActionContext{
			EventID:       targetID,
			EventPubkey:   targetPubkey,
			Kind:          targetKind,
			IsBookmarked:  targetIsBookmarked,
			IsReacted:     targetIsReacted,
			IsReposted:    targetIsReposted,
			IsZapped:      targetIsZapped,
			IsMuted:       targetIsMuted,
			ReplyCount:    targetReplyCount,
			ReactionCount: targetReactionCount,
			LoggedIn:      loggedIn,
			HasWallet:     hasWallet,
			IsAuthor:      loggedIn && targetPubkey == userPubkeyHex,
			CSRFToken:     csrfToken,
			ReturnURL:     currentURL,
			LoginURL:      loginURL,
		}

		// Use BuildHypermediaEntity for NATEOAS Phase 4 action discovery
		entity := BuildHypermediaEntity(ctx, item.Tags, nil)
		item.ActionGroups = GroupActionsForKind(entity.Actions, item.Kind)
		item.LoggedIn = loggedIn

		// Also populate actions for the reposted event if present
		if item.RepostedEvent != nil {
			var repostedReactionCount int
			if item.RepostedEvent.Reactions != nil {
				repostedReactionCount = item.RepostedEvent.Reactions.Total
			}
			repostedCtx := ActionContext{
				EventID:       item.RepostedEvent.ID,
				EventPubkey:   item.RepostedEvent.Pubkey,
				Kind:          item.RepostedEvent.Kind,
				IsBookmarked:  item.RepostedEvent.IsBookmarked,
				IsReacted:     item.RepostedEvent.IsReacted,
				IsReposted:    item.RepostedEvent.IsReposted,
				IsZapped:      item.RepostedEvent.IsZapped,
				IsMuted:       item.RepostedEvent.IsMuted,
				ReplyCount:    item.RepostedEvent.ReplyCount,
				ReactionCount: repostedReactionCount,
				LoggedIn:      loggedIn,
				HasWallet:     hasWallet,
				IsAuthor:      loggedIn && item.RepostedEvent.Pubkey == userPubkeyHex,
				CSRFToken:     csrfToken,
				ReturnURL:     currentURL,
				LoginURL:      loginURL,
			}
			repostedEntity := BuildHypermediaEntity(repostedCtx, item.RepostedEvent.Tags, nil)
			item.RepostedEvent.ActionGroups = GroupActionsForKind(repostedEntity.Actions, item.RepostedEvent.Kind)
			item.RepostedEvent.LoggedIn = loggedIn
		}
	}

	// Build pagination
	var pagination *HTMLPagination
	if resp.Page.Next != nil {
		// Page.Next is already the HTML path from html_handlers.go
		pagination = &HTMLPagination{
			Next: *resp.Page.Next,
		}
	}

	kindsStr := kindsToString(kinds)
	data := HTMLPageData{
		Title:               "Nostr Timeline",
		Meta:                &resp.Meta,
		Items:               items,
		Pagination:          pagination,
		Actions:             []HTMLAction{},
		Error:               errorMsg,
		Success:             successMsg,
		ShowPostForm:  true, // Only timeline has post form
		ShowGifButton: GiphyEnabled(),
		FeedMode:            feedMode,
		KindFilter:          computeKindFilter(kinds),
		KindsParam:          kindsStr,
		ActiveRelays:        relays,
		CurrentURL:          currentURL,
		ThemeClass:          themeClass,
		ThemeLabel:          themeLabel,
		CSRFToken:           csrfToken,
		NewestTimestamp:     newestTimestamp,
	}

	// Add session info if logged in
	var userAvatarURL string
	if session != nil && session.Connected {
		data.LoggedIn = true
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
		data.HasUnreadNotifications = hasUnreadNotifs
		userAvatarURL = getUserAvatarURL(pubkeyHex)
	}

	// Build navigation (NATEOAS)
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:    data.LoggedIn,
		ActiveFeed:  feedMode,
		CurrentPage: "timeline",
	})
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   data.LoggedIn,
		ActivePage: "",
		HasUnread:  hasUnreadNotifs,
	})
	data.KindFilters = GetKindFilters(KindFilterContext{
		LoggedIn:    data.LoggedIn,
		ActiveFeed:  feedMode,
		ActiveKinds: kindsStr,
	})
	settingsCtx := SettingsContext{
		LoggedIn:      data.LoggedIn,
		ThemeLabel:    themeLabel,
		FeedMode:      feedMode,
		KindFilter:    kindsStr,
		UserAvatarURL: userAvatarURL,
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	// Use cached template for better performance
	var buf strings.Builder
	if isAppend {
		// HelmJS "Load More" request: render items + updated pagination for append
		if err := cachedAppendFragment.ExecuteTemplate(&buf, tmplAppendFragment, data); err != nil {
			return "", err
		}
	} else if isFragment {
		// HelmJS request: render just the content fragment
		if err := cachedTimelineFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		// Full page request: render with base template
		if err := cachedHTMLTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}


// ReplyGroup represents a top-level reply and its nested children (two-level nesting).
// Direct replies to root are Parents, all their descendants are flattened into Children.
type ReplyGroup struct {
	Parent   HTMLEventItem   // Direct reply to the root
	Children []HTMLEventItem // All descendants of this reply, flattened
}

// groupRepliesIntoTwoLevels organizes replies into two-level nesting.
// Level 1: Direct replies to the root event
// Level 2: All descendants of each level-1 reply (flattened, sorted chronologically)
func groupRepliesIntoTwoLevels(replies []HTMLEventItem, rootID string) []ReplyGroup {
	if len(replies) == 0 {
		return nil
	}

	// Build a map of eventID -> reply for quick lookup
	replyMap := make(map[string]*HTMLEventItem)
	for i := range replies {
		replyMap[replies[i].ID] = &replies[i]
	}

	// Build parent->children map
	childrenOf := make(map[string][]string) // parentID -> list of child IDs
	for _, reply := range replies {
		parentID := reply.ParentID
		if parentID == "" {
			parentID = rootID // If no parent, assume it's a direct reply to root
		}
		childrenOf[parentID] = append(childrenOf[parentID], reply.ID)
	}

	// Find the top-level ancestor (level-1 reply) for any given reply
	// Returns the ID of the direct child of root that this reply descends from
	findTopLevelAncestor := func(replyID string) string {
		visited := make(map[string]bool)
		current := replyID
		for {
			if visited[current] {
				return current // Cycle detected, just return current
			}
			visited[current] = true

			reply, exists := replyMap[current]
			if !exists {
				return current // Reply not in our map, must be top-level
			}
			parentID := reply.ParentID
			if parentID == "" || parentID == rootID {
				return current // This is a direct reply to root
			}
			current = parentID
		}
	}

	// Collect all descendants of a given reply (for level-2 children)
	collectDescendants := func(parentID string) []HTMLEventItem {
		var descendants []HTMLEventItem
		var stack []string
		stack = append(stack, childrenOf[parentID]...)

		visited := make(map[string]bool)
		for len(stack) > 0 {
			childID := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			if visited[childID] {
				continue
			}
			visited[childID] = true

			if child, exists := replyMap[childID]; exists {
				descendants = append(descendants, *child)
				stack = append(stack, childrenOf[childID]...)
			}
		}

		// Sort descendants chronologically
		sort.Slice(descendants, func(i, j int) bool {
			return descendants[i].CreatedAt < descendants[j].CreatedAt
		})

		return descendants
	}

	// Group replies: level-1 replies are direct children of root
	directReplies := childrenOf[rootID]
	groups := make([]ReplyGroup, 0, len(directReplies))

	// Track which replies we've assigned to groups
	assigned := make(map[string]bool)

	// Sort direct replies chronologically
	sort.Slice(directReplies, func(i, j int) bool {
		r1, r2 := replyMap[directReplies[i]], replyMap[directReplies[j]]
		if r1 == nil || r2 == nil {
			return false
		}
		return r1.CreatedAt < r2.CreatedAt
	})

	for _, replyID := range directReplies {
		parent, exists := replyMap[replyID]
		if !exists {
			continue
		}
		assigned[replyID] = true

		children := collectDescendants(replyID)
		for _, child := range children {
			assigned[child.ID] = true
		}

		groups = append(groups, ReplyGroup{
			Parent:   *parent,
			Children: children,
		})
	}

	// Handle orphaned replies (replies whose parent isn't in our reply set)
	// Find their top-level ancestor and add them as a new group if needed
	for _, reply := range replies {
		if assigned[reply.ID] {
			continue
		}

		ancestorID := findTopLevelAncestor(reply.ID)
		if ancestorID == reply.ID {
			// This reply's parent is outside our set, treat it as a top-level reply
			assigned[reply.ID] = true
			children := collectDescendants(reply.ID)
			for _, child := range children {
				assigned[child.ID] = true
			}
			groups = append(groups, ReplyGroup{
				Parent:   reply,
				Children: children,
			})
		}
	}

	// Final sort of groups by parent's creation time
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Parent.CreatedAt < groups[j].Parent.CreatedAt
	})

	return groups
}

type HTMLThreadData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	Meta                   *MetaInfo
	Root                   *HTMLEventItem
	ReplyGroups            []ReplyGroup // Two-level nested replies
	TotalReplyCount        int          // Total number of replies across all groups
	LoggedIn               bool
	UserPubKey             string
	UserDisplayName        string
	UserAvatarURL          string
	UserNpub               string
	CurrentURL             string
	ThemeClass             string // "dark", "light", or "" for system default
	ThemeLabel             string // Label for theme toggle button
	Error                  string
	Success                string
	CSRFToken              string // CSRF token for form submission
	HasUnreadNotifications bool   // Whether there are notifications newer than last seen
	ShowPostForm           bool   // For base template compatibility (always false for thread)
	ShowGifButton          bool   // Show GIF button in reply form (depends on GIPHY_API_KEY)
	FeedMode               string   // For base template compatibility
	ActiveRelays           []string // For base template compatibility
	// Navigation (NATEOAS)
	FeedModes      []FeedMode
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	KindFilters    []KindFilter // For base template compatibility (always empty for thread)
}

// extractParentID extracts the parent event ID from the "e" tags
// The parent is typically the last "e" tag, or the one marked as "reply"
func extractParentID(tags [][]string) string {
	var parentID string
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "e" {
			// Check if this tag has a marker
			if len(tag) >= 4 && tag[3] == "reply" {
				return tag[1] // This is explicitly marked as the reply target
			}
			// Otherwise, use the last "e" tag as the parent
			parentID = tag[1]
		}
	}
	return parentID
}

func renderThreadHTML(resp ThreadResponse, relays []string, session *BunkerSession, currentURL string, themeClass, themeLabel, successMsg, csrfToken string, hasUnreadNotifs bool, isFragment bool, repostEvents map[string]*Event) (string, error) {
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, 1+len(resp.Replies))
	contents[0] = resp.Root.Content
	for i, item := range resp.Replies {
		contents[i+1] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

	// Collect q tags for quote post processing (kinds that support quote posts)
	// q tags can be hex IDs, note1, nevent1, naddr1, or raw a-tag format (kind:pubkey:d-tag)
	var qTagValues []string
	rootKindDef := GetKindDefinition(resp.Root.Kind)
	if rootKindDef.SupportsQuotePosts {
		for _, tag := range resp.Root.Tags {
			if len(tag) >= 2 && tag[0] == "q" {
				qTagValues = append(qTagValues, tag[1])
			}
		}
	}
	for _, item := range resp.Replies {
		replyKindDef := GetKindDefinition(item.Kind)
		if replyKindDef.SupportsQuotePosts {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValues = append(qTagValues, tag[1])
				}
			}
		}
	}

	// Fetch quoted events and their profiles (handles both regular IDs and naddr references)
	quotedEvents, quotedEventProfiles := fetchQuotedEvents(qTagValues)

	// Generate npub for root author
	rootNpub, _ := encodeBech32Pubkey(resp.Root.Pubkey)

	// Convert root to HTML item
	root := &HTMLEventItem{
		ID:             resp.Root.ID,
		Kind:           resp.Root.Kind,
		Tags:           resp.Root.Tags,
		TemplateName:   rootKindDef.TemplateName,
		RenderTemplate: computeRenderTemplate(rootKindDef.TemplateName, resp.Root.Tags),
		Pubkey:         resp.Root.Pubkey,
		Npub:           rootNpub,
		NpubShort:      formatNpubShort(rootNpub),
		CreatedAt:      resp.Root.CreatedAt,
		Content:        resp.Root.Content,
		ContentHTML:    processContentToHTMLFull(resp.Root.Content, relays, resolvedRefs, linkPreviews),
		RelaysSeen:     resp.Root.RelaysSeen,
		AuthorProfile:  resp.Root.AuthorProfile,
		ReplyCount:     resp.Root.ReplyCount,
		ParentID:       extractParentID(resp.Root.Tags),
	}

	// Check if logged-in user has bookmarked, reacted, reposted, or muted the root event's author
	if session != nil && session.Connected {
		root.IsBookmarked = session.IsEventBookmarked(resp.Root.ID)
		root.IsReacted = session.IsEventReacted(resp.Root.ID)
		root.IsReposted = session.IsEventReposted(resp.Root.ID)
		root.IsZapped = session.IsEventZapped(resp.Root.ID)
		root.IsMuted = session.IsPubkeyMuted(resp.Root.Pubkey)
	}

	// Extract kind-specific metadata using KindDefinition
	if rootKindDef.ExtractTitle {
		root.Title = extractTitle(resp.Root.Tags)
	}
	if rootKindDef.ExtractSummary {
		root.Summary = extractSummary(resp.Root.Tags)
		root.HeaderImage = extractHeaderImage(resp.Root.Tags)
		root.PublishedAt = extractPublishedAt(resp.Root.Tags)
	}
	if rootKindDef.RenderMarkdown {
		root.ContentHTML = renderMarkdown(resp.Root.Content)
	}

	// Handle quote posts for root event (kinds that support q tags)
	if rootKindDef.SupportsQuotePosts {
		for _, tag := range resp.Root.Tags {
			if len(tag) >= 2 && tag[0] == "q" {
				qTagValue := tag[1]
				root.QuotedEventID = qTagValue
				// Strip the nostr reference from content since we render the quoted box
				strippedContent := stripQuotedNostrRef(resp.Root.Content, qTagValue)
				root.ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
				// Check if we fetched this event (keyed by original q tag value)
				if qev, ok := quotedEvents[qTagValue]; ok {
					root.QuotedEvent = buildQuotedEventItem(qev, quotedEventProfiles[qev.PubKey], relays, resolvedRefs, linkPreviews)
				}
				break
			}
		}
	}

	// Handle reposts for root event (kind 6) - both embedded JSON and reference-only
	if rootKindDef.IsRepost {
		// Build profiles map from response items for reposted author lookup
		profilesMap := make(map[string]*ProfileInfo)
		if resp.Root.AuthorProfile != nil {
			profilesMap[resp.Root.Pubkey] = resp.Root.AuthorProfile
		}
		for _, item := range resp.Replies {
			if item.AuthorProfile != nil {
				profilesMap[item.Pubkey] = item.AuthorProfile
			}
		}
		root.RepostedEvent = parseRepostedEvent(resp.Root.Content, resp.Root.Tags, relays, resolvedRefs, linkPreviews, profilesMap, repostEvents)
		// Check if logged-in user has bookmarked, reacted, reposted, or muted the reposted event's author
		if root.RepostedEvent != nil && session != nil && session.Connected {
			root.RepostedEvent.IsBookmarked = session.IsEventBookmarked(root.RepostedEvent.ID)
			root.RepostedEvent.IsReacted = session.IsEventReacted(root.RepostedEvent.ID)
			root.RepostedEvent.IsReposted = session.IsEventReposted(root.RepostedEvent.ID)
			root.RepostedEvent.IsZapped = session.IsEventZapped(root.RepostedEvent.ID)
			root.RepostedEvent.IsMuted = session.IsPubkeyMuted(root.RepostedEvent.Pubkey)
		}
	}

	// Build a map of event IDs to author info for "replying to" lookups
	// Include root and all replies
	type authorInfo struct {
		pubkey  string
		npub    string
		profile *ProfileInfo
	}
	authorsByEventID := make(map[string]authorInfo)
	authorsByEventID[resp.Root.ID] = authorInfo{
		pubkey:  resp.Root.Pubkey,
		npub:    rootNpub,
		profile: resp.Root.AuthorProfile,
	}
	for _, item := range resp.Replies {
		itemNpub, _ := encodeBech32Pubkey(item.Pubkey)
		authorsByEventID[item.ID] = authorInfo{
			pubkey:  item.Pubkey,
			npub:    itemNpub,
			profile: item.AuthorProfile,
		}
	}

	// Convert replies to HTML items
	replies := make([]HTMLEventItem, len(resp.Replies))
	for i, item := range resp.Replies {
		npub, _ := encodeBech32Pubkey(item.Pubkey)
		replyKindDef := GetKindDefinition(item.Kind)
		parentID := extractParentID(item.Tags)

		replies[i] = HTMLEventItem{
			ID:             item.ID,
			Kind:           item.Kind,
			Tags:           item.Tags,
			TemplateName:   replyKindDef.TemplateName,
			RenderTemplate: computeRenderTemplate(replyKindDef.TemplateName, item.Tags),
			Pubkey:         item.Pubkey,
			Npub:           npub,
			NpubShort:      formatNpubShort(npub),
			CreatedAt:      item.CreatedAt,
			Content:        item.Content,
			ContentHTML:    processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
			RelaysSeen:     item.RelaysSeen,
			AuthorProfile:  item.AuthorProfile,
			ReplyCount:     item.ReplyCount,
			ParentID:       parentID,
		}

		// Set "replying to" info if this is a reply to another reply (not the root)
		if parentID != "" && parentID != resp.Root.ID {
			if parentAuthor, ok := authorsByEventID[parentID]; ok {
				// Get display name
				if parentAuthor.profile != nil {
					if parentAuthor.profile.DisplayName != "" {
						replies[i].ReplyToName = parentAuthor.profile.DisplayName
					} else if parentAuthor.profile.Name != "" {
						replies[i].ReplyToName = parentAuthor.profile.Name
					} else {
						replies[i].ReplyToName = formatNpubShort(parentAuthor.npub)
					}
				} else {
					replies[i].ReplyToName = formatNpubShort(parentAuthor.npub)
				}
				replies[i].ReplyToNpub = parentAuthor.npub
			}
		}

		// Check if logged-in user has bookmarked, reacted, reposted, or muted this reply's author
		if session != nil && session.Connected {
			replies[i].IsBookmarked = session.IsEventBookmarked(item.ID)
			replies[i].IsReacted = session.IsEventReacted(item.ID)
			replies[i].IsReposted = session.IsEventReposted(item.ID)
			replies[i].IsZapped = session.IsEventZapped(item.ID)
			replies[i].IsMuted = session.IsPubkeyMuted(item.Pubkey)
		}

		// Handle quote posts for replies (kinds that support q tags)
		itemKindDef := GetKindDefinition(item.Kind)
		if itemKindDef.SupportsQuotePosts {
			for _, tag := range item.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValue := tag[1]
					replies[i].QuotedEventID = qTagValue
					// Strip the nostr reference from content since we render the quoted box
					strippedContent := stripQuotedNostrRef(item.Content, qTagValue)
					replies[i].ContentHTML = processContentToHTMLFull(strippedContent, relays, resolvedRefs, linkPreviews)
					// Check if we fetched this event (keyed by original q tag value)
					if qev, ok := quotedEvents[qTagValue]; ok {
						replies[i].QuotedEvent = buildQuotedEventItem(qev, quotedEventProfiles[qev.PubKey], relays, resolvedRefs, linkPreviews)
					}
					break
				}
			}
		}
	}

	// Populate actions for root and replies
	loggedIn := session != nil && session.Connected
	hasWallet := loggedIn && session.HasWallet()
	var userPubkeyHex string
	if loggedIn {
		userPubkeyHex = hex.EncodeToString(session.UserPubKey)
	}
	loginURL := "/html/login?return_url=" + currentURL

	// Actions for root event (thread view doesn't show reply action since there's a reply form)
	var rootReactionCount int
	if root.Reactions != nil {
		rootReactionCount = root.Reactions.Total
	}
	rootCtx := ActionContext{
		EventID:       root.ID,
		EventPubkey:   root.Pubkey,
		Kind:          root.Kind,
		IsBookmarked:  root.IsBookmarked,
		IsReacted:     root.IsReacted,
		IsReposted:    root.IsReposted,
		IsZapped:      root.IsZapped,
		IsMuted:       root.IsMuted,
		ReplyCount:    root.ReplyCount,
		ReactionCount: rootReactionCount,
		LoggedIn:      loggedIn,
		HasWallet:     hasWallet,
		IsAuthor:      loggedIn && root.Pubkey == userPubkeyHex,
		CSRFToken:     csrfToken,
		ReturnURL:     currentURL,
		LoginURL:      loginURL,
	}
	rootEntity := BuildHypermediaEntity(rootCtx, root.Tags, nil)
	// Filter out "reply" action for root since thread has a dedicated reply form
	var filteredRootActions []ActionDefinition
	for _, def := range rootEntity.Actions {
		if def.Name != "reply" {
			filteredRootActions = append(filteredRootActions, def)
		}
	}
	root.ActionGroups = GroupActionsForKind(filteredRootActions, root.Kind)
	root.LoggedIn = loggedIn

	// Actions for replies
	for i := range replies {
		reply := &replies[i]
		var replyReactionCount int
		if reply.Reactions != nil {
			replyReactionCount = reply.Reactions.Total
		}
		ctx := ActionContext{
			EventID:       reply.ID,
			EventPubkey:   reply.Pubkey,
			Kind:          reply.Kind,
			IsBookmarked:  reply.IsBookmarked,
			IsReacted:     reply.IsReacted,
			IsReposted:    reply.IsReposted,
			IsZapped:      reply.IsZapped,
			IsMuted:       reply.IsMuted,
			ReplyCount:    reply.ReplyCount,
			ReactionCount: replyReactionCount,
			LoggedIn:      loggedIn,
			HasWallet:     hasWallet,
			IsAuthor:      loggedIn && reply.Pubkey == userPubkeyHex,
			CSRFToken:     csrfToken,
			ReturnURL:     currentURL,
			LoginURL:      loginURL,
		}
		replyEntity := BuildHypermediaEntity(ctx, reply.Tags, nil)
		reply.ActionGroups = GroupActionsForKind(replyEntity.Actions, reply.Kind)
		reply.LoggedIn = loggedIn
	}

	// Group replies into two-level nesting
	replyGroups := groupRepliesIntoTwoLevels(replies, resp.Root.ID)

	// Calculate total reply count across all groups
	totalReplyCount := 0
	for _, g := range replyGroups {
		totalReplyCount++ // Count the parent
		totalReplyCount += len(g.Children)
	}

	// Build SEO meta data from root event
	pageDescription := root.Content
	if len(pageDescription) > 200 {
		pageDescription = pageDescription[:197] + "..."
	}
	var pageImage string
	if root.AuthorProfile != nil && root.AuthorProfile.Picture != "" {
		pageImage = root.AuthorProfile.Picture
	}

	data := HTMLThreadData{
		Title:           "Thread",
		PageDescription: pageDescription,
		PageImage:       pageImage,
		CanonicalURL:    currentURL,
		Meta:            &resp.Meta,
		Root:            root,
		ReplyGroups:     replyGroups,
		TotalReplyCount: totalReplyCount,
		CurrentURL:      currentURL,
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		Success:         successMsg,
		CSRFToken:       csrfToken,
		ShowGifButton:   GiphyEnabled(),
	}

	// Add session info
	if session != nil && session.Connected {
		data.LoggedIn = true
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		data.UserPubKey = pubkeyHex
		data.UserDisplayName = getUserDisplayName(pubkeyHex)
		data.UserAvatarURL = getUserAvatarURL(pubkeyHex)
		data.UserNpub, _ = encodeBech32Pubkey(pubkeyHex)
		data.HasUnreadNotifications = hasUnreadNotifs
	}

	// Build navigation (NATEOAS)
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:    data.LoggedIn,
		ActiveFeed:  "",
		CurrentPage: "thread",
	})
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   data.LoggedIn,
		ActivePage: "",
		HasUnread:  hasUnreadNotifs,
	})
	var userAvatarURL string
	if session != nil && session.Connected {
		userAvatarURL = getUserAvatarURL(hex.EncodeToString(session.UserPubKey))
	}
	settingsCtx := SettingsContext{
		LoggedIn:      data.LoggedIn,
		ThemeLabel:    themeLabel,
		UserAvatarURL: userAvatarURL,
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	// Use cached template for better performance
	var buf strings.Builder
	if isFragment {
		// HelmJS request: render just the content fragment
		if err := cachedThreadFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		// Full page request: render with base template
		if err := cachedThreadTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}


type HTMLProfileData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	Pubkey                 string
	Npub                   string
	NpubShort              string
	Profile                *ProfileInfo
	Items                  []HTMLEventItem
	Pagination             *HTMLPagination
	Meta                   *MetaInfo
	ThemeClass             string // "dark", "light", or "" for system default
	ThemeLabel             string // Label for theme toggle button
	LoggedIn               bool
	CurrentURL             string
	CSRFToken              string // CSRF token for form submission
	IsFollowing            bool   // Whether logged-in user follows this profile
	IsSelf                 bool   // Whether this is the logged-in user's own profile
	IsMuted                bool   // Whether logged-in user has muted this profile
	HasUnreadNotifications bool   // Whether there are notifications newer than last seen
	ShowPostForm           bool   // For base template compatibility (always false for profile)
	FeedMode               string // For base template compatibility
	ActiveRelays           []string // For base template compatibility
	// Edit mode fields
	EditMode   bool   // Whether showing edit form instead of notes
	RawContent string // JSON of raw profile content (for preserving unknown fields)
	Error      string // Error message for edit form
	Success    string // Success message for edit form
	// Navigation (NATEOAS)
	FeedModes      []FeedMode
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	KindFilters    []KindFilter // For base template compatibility (always empty for profile)
}

func renderProfileHTML(resp ProfileResponse, relays []string, limit int, themeClass, themeLabel string, loggedIn bool, currentURL, csrfToken string, isFollowing, isSelf, hasUnreadNotifs bool, isFragment bool, isAppend bool, session *BunkerSession) (string, error) {
	// Pre-fetch all nostr: references in parallel for much faster rendering
	contents := make([]string, len(resp.Notes.Items))
	for i, item := range resp.Notes.Items {
		contents[i] = item.Content
	}
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Pre-fetch link previews for all URLs
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

	// Generate npub from hex pubkey
	npub, _ := encodeBech32Pubkey(resp.Pubkey)

	// Convert notes to HTML items
	items := make([]HTMLEventItem, len(resp.Notes.Items))
	for i, item := range resp.Notes.Items {
		kindDef := GetKindDefinition(item.Kind)
		items[i] = HTMLEventItem{
			ID:             item.ID,
			Kind:           item.Kind,
			Tags:           item.Tags,
			TemplateName:   kindDef.TemplateName,
			RenderTemplate: computeRenderTemplate(kindDef.TemplateName, item.Tags),
			Pubkey:         item.Pubkey,
			CreatedAt:      item.CreatedAt,
			Content:        item.Content,
			ContentHTML:    processContentToHTMLFull(item.Content, relays, resolvedRefs, linkPreviews),
			RelaysSeen:     item.RelaysSeen,
			AuthorProfile:  item.AuthorProfile,
		}

		// Check if logged-in user has bookmarked, reacted, reposted, or muted this event's author
		if session != nil && session.Connected {
			items[i].IsBookmarked = session.IsEventBookmarked(item.ID)
			items[i].IsReacted = session.IsEventReacted(item.ID)
			items[i].IsReposted = session.IsEventReposted(item.ID)
			items[i].IsZapped = session.IsEventZapped(item.ID)
			items[i].IsMuted = session.IsPubkeyMuted(item.Pubkey)
		}
	}

	// Populate actions for each item
	hasWallet := loggedIn && session != nil && session.HasWallet()
	loginURL := "/html/login?return_url=" + currentURL
	for i := range items {
		item := &items[i]
		var itemReactionCount int
		if item.Reactions != nil {
			itemReactionCount = item.Reactions.Total
		}
		ctx := ActionContext{
			EventID:       item.ID,
			EventPubkey:   item.Pubkey,
			Kind:          item.Kind,
			IsBookmarked:  item.IsBookmarked,
			IsReacted:     item.IsReacted,
			IsReposted:    item.IsReposted,
			IsZapped:      item.IsZapped,
			IsMuted:       item.IsMuted,
			ReplyCount:    item.ReplyCount,
			ReactionCount: itemReactionCount,
			LoggedIn:      loggedIn,
			HasWallet:     hasWallet,
			IsAuthor:      isSelf, // On profile page, isSelf indicates if viewing own profile
			CSRFToken:     csrfToken,
			ReturnURL:     currentURL,
			LoginURL:      loginURL,
		}
		entity := BuildHypermediaEntity(ctx, item.Tags, nil)
		item.ActionGroups = GroupActionsForKind(entity.Actions, item.Kind)
		item.LoggedIn = loggedIn
	}

	// Build pagination
	var pagination *HTMLPagination
	if resp.Notes.Page.Next != nil {
		pagination = &HTMLPagination{
			Next: *resp.Notes.Page.Next,
		}
	}

	// Get display name for title
	title := "Profile"
	if resp.Profile != nil {
		if resp.Profile.DisplayName != "" {
			title = resp.Profile.DisplayName
		} else if resp.Profile.Name != "" {
			title = resp.Profile.Name
		}
	}

	// Check if logged-in user has muted this profile
	var isMuted bool
	if session != nil && session.Connected {
		isMuted = session.IsPubkeyMuted(resp.Pubkey)
	}

	// Build SEO meta data from profile
	var pageDescription, pageImage string
	if resp.Profile != nil {
		if resp.Profile.About != "" {
			pageDescription = resp.Profile.About
			if len(pageDescription) > 200 {
				pageDescription = pageDescription[:197] + "..."
			}
		}
		if resp.Profile.Picture != "" {
			pageImage = resp.Profile.Picture
		}
	}

	data := HTMLProfileData{
		Title:                  title,
		PageDescription:        pageDescription,
		PageImage:              pageImage,
		CanonicalURL:           currentURL,
		Pubkey:                 resp.Pubkey,
		Npub:                   npub,
		NpubShort:              formatNpubShort(npub),
		Profile:                resp.Profile,
		Items:                  items,
		Pagination:             pagination,
		Meta:                   &resp.Notes.Meta,
		ThemeClass:             themeClass,
		ThemeLabel:             themeLabel,
		LoggedIn:               loggedIn,
		CurrentURL:             currentURL,
		CSRFToken:              csrfToken,
		IsFollowing:            isFollowing,
		IsSelf:                 isSelf,
		IsMuted:                isMuted,
		HasUnreadNotifications: hasUnreadNotifs,
	}

	// Build navigation (NATEOAS)
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:    loggedIn,
		ActiveFeed:  "", // No active feed on profile page
		CurrentPage: "profile",
	})
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   loggedIn,
		ActivePage: "",
		HasUnread:  hasUnreadNotifs,
	})
	var userAvatarURL string
	if session != nil && session.Connected {
		userAvatarURL = getUserAvatarURL(hex.EncodeToString(session.UserPubKey))
	}
	settingsCtx := SettingsContext{
		LoggedIn:      loggedIn,
		ThemeLabel:    themeLabel,
		UserAvatarURL: userAvatarURL,
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	// Use cached template for better performance
	var buf strings.Builder
	if isAppend {
		// HelmJS "Load More" request: render items + updated pagination
		if err := cachedProfileAppend.ExecuteTemplate(&buf, tmplProfileAppend, data); err != nil {
			return "", err
		}
	} else if isFragment {
		// HelmJS request: render just the content fragment
		if err := cachedProfileFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		// Full page request: render with base template
		if err := cachedProfileTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

// HTMLNotificationItem represents a notification for HTML rendering
type HTMLNotificationItem struct {
	Event             *Event
	Type              NotificationType
	TypeLabel         string // Human-readable label: "replied", "mentioned", "reacted", "reposted"
	TypeIcon          string // Emoji icon for the notification type
	TargetEventID     string
	TargetContentHTML template.HTML // Content of the target event (for reactions/reposts to show what was reacted to)
	AuthorProfile     *ProfileInfo
	AuthorNpub        string
	AuthorNpubShort   string
	ContentHTML       template.HTML
	TimeAgo           string
	QuotedEvent       *HTMLEventItem // For quote posts within notifications
	QuotedEventID     string         // Event ID from q tag
}

// HTMLNotificationsData is the data passed to the notifications template
type HTMLNotificationsData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	ThemeClass             string
	ThemeLabel             string
	UserDisplayName        string
	UserPubKey             string
	Items                  []HTMLNotificationItem
	GeneratedAt            time.Time
	Pagination             *HTMLPagination
	CurrentURL             string   // For base template compatibility
	CSRFToken              string   // For base template compatibility
	HasUnreadNotifications bool     // For base template compatibility
	ShowPostForm           bool     // For base template compatibility (always false for notifications)
	FeedMode               string   // For base template compatibility
	ActiveRelays           []string // For base template compatibility
	// Navigation (NATEOAS)
	FeedModes     []FeedMode
	KindFilters    []KindFilter
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	LoggedIn       bool // Always true for notifications, but needed for template consistency
}


func renderNotificationsHTML(notifications []Notification, profiles map[string]*ProfileInfo, targetEvents map[string]*Event, relays []string, resolvedRefs map[string]string, linkPreviews map[string]*LinkPreview, quotedEvents map[string]*Event, themeClass, themeLabel, userDisplayName, userPubKey string, pagination *HTMLPagination, isFragment bool, isAppend bool) (string, error) {

	items := make([]HTMLNotificationItem, len(notifications))
	for i, notif := range notifications {
		// Get author profile - for zaps, use the zap sender pubkey (not the LNURL provider)
		authorPubkey := notif.Event.PubKey
		if notif.Type == NotificationZap && notif.ZapSenderPubkey != "" {
			authorPubkey = notif.ZapSenderPubkey
		}
		profile := profiles[authorPubkey]
		npub, _ := encodeBech32Pubkey(authorPubkey)

		// Determine type label and icon
		var typeLabel, typeIcon string
		switch notif.Type {
		case NotificationMention:
			typeLabel = "mentioned you"
			typeIcon = "📢"
		case NotificationReply:
			typeLabel = "replied to you"
			typeIcon = "💬"
		case NotificationReaction:
			typeLabel = "reacted to your note"
			typeIcon = "❤️"
			// Use the actual reaction content as icon if it's an emoji
			if len(notif.Event.Content) > 0 && len(notif.Event.Content) < 10 {
				typeIcon = notif.Event.Content
				if typeIcon == "+" || typeIcon == "" {
					typeIcon = "❤️"
				}
			}
		case NotificationRepost:
			typeLabel = "reposted your note"
			typeIcon = "🔁"
		case NotificationZap:
			typeIcon = "⚡"
			if notif.ZapAmountSats > 0 {
				typeLabel = fmt.Sprintf("zapped you %d sats", notif.ZapAmountSats)
			} else {
				typeLabel = "zapped you"
			}
		}

		// Check for q tag (quote posts) in mentions and replies
		var qTagValue string
		if notif.Type == NotificationMention || notif.Type == NotificationReply {
			kindDef := GetKindDefinition(notif.Event.Kind)
			if kindDef.SupportsQuotePosts {
				for _, tag := range notif.Event.Tags {
					if len(tag) >= 2 && tag[0] == "q" {
						qTagValue = tag[1]
						break
					}
				}
			}
		}

		// Process content with full rendering (nostr refs, images, link previews)
		// Skip for reactions/reposts/zaps since they show target content instead
		var contentHTML template.HTML
		if notif.Type != NotificationReaction && notif.Type != NotificationRepost && notif.Type != NotificationZap {
			// Strip quoted reference if we have a quoted event
			content := notif.Event.Content
			if qTagValue != "" && quotedEvents != nil {
				if _, hasQuoted := quotedEvents[qTagValue]; hasQuoted {
					content = stripQuotedNostrRef(content, qTagValue)
				}
			}
			contentHTML = processContentToHTMLFull(content, relays, resolvedRefs, linkPreviews)
		}

		// For reactions/reposts/zaps, show fully processed target note content
		var targetContentHTML template.HTML
		if (notif.Type == NotificationReaction || notif.Type == NotificationRepost || notif.Type == NotificationZap) && notif.TargetEventID != "" {
			if targetEvent, ok := targetEvents[notif.TargetEventID]; ok {
				targetContentHTML = processContentToHTMLFull(targetEvent.Content, relays, resolvedRefs, linkPreviews)
			}
		}

		item := HTMLNotificationItem{
			Event:             &notif.Event,
			Type:              notif.Type,
			TypeLabel:         typeLabel,
			TypeIcon:          typeIcon,
			TargetEventID:     notif.TargetEventID,
			TargetContentHTML: targetContentHTML,
			AuthorProfile:     profile,
			AuthorNpub:        npub,
			AuthorNpubShort:   formatNpubShort(npub),
			ContentHTML:       contentHTML,
			TimeAgo:           formatTimeAgo(notif.Event.CreatedAt),
		}

		// Build quoted event if available
		if qTagValue != "" && quotedEvents != nil {
			item.QuotedEventID = qTagValue
			if qev, ok := quotedEvents[qTagValue]; ok {
				item.QuotedEvent = buildQuotedEventItem(qev, profiles[qev.PubKey], relays, resolvedRefs, linkPreviews)
			}
		}

		items[i] = item
	}

	data := HTMLNotificationsData{
		Title:           "Notifications",
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		UserDisplayName: userDisplayName,
		UserPubKey:      userPubKey,
		Items:           items,
		GeneratedAt:     time.Now(),
		Pagination:      pagination,
		LoggedIn:        true, // Notifications page requires login
	}

	// Build navigation (NATEOAS)
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:    true,
		ActiveFeed:  "me", // Notifications is part of "Me" context
		CurrentPage: "notifications",
	})
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   true,
		ActivePage: "notifications",
		HasUnread:  false, // Already on notifications page
	})
	// No kind filters submenu on notifications page
	data.KindFilters = nil
	settingsCtx := SettingsContext{
		LoggedIn:      true,
		ThemeLabel:    themeLabel,
		UserAvatarURL: getUserAvatarURL(userPubKey),
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if isAppend {
		// HelmJS "Load More" request: render items + updated pagination
		if err := cachedNotificationsAppend.ExecuteTemplate(&buf, tmplNotificationsAppend, data); err != nil {
			return "", err
		}
	} else if isFragment {
		// HelmJS request: render just the content fragment
		if err := cachedNotificationsFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
		// Add OOB update to hide the notification badge since user is viewing notifications
		buf.WriteString(`<span class="notification-badge notification-badge-hidden" id="notification-badge" h-oob="outer"></span>`)
	} else {
		// Full page request: render with base template
		if err := cachedNotificationsTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

// HTMLMutedUser represents a muted user for display
type HTMLMutedUser struct {
	Pubkey    string
	Npub      string
	NpubShort string
	Profile   *ProfileInfo
}

// HTMLMutesData is the data passed to the mutes template
type HTMLMutesData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	ThemeClass             string
	ThemeLabel             string
	UserDisplayName        string
	UserPubKey             string
	Items                  []HTMLMutedUser
	CurrentURL             string
	CSRFToken              string
	ShowPostForm           bool     // Always false for mutes page
	HasUnreadNotifications bool     // For base template compatibility
	ActiveRelays           []string // For base template compatibility
	// Navigation (NATEOAS)
	FeedModes     []FeedMode
	KindFilters    []KindFilter
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	LoggedIn       bool
}

func renderMutesHTML(mutedUsers []HTMLMutedUser, themeClass, themeLabel, userDisplayName, userPubKey, csrfToken string, isFragment bool) (string, error) {
	data := HTMLMutesData{
		Title:           I18n("nav.mutes"),
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		UserDisplayName: userDisplayName,
		UserPubKey:      userPubKey,
		Items:           mutedUsers,
		CurrentURL:      "/html/mutes",
		CSRFToken:       csrfToken,
		LoggedIn:        true,
	}

	// Build navigation items
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:   true,
		ActiveFeed: "me", // Mutes is part of "Me" context
	})
	// No kind filters submenu on mutes page
	data.KindFilters = nil
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   true,
		ActivePage: "mutes",
	})
	settingsCtx := SettingsContext{
		LoggedIn:      true,
		ThemeLabel:    themeLabel,
		UserAvatarURL: getUserAvatarURL(userPubKey),
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if isFragment {
		if err := cachedMutesFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		if err := cachedMutesTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

// HTMLWalletData is the data passed to the wallet template
type HTMLWalletData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	ThemeClass             string
	ThemeLabel             string
	UserDisplayName        string
	UserPubKey             string
	CurrentURL             string
	CSRFToken              string
	ReturnURL              string // URL to redirect back to after wallet setup
	HasWallet              bool   // Whether a wallet is connected
	WalletRelay            string // Connected wallet's relay URL
	WalletDomain           string // Domain extracted from relay URL (e.g., "nwc.primal.net")
	ShowPostForm           bool   // Always false for wallet page
	HasUnreadNotifications bool
	Success                string // Flash message for success
	Error                  string // Flash message for error
	// Navigation (NATEOAS)
	FeedModes      []FeedMode
	KindFilters    []KindFilter
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	LoggedIn       bool
}

func renderWalletHTML(data HTMLWalletData, isFragment bool) (string, error) {
	var buf strings.Builder
	if isFragment {
		if err := cachedWalletFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		if err := cachedWalletTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

func formatTimeAgo(timestamp int64) string {
	now := time.Now().Unix()
	diff := now - timestamp

	if diff < 60 {
		return "just now"
	} else if diff < 3600 {
		mins := diff / 60
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if diff < 86400 {
		hours := diff / 3600
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 604800 {
		days := diff / 86400
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else {
		return time.Unix(timestamp, 0).Format("Jan 2")
	}
}

// HTMLSearchData is the data passed to the search template
type HTMLSearchData struct {
	Title                  string
	PageDescription        string // SEO: overrides site default description
	PageImage              string // SEO: overrides site default OG image
	CanonicalURL           string // SEO: canonical URL for this page
	ThemeClass             string
	ThemeLabel             string
	Query                  string
	Items                  []HTMLEventItem
	Pagination             *HTMLPagination
	LoggedIn               bool
	UserPubKey             string
	UserDisplayName        string
	CSRFToken              string
	CurrentURL             string   // For base template compatibility
	HasUnreadNotifications bool
	ShowPostForm           bool     // For base template compatibility (always false for search)
	FeedMode               string   // For base template compatibility
	ActiveRelays           []string // For base template compatibility
	GeneratedAt            string
	// Navigation (NATEOAS)
	FeedModes      []FeedMode
	NavItems       []NavItem
	SettingsItems  []SettingsItem
	SettingsToggle SettingsToggle
	KindFilters    []KindFilter // For base template compatibility (always empty for search)
}


func renderSearchHTML(events []Event, profiles map[string]*ProfileInfo, query, themeClass, themeLabel string, loggedIn bool, userPubKey, userDisplayName, csrfToken string, hasUnreadNotifs bool, pagination *HTMLPagination, isFragment bool, isAppend bool, isLiveSearch bool, relays []string, quotedEvents map[string]*Event) (string, error) {
	// Convert events to HTMLEventItem
	items := make([]HTMLEventItem, 0, len(events))
	for _, evt := range events {
		npub, _ := encodeBech32Pubkey(evt.PubKey)
		npubShort := npub
		if len(npub) > 20 {
			npubShort = npub[:12] + "..." + npub[len(npub)-6:]
		}

		// Get q tag value for quote posts
		var qTagValue string
		kindDef := GetKindDefinition(evt.Kind)
		if kindDef.SupportsQuotePosts {
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValue = tag[1]
					break
				}
			}
		}

		// Process content - strip quoted reference if we have a quoted event
		contentHTML := processContentToHTML(evt.Content)
		if qTagValue != "" && quotedEvents != nil {
			// quotedEvents is keyed by original q tag value
			if _, hasQuoted := quotedEvents[qTagValue]; hasQuoted {
				strippedContent := stripQuotedNostrRef(evt.Content, qTagValue)
				contentHTML = processContentToHTML(strippedContent)
			}
		}

		item := HTMLEventItem{
			ID:             evt.ID,
			Kind:           evt.Kind,
			TemplateName:   kindDef.TemplateName,
			RenderTemplate: computeRenderTemplate(kindDef.TemplateName, evt.Tags),
			Pubkey:         evt.PubKey,
			Npub:           npub,
			NpubShort:      npubShort,
			CreatedAt:      evt.CreatedAt,
			ContentHTML:    contentHTML,
			AuthorProfile:  profiles[evt.PubKey],
			Tags:           evt.Tags,
		}

		// Build quoted event if available (quotedEvents keyed by original q tag value)
		if qTagValue != "" && quotedEvents != nil {
			item.QuotedEventID = qTagValue
			if qev, ok := quotedEvents[qTagValue]; ok {
				item.QuotedEvent = buildQuotedEventItem(qev, profiles[qev.PubKey], relays, nil, nil)
			}
		}

		items = append(items, item)
	}

	title := "Search"
	if query != "" {
		title = "Search: " + query
	}

	data := HTMLSearchData{
		Title:                  title,
		ThemeClass:             themeClass,
		ThemeLabel:             themeLabel,
		Query:                  query,
		Items:                  items,
		Pagination:             pagination,
		LoggedIn:               loggedIn,
		UserPubKey:             userPubKey,
		UserDisplayName:        userDisplayName,
		CSRFToken:              csrfToken,
		HasUnreadNotifications: hasUnreadNotifs,
		GeneratedAt:            time.Now().Format("15:04:05"),
	}

	// Build navigation (NATEOAS)
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:    loggedIn,
		ActiveFeed:  "", // No active feed on search page
		CurrentPage: "search",
	})
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   loggedIn,
		ActivePage: "search",
		HasUnread:  hasUnreadNotifs,
	})
	var userAvatarURL string
	if loggedIn {
		userAvatarURL = getUserAvatarURL(userPubKey)
	}
	settingsCtx := SettingsContext{
		LoggedIn:      loggedIn,
		ThemeLabel:    themeLabel,
		UserAvatarURL: userAvatarURL,
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if isAppend {
		// HelmJS "Load More" request: render items + updated pagination
		if err := cachedSearchAppend.ExecuteTemplate(&buf, tmplSearchAppend, data); err != nil {
			return "", err
		}
	} else if isLiveSearch {
		// HelmJS live search: render just the search-results content
		if err := cachedSearchTemplate.ExecuteTemplate(&buf, tmplSearchResults, data); err != nil {
			return "", err
		}
	} else if isFragment {
		// HelmJS navigation to search page: render full fragment with form
		if err := cachedSearchFragment.ExecuteTemplate(&buf, tmplFragment, data); err != nil {
			return "", err
		}
	} else {
		// Full page request: render with base template
		if err := cachedSearchTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}
