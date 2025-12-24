package kinds

// Stall renders kind 30017 marketplace stall events (NIP-15)
var Stall = `{{define "render-stall"}}
<article class="note stall-note" id="note-{{.ID}}" aria-label="Stall by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="stall-card">
    <div class="stall-header">
      <span class="stall-icon">🏪</span>
      <h3 class="stall-name">{{.StallName}}</h3>
    </div>
    {{if .StallDescription}}<p class="stall-description">{{.StallDescription}}</p>{{end}}
    <div class="stall-meta">
      {{if .StallCurrency}}<span class="stall-currency">{{i18n "label.currency"}}: {{.StallCurrency}}</span>{{end}}
      {{if .StallShippingZones}}
      <details class="stall-shipping">
        <summary>{{i18n "label.shipping"}} ({{len .StallShippingZones}})</summary>
        <ul class="stall-shipping-list">
          {{range .StallShippingZones}}
          <li>{{.Name}}{{if .Regions}} ({{.Regions}}){{end}}: {{.Cost}}</li>
          {{end}}
        </ul>
      </details>
      {{end}}
    </div>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`

// Product renders kind 30018 marketplace product events (NIP-15)
var Product = `{{define "render-product"}}
<article class="note product-note" id="note-{{.ID}}" aria-label="Product by {{displayName .AuthorProfile .NpubShort}}">
  {{template "author-header" .}}
  <div class="product-card">
    {{if .ProductImages}}
    <div class="product-images">
      {{range $i, $img := .ProductImages}}
      {{if eq $i 0}}
      <img src="{{$img}}" alt="{{$.ProductName}}" class="product-image-main" loading="lazy">
      {{end}}
      {{end}}
      {{if gt (len .ProductImages) 1}}
      <div class="product-image-thumbs">
        {{range $i, $img := .ProductImages}}
        {{if gt $i 0}}
        <img src="{{$img}}" alt="" class="product-image-thumb" loading="lazy">
        {{end}}
        {{end}}
      </div>
      {{end}}
    </div>
    {{end}}
    <div class="product-info">
      <h3 class="product-name">{{.ProductName}}</h3>
      <div class="product-price">
        <span class="product-price-amount">{{.ProductPrice}}</span>
        <span class="product-price-currency">{{.ProductCurrency}}</span>
      </div>
      {{if .ProductDescription}}<p class="product-description">{{.ProductDescription}}</p>{{end}}
      {{if .ProductQuantity}}<p class="product-quantity">{{i18n "label.in_stock"}}: {{.ProductQuantity}}</p>{{end}}
      {{if .ProductSpecs}}
      <dl class="product-specs">
        {{range .ProductSpecs}}
        <dt>{{.Key}}</dt>
        <dd>{{.Value}}</dd>
        {{end}}
      </dl>
      {{end}}
      {{if .ProductCategories}}
      <div class="product-categories">
        {{range .ProductCategories}}
        <a href="{{buildURL "/search" "q" (print "#" .)}}" class="product-category">#{{.}}</a>
        {{end}}
      </div>
      {{end}}
    </div>
  </div>
  {{template "note-footer" .}}
</article>
{{end}}`
