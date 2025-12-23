package kinds

// Dispatcher routes to the appropriate render template based on .RenderTemplate.
// This is the universal entry point - routing is purely mechanical (no kind logic here).
// The RenderTemplate value is computed server-side from event metadata (render-hint tags or kind mapping).
var Dispatcher = `{{define "event-dispatcher"}}
{{if eq .RenderTemplate "render-note"}}{{template "render-note" .}}
{{else if eq .RenderTemplate "render-repost"}}{{template "render-repost" .}}
{{else if eq .RenderTemplate "render-picture"}}{{template "render-picture" .}}
{{else if eq .RenderTemplate "render-shortvideo"}}{{template "render-shortvideo" .}}
{{else if eq .RenderTemplate "render-longform"}}{{template "render-longform" .}}
{{else if eq .RenderTemplate "render-zap"}}{{template "render-zap" .}}
{{else if eq .RenderTemplate "render-livestream"}}{{template "render-livestream" .}}
{{else if eq .RenderTemplate "render-bookmarks"}}{{template "render-bookmarks" .}}
{{else if eq .RenderTemplate "render-highlight"}}{{template "render-highlight" .}}
{{else if eq .RenderTemplate "render-comment"}}{{template "render-comment" .}}
{{else if eq .RenderTemplate "render-classified"}}{{template "render-classified" .}}
{{else if eq .RenderTemplate "render-video"}}{{template "render-video" .}}
{{else if eq .RenderTemplate "render-calendar"}}{{template "render-calendar" .}}
{{else if eq .RenderTemplate "render-file"}}{{template "render-file" .}}
{{else if eq .RenderTemplate "render-stall"}}{{template "render-stall" .}}
{{else if eq .RenderTemplate "render-product"}}{{template "render-product" .}}
{{else if eq .RenderTemplate "render-status"}}{{template "render-status" .}}
{{else if eq .RenderTemplate "render-community"}}{{template "render-community" .}}
{{else if eq .RenderTemplate "render-badge-definition"}}{{template "render-badge-definition" .}}
{{else if eq .RenderTemplate "render-badge-award"}}{{template "render-badge-award" .}}
{{else if eq .RenderTemplate "render-report"}}{{template "render-report" .}}
{{else if eq .RenderTemplate "render-live-chat"}}{{template "render-live-chat" .}}
{{else if eq .RenderTemplate "render-rsvp"}}{{template "render-rsvp" .}}
{{else if eq .RenderTemplate "render-label"}}{{template "render-label" .}}
{{else if eq .RenderTemplate "render-repository"}}{{template "render-repository" .}}
{{else if eq .RenderTemplate "render-handler"}}{{template "render-handler" .}}
{{else if eq .RenderTemplate "render-recommendation"}}{{template "render-recommendation" .}}
{{else if eq .RenderTemplate "render-audio"}}{{template "render-audio" .}}
{{else}}{{template "render-default" .}}
{{end}}
{{end}}`

// GetAllTemplates returns all kind templates concatenated for parsing.
func GetAllTemplates() string {
	return GetPartials() +
		Note +
		Repost +
		Picture +
		Shortvideo +
		Video +
		Comment +
		Zap +
		Highlight +
		Bookmarks +
		Longform +
		Livestream +
		Classified +
		Calendar +
		File +
		Stall +
		Product +
		Status +
		Community +
		BadgeDefinition +
		BadgeAward +
		Report +
		LiveChatTemplate +
		RSVPTemplate +
		LabelTemplate +
		RepositoryTemplate +
		Handler +
		Recommendation +
		Audio +
		Default +
		Dispatcher
}
