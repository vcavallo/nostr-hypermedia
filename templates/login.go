package templates

// Login template - QR code scan, bunker URL, or reconnect options.

var loginContent = `{{define "content"}}
{{template "flash-messages" .}}

{{if .NostrConnectURL}}
<div class="login-form login-section">
  <h2>Option 1: Scan with Signer App</h2>
  {{if .QRCodeDataURL}}
  <div class="qr-container">
    <img src="{{.QRCodeDataURL}}" alt="Scan this QR code with your signer app" class="qr-code" loading="lazy">
  </div>
  <p class="qr-help">
    Scan this QR code with your signer app (Amber, etc.)
  </p>
  {{end}}
  <details class="url-details">
    <summary>Or copy URL manually</summary>
    <div class="url-box">
      {{.NostrConnectURL}}
    </div>
  </details>
  <a href="/html/check-connection?secret={{.Secret}}" class="submit-btn submit-btn-block">
    Check Connection
  </a>
  <p class="form-help">
    After approving in your signer app, click the button above to complete login.
  </p>
</div>

<div class="divider">
  &mdash; or &mdash;
</div>
{{end}}

<form class="login-form login-section" method="POST" action="/html/login">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <h2>Option 2: Paste Bunker URL</h2>
  <div class="form-group">
    <label for="bunker_url">Bunker URL</label>
    <input type="text" id="bunker_url" name="bunker_url"
           placeholder="bunker://pubkey?relay=wss://...&secret=..."
           required autocomplete="off">
    <p class="form-help">
      Paste your bunker:// URL from your Nostr signer app (nsec.app, Amber, etc.)
    </p>
  </div>
  <button type="submit" class="submit-btn">{{i18n "btn.login"}}</button>
</form>

<div class="divider">
  &mdash; or &mdash;
</div>

<form class="login-form login-section" method="POST" action="/html/reconnect">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
  <h2>Option 3: Reconnect to Existing Bunker</h2>
  <p class="server-info">
    <strong>This server's pubkey:</strong><br>
    <code>{{.ServerPubKey}}</code><br>
    <span class="server-info-note">Look for this in your signer's approved connections list.</span>
  </p>
  <div class="form-group">
    <label for="signer_pubkey">Signer Public Key</label>
    <input type="text" id="signer_pubkey" name="signer_pubkey"
           placeholder="npub1... or hex pubkey"
           required autocomplete="off">
    <p class="form-help">
      Enter the pubkey your signer uses for NIP-46 (found in Amber under the approved connection details).
    </p>
  </div>
  <button type="submit" class="submit-btn">Reconnect</button>
</form>

<section class="info-section" aria-labelledby="how-it-works-heading">
  <h2 id="how-it-works-heading">How it works</h2>
  <p>
    This login uses <strong>NIP-46 (Nostr Connect)</strong> - your private key never leaves your signer app.
    The server only sees your public key and cannot sign events without your approval.
  </p>
  <h2>Supported signers</h2>
  <ul>
    <li><a href="https://nsec.app" target="_blank" rel="external noopener">nsec.app</a> - Web-based remote signer</li>
    <li><a href="https://github.com/greenart7c3/Amber" target="_blank" rel="external noopener">Amber</a> - Android signer</li>
    <li>Any NIP-46 compatible bunker</li>
  </ul>
</section>
{{end}}`
