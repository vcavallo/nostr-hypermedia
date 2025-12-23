package templates

// Report template - report form for flagging content.

func GetReportTemplate() string {
	return reportContent
}

var reportContent = `{{define "content"}}
{{template "flash-messages" .}}

<div class="report-page">
  <h1>{{i18n "report.title"}}</h1>

  <div class="reported-note">
    <div class="quoted-author">
      <img src="{{if and .ReportedEvent.AuthorProfile .ReportedEvent.AuthorProfile.Picture}}{{avatarURL .ReportedEvent.AuthorProfile.Picture}}{{else}}/static/avatar.jpg{{end}}" alt="{{displayName .ReportedEvent.AuthorProfile "User"}}'s avatar" loading="lazy">
      <span class="quoted-author-name">{{displayName .ReportedEvent.AuthorProfile .ReportedEvent.NpubShort}}</span>
    </div>
    {{if eq .ReportedEvent.TemplateName "longform"}}
    <div class="quoted-article-title">{{if .ReportedEvent.Title}}{{.ReportedEvent.Title}}{{else}}{{i18n "msg.untitled"}} Article{{end}}</div>
    {{if .ReportedEvent.Summary}}<div class="quoted-article-summary">{{.ReportedEvent.Summary}}</div>{{end}}
    {{else}}
    <div class="note-content">{{.ReportedEvent.ContentHTML}}</div>
    {{end}}
  </div>

  {{if .LoggedIn}}
  <form method="POST" action="/report" class="report-form">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <input type="hidden" name="event_id" value="{{.ReportedEvent.ID}}">
    <input type="hidden" name="event_pubkey" value="{{.ReportedEvent.Pubkey}}">
    <input type="hidden" name="return_url" value="{{.ReturnURL}}">

    <fieldset>
      <legend>{{i18n "report.reason"}}</legend>
      <label class="radio-label"><input type="radio" name="category" value="spam" required> {{i18n "report.spam"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="impersonation"> {{i18n "report.impersonation"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="illegal"> {{i18n "report.illegal"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="nudity"> {{i18n "report.nudity"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="malware"> {{i18n "report.malware"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="profanity"> {{i18n "report.profanity"}}</label>
      <label class="radio-label"><input type="radio" name="category" value="other"> {{i18n "report.other"}}</label>
    </fieldset>

    <div class="form-group">
      <label for="report-details">{{i18n "report.details"}}</label>
      <textarea id="report-details" name="content" placeholder="{{i18n "report.details_placeholder"}}"></textarea>
    </div>

    <div class="form-group">
      <label class="checkbox-label"><input type="checkbox" name="mute_user" value="1"> {{i18n "report.also_mute"}}</label>
    </div>

    <div class="form-actions">
      <button type="submit" class="btn-primary">{{i18n "report.submit"}}</button>
      <a href="{{.ReturnURL}}" class="btn-secondary">{{i18n "btn.cancel"}}</a>
    </div>
  </form>
  {{else}}
  <div class="login-prompt-box">
    <a href="/login" class="text-link" rel="nofollow">{{i18n "btn.login"}}</a> to report this content
  </div>
  {{end}}
</div>
{{end}}`
