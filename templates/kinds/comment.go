package kinds

// Template renders kind 1111 (comment - NIP-22).
var Comment = `{{define "render-comment"}}
<article class="note comment" id="note-{{.ID}}"{{if .IsScrollTarget}} data-scroll-target{{end}} aria-label="Comment by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="note-content">{{.ContentHTML}}</div>
  {{if or .CommentRootID .CommentRootURL}}
  <div class="comment-context">
    <span class="comment-context-icon">💬</span>
    <span class="comment-context-text">
      {{if .CommentIsNested}}{{i18n "label.reply_on"}}{{else}}{{i18n "label.comment_on"}}{{end}}
      {{if .CommentRootURL}}
      <a href="{{.CommentRootURL}}" class="comment-context-link" target="_blank" rel="external noopener">{{.CommentRootURL}}</a>
      {{else if .CommentRootID}}
      <a href="/thread/{{noteLink .CommentRootID}}" h-get h-target="#page-content" h-swap="inner" h-push-url h-scroll="top" h-prefetch class="comment-context-link" rel="related">{{if .CommentRootLabel}}{{.CommentRootLabel}}{{else}}event{{end}}</a>
      {{end}}
    </span>
  </div>
  {{end}}
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
