package templates

// Wallet template - NWC wallet connection for zaps.

func GetWalletTemplate() string {
	return walletContent
}

// GetWalletInfoTemplate returns the wallet info fragment template.
// Used by /wallet/info endpoint for lazy-loading wallet balance and transactions.
func GetWalletInfoTemplate() string {
	return walletInfoContent
}

var walletContent = `{{define "content"}}
{{template "flash-messages" .}}

<div class="wallet-page">
{{if .HasWallet}}
  <div class="wallet-card wallet-card-connected">
    <div class="wallet-connection-status">
      <span class="wallet-connection-indicator"></span>
      <span class="wallet-connection-text">{{i18n "wallet.connected_via"}} {{.WalletDomain}}</span>
    </div>

    <div id="wallet-info" class="wallet-info-container">
      <a href="/wallet/info"
         h-get
         h-target="#wallet-info"
         h-swap="inner"
         h-trigger="intersect once"
         h-indicator="#wallet-info-loading"
         aria-live="polite">
        <span id="wallet-info-loading" class="h-indicator wallet-loading">
          <span class="h-spinner"></span>
          <span>{{i18n "wallet.loading"}}</span>
        </span>
      </a>
    </div>

    <form method="POST" action="/wallet/disconnect" class="wallet-disconnect-form">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <input type="hidden" name="return_url" value="{{.ReturnURL}}">
      <button type="submit" class="btn-secondary btn-block" h-confirm="{{i18n "confirm.disconnect_wallet"}}">
        {{i18n "wallet.disconnect"}}
      </button>
    </form>
  </div>
{{else}}
  <div class="wallet-card">
    <div class="wallet-status wallet-status-disconnected">
      <span class="wallet-status-icon">&#x26A1;</span>
      <span class="wallet-status-label">{{i18n "wallet.setup_title"}}</span>
    </div>
    <p class="wallet-intro-text">{{i18n "wallet.setup_desc"}}</p>

    <form method="POST" action="/wallet/connect" class="wallet-connect-form">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <input type="hidden" name="return_url" value="{{.ReturnURL}}">
      <div class="wallet-input-group">
        <label for="nwc_uri" class="wallet-input-label">{{i18n "wallet.nwc_uri"}}</label>
        <input type="text" id="nwc_uri" name="nwc_uri"
               class="wallet-input"
               placeholder="nostr+walletconnect://..."
               required autocomplete="off"
               aria-describedby="nwc-help">
        <p class="wallet-input-help" id="nwc-help">{{i18n "wallet.nwc_help"}}</p>
      </div>
      <button type="submit" class="submit-btn submit-btn-block">{{i18n "wallet.connect"}}</button>
    </form>
  </div>

  <section class="wallet-info-section">
    <h3 class="wallet-info-heading">{{i18n "wallet.how_it_works"}}</h3>
    <p>{{i18n "wallet.how_desc"}}</p>
  </section>

  <section class="wallet-info-section">
    <h3 class="wallet-info-heading">{{i18n "wallet.supported_wallets"}}</h3>
    <ul class="wallet-list">
      <li><a href="https://albyhub.com/" target="_blank" rel="external noopener">Alby Hub</a> <span class="wallet-list-desc">{{i18n "wallet.alby_desc"}}</span></li>
      <li><a href="https://primal.net/" target="_blank" rel="external noopener">Primal</a> <span class="wallet-list-desc">{{i18n "wallet.primal_desc"}}</span></li>
      <li>{{i18n "wallet.any_nwc"}}</li>
    </ul>
  </section>

  <section class="wallet-info-section">
    <h3 class="wallet-info-heading">{{i18n "wallet.zap_amount"}}</h3>
    <p>{{i18n "wallet.zap_amount_desc"}}</p>
  </section>
{{end}}
</div>
{{end}}`

// walletInfoContent is the wallet info fragment shown after lazy-load.
// Displays balance and recent transactions fetched via NWC.
var walletInfoContent = `{{define "wallet-info"}}{{if .Error}}<div class="wallet-info-error">{{.Error}}</div>{{else}}
<div class="wallet-balance">
  <span class="wallet-balance-icon">⚡</span>
  <span class="wallet-balance-amount">{{.Balance}}</span>
  <span class="wallet-balance-unit">sats</span>
</div>
<div class="wallet-transactions">
  <h4 class="wallet-transactions-title">Recent</h4>
  {{if .Transactions}}
  <div class="wallet-transaction-list">
    {{range .Transactions}}
    <div class="wallet-transaction wallet-transaction-{{.Type}}{{if .IsZap}} wallet-transaction-zap{{end}}">
      <div class="wallet-transaction-left">
        <span class="wallet-transaction-icon">{{.TypeIcon}}</span>
        <span class="wallet-transaction-desc">{{if .IsZap}}{{.ZapDisplayName}}{{else}}{{.Description}}{{end}}</span>
      </div>
      <div class="wallet-transaction-right">
        <span class="wallet-transaction-amount">{{if eq .Type "outgoing"}}-{{end}}{{.Amount}} sats</span>
        <span class="wallet-transaction-time">{{.TimeAgo}}</span>
      </div>
    </div>
    {{end}}
  </div>
  {{else}}
  <p class="wallet-transactions-empty">No recent transactions for this wallet</p>
  {{end}}
</div>
{{end}}{{end}}`
