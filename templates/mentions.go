package templates

// Mention templates for @mention autocomplete

func GetMentionsDropdownTemplate() string {
	return mentionsDropdownContent
}

func GetMentionsSelectResponseTemplate() string {
	return mentionsSelectResponseContent
}

// MentionResult represents a profile match for the dropdown
type MentionResult struct {
	Pubkey      string
	DisplayName string
	NpubShort   string
	Picture     string
}

// MentionsDropdownData is the data for the mentions dropdown template
type MentionsDropdownData struct {
	Results []MentionResult
	Target  string // textarea target ID (e.g., "post", "reply", "quote")
	Query   string // the @query that was searched for
}

// MentionsSelectData is the data for the select response (OOB updates)
type MentionsSelectData struct {
	Target  string // textarea target ID
	Query   string // the @query to replace
	Name    string // display name to insert
	Pubkey  string // pubkey for the mapping
}

var mentionsDropdownContent = `{{define "mentions-dropdown"}}
{{if .Results}}
<div class="mentions-panel" role="listbox" aria-label="Mention suggestions">
  {{range .Results}}
  <a href="{{buildURL "/mentions/select" "target" $.Target "query" $.Query "name" .DisplayName "pubkey" .Pubkey}}"
     h-get h-target="#mentions-dropdown-{{$.Target}}" h-swap="inner"
     class="mention-item" role="option">
    <img src="{{if .Picture}}{{avatarURL .Picture}}{{else}}/static/avatar.jpg{{end}}" alt="" class="mention-avatar" loading="lazy">
    <span class="mention-name">{{.DisplayName}}</span>
    <span class="mention-npub">{{.NpubShort}}</span>
  </a>
  {{end}}
</div>
{{end}}
{{end}}`

var mentionsSelectResponseContent = `{{define "mentions-select-response"}}
<textarea id="{{.Target}}-content" h-oob="replace" data-find="@{{.Query}}" data-replace="@{{.Name}} "></textarea>
<input id="mentions-data-{{.Target}}" h-oob="merge" value="{&quot;{{.Name}}&quot;:&quot;{{.Pubkey}}&quot;}">
{{end}}`
