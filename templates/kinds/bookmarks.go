package kinds

// Template renders kind 10003 (bookmark list).
var Bookmarks = `{{define "render-bookmarks"}}
<article class="note bookmarks" id="note-{{.ID}}" aria-label="Bookmarks by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="bookmarks-header">
    <span class="bookmarks-icon">🔖</span>
    <span class="bookmarks-title">{{i18n "label.bookmarks"}}</span>
    <span class="bookmarks-count">{{.BookmarkCount}} items</span>
  </div>
  {{if .BookmarkEventIDs}}
  <div class="bookmarks-section">
    <div class="bookmarks-section-title">{{i18n "label.events"}}</div>
    <div class="bookmarks-list">
      {{range .BookmarkEventIDs}}
      <a href="/event/{{.}}" class="bookmark-item">
        <span class="bookmark-item-icon">📝</span>
        <span class="bookmark-item-text">{{slice . 0 12}}...</span>
      </a>
      {{end}}
    </div>
  </div>
  {{end}}
  {{if .BookmarkArticleRefs}}
  <div class="bookmarks-section">
    <div class="bookmarks-section-title">{{i18n "label.articles"}}</div>
    <div class="bookmarks-list">
      {{range .BookmarkArticleRefs}}
      <div class="bookmark-item">
        <span class="bookmark-item-icon">📄</span>
        <span class="bookmark-item-text">{{.}}</span>
      </div>
      {{end}}
    </div>
  </div>
  {{end}}
  {{if .BookmarkHashtags}}
  <div class="bookmarks-section">
    <div class="bookmarks-section-title">{{i18n "label.hashtags"}}</div>
    <div class="bookmarks-list">
      {{range .BookmarkHashtags}}
      <span class="bookmark-item bookmark-hashtag">
        <span class="bookmark-item-icon">#</span>
        <span class="bookmark-item-text">{{.}}</span>
      </span>
      {{end}}
    </div>
  </div>
  {{end}}
  {{if .BookmarkURLs}}
  <div class="bookmarks-section">
    <div class="bookmarks-section-title">{{i18n "label.links"}}</div>
    <div class="bookmarks-list">
      {{range .BookmarkURLs}}
      <a href="{{.}}" class="bookmark-item" target="_blank" rel="external noopener">
        <span class="bookmark-item-icon">🔗</span>
        <span class="bookmark-item-text">{{.}}</span>
      </a>
      {{end}}
    </div>
  </div>
  {{end}}
  {{template "note-footer" .}}
</article>
{{end}}`
