package kinds

// File renders kind 1063 file metadata events (NIP-94)
var File = `{{define "render-file"}}
<article class="note file-note" id="note-{{.ID}}" aria-label="File by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  {{template "content-warning-start" .}}
  <div class="file-card">
    {{if .FileIsImage}}
    <a href="{{.FileURL}}" target="_blank" rel="noopener" class="file-preview-link">
      <img src="{{.FileURL}}" alt="{{if .FileAlt}}{{.FileAlt}}{{else}}{{.FileTitle}}{{end}}" class="file-preview-image" loading="lazy">
    </a>
    {{else if .FileIsVideo}}
    <div class="file-video-container">
      <video class="file-video" controls preload="metadata"{{if .FileThumbnail}} poster="{{.FileThumbnail}}"{{end}}>
        <source src="{{.FileURL}}"{{if .FileMimeType}} type="{{.FileMimeType}}"{{end}}>
      </video>
    </div>
    {{else if .FileIsAudio}}
    <div class="file-audio-container">
      <audio class="file-audio" controls preload="metadata">
        <source src="{{.FileURL}}"{{if .FileMimeType}} type="{{.FileMimeType}}"{{end}}>
      </audio>
    </div>
    {{else}}
    <div class="file-generic">
      <span class="file-icon">📄</span>
    </div>
    {{end}}
    <div class="file-info">
      {{if .FileTitle}}<h3 class="file-title">{{.FileTitle}}</h3>{{end}}
      {{if .Content}}<p class="file-description">{{.ContentHTML}}</p>{{end}}
      <div class="file-meta">
        {{if .FileMimeType}}<span class="file-type">{{.FileMimeType}}</span>{{end}}
        {{if .FileSize}}<span class="file-size">{{.FileSize}}</span>{{end}}
        {{if .FileDimensions}}<span class="file-dimensions">{{.FileDimensions}}</span>{{end}}
      </div>
      <a href="{{.FileURL}}" target="_blank" rel="noopener" class="file-download-link">{{i18n "action.download"}}</a>
    </div>
  </div>
  {{template "content-warning-end" .}}
  {{template "note-footer" .}}
</article>
{{end}}`
