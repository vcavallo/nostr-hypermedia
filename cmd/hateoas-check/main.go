// Static HATEOAS Compliance Checker
// Analyzes Go template files for hypermedia best practices without requiring a running server
package main

import (
	"flag"
	"fmt"
	htmlpkg "html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Check categories
const (
	CategoryNavigation    = "Navigation"
	CategoryForms         = "Forms & Actions"
	CategoryLinks         = "Links"
	CategorySelfDescribe  = "Self-Describing"
	CategoryStateTransfer = "State Transfer"
	CategoryAccessibility = "Accessibility"
	CategoryErrorHandling = "Error Responses"
	CategoryPagination    = "Pagination"
)

// Severity levels
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// CheckResult represents a single compliance check result
type CheckResult struct {
	Category string
	Rule     string
	Passed   bool
	Message  string
	File     string
	Line     int
	Element  string
	Severity string
}

// TemplateAnalysis contains analysis results for a single template
type TemplateAnalysis struct {
	File            string
	TemplateName    string
	Checks          []CheckResult
	Links           []LinkInfo
	Forms           []FormInfo
	Scripts         []string
	HasNav          bool
	HasMain         bool
	HasTitle        bool
	IsContentBlock  bool // True if this is a {{define "content"}} block that gets embedded in base
	IsPartialBlock  bool // True if this is a partial template (e.g., header, footer, kind-*)
}

// LinkInfo captures link details
type LinkInfo struct {
	Href         string
	Text         string
	HasAriaLabel bool
	IsEmpty      bool
	HasRel       bool
	Rel          string
	IsExternal   bool
	Line         int
}

// FormInfo captures form details
type FormInfo struct {
	Action     string
	Method     string
	HasCSRF    bool
	HasConfirm bool // h-confirm attribute for destructive actions
	Inputs     []InputInfo
	Line       int
}

// InputInfo captures form input details
type InputInfo struct {
	Type             string
	Name             string
	ID               string
	HasLabel         bool
	HasWrappingLabel bool // Input is wrapped inside a <label> element
	Placeholder      string
	Line             int
}

// Report contains the full compliance report
type Report struct {
	GeneratedAt time.Time
	ProjectPath string
	Templates   []TemplateAnalysis
	Summary     map[string]CategorySummary
	TotalScore  float64
}

// CategorySummary summarizes results per category
type CategorySummary struct {
	Passed int
	Failed int
	Total  int
	Score  float64
}

var (
	projectPath string
	verbose     bool
	outputFile  string

	// Known routes extracted from main.go
	knownRoutes = []string{
		"/timeline",
		"/thread/",
		"/profile/edit",
		"/profile/",
		"/login",
		"/logout",
		"/post",
		"/reply",
		"/react",
		"/bookmark",
		"/mute",
		"/mutes",
		"/repost",
		"/follow",
		"/quote/",
		"/check-connection",
		"/reconnect",
		"/theme",
		"/notifications",
		"/stream/notifications",
		"/search",
		"/timeline/check-new",
		"/event/",
		"/static/",
		"/",
	}
)

func main() {
	flag.StringVar(&projectPath, "path", ".", "Path to project root")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.StringVar(&outputFile, "output", "hateoas-report.html", "Output file")
	flag.Parse()

	fmt.Printf("HATEOAS Compliance Checker\n")
	fmt.Printf("==================================\n")
	fmt.Printf("Project path: %s\n", projectPath)

	report := &Report{
		GeneratedAt: time.Now(),
		ProjectPath: projectPath,
		Templates:   []TemplateAnalysis{},
		Summary:     make(map[string]CategorySummary),
	}

	// Find all template files (including subdirectories like templates/kinds/)
	templatesDir := filepath.Join(projectPath, "templates")
	templateFiles, err := filepath.Glob(filepath.Join(templatesDir, "*.go"))
	if err != nil {
		fmt.Printf("Error finding templates: %v\n", err)
		os.Exit(1)
	}
	// Also include templates/kinds/*.go
	kindTemplates, _ := filepath.Glob(filepath.Join(templatesDir, "kinds", "*.go"))
	templateFiles = append(templateFiles, kindTemplates...)

	if len(templateFiles) == 0 {
		fmt.Printf("No template files found in %s\n", templatesDir)
		os.Exit(1)
	}

	fmt.Printf("Found %d template files\n\n", len(templateFiles))

	// Analyze each template file
	for _, file := range templateFiles {
		if verbose {
			fmt.Printf("Analyzing: %s\n", file)
		}

		analyses, err := analyzeTemplateFile(file)
		if err != nil {
			if verbose {
				fmt.Printf("  Error: %v\n", err)
			}
			continue
		}

		report.Templates = append(report.Templates, analyses...)
	}

	// Calculate summary
	calculateSummary(report)

	// Generate HTML report
	if err := generateHTMLReport(report, outputFile); err != nil {
		fmt.Printf("Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Analyzed %d templates\n", len(report.Templates))
	fmt.Printf("Overall Score: %.1f%%\n", report.TotalScore)
	fmt.Printf("Report saved to: %s\n", outputFile)

	// Print summary
	fmt.Printf("\nCategory Scores:\n")
	categories := []string{CategoryNavigation, CategoryForms, CategoryLinks, CategorySelfDescribe, CategoryStateTransfer, CategoryAccessibility, CategoryErrorHandling, CategoryPagination}
	for _, cat := range categories {
		if summary, ok := report.Summary[cat]; ok {
			fmt.Printf("  %-20s %3d/%3d (%.0f%%)\n", cat+":", summary.Passed, summary.Total, summary.Score)
		}
	}
}

func analyzeTemplateFile(filePath string) ([]TemplateAnalysis, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	fileContent := string(content)
	var analyses []TemplateAnalysis

	// Extract template strings from Go files
	templatePattern := regexp.MustCompile("(?s)`([^`]+)`")
	matches := templatePattern.FindAllStringSubmatchIndex(fileContent, -1)

	// For base.go, we need to understand that baseTemplate is split by concatenation
	// Look at the entire file to understand the full template structure
	isBaseFile := strings.Contains(filePath, "base.go")
	var fullBaseContent string
	if isBaseFile {
		// Combine all backtick strings to understand the full template
		for _, match := range matches {
			if len(match) >= 4 {
				fullBaseContent += fileContent[match[2]:match[3]]
			}
		}
	}

	for _, match := range matches {
		if len(match) >= 4 {
			templateStr := fileContent[match[2]:match[3]]

			// Skip if it doesn't look like HTML
			if !strings.Contains(templateStr, "<") {
				continue
			}

			// Find template name from {{define "name"}}
			templateName := extractTemplateName(templateStr)
			if templateName == "" {
				// Skip unnamed fragments in base.go - they're part of the combined base template
				// and would be analyzed as part of the "base" template
				if isBaseFile {
					continue
				}
				templateName = filepath.Base(filePath)
			}

			// For the "base" template in base.go, use the full file content for analysis
			// This handles templates split by Go string concatenation (e.g., ` + baseCSS + `)
			analysisContent := templateStr
			if isBaseFile && templateName == "base" {
				analysisContent = fullBaseContent
			}

			analysis := analyzeTemplate(analysisContent, filePath, templateName)
			if len(analysis.Checks) > 0 || len(analysis.Links) > 0 || len(analysis.Forms) > 0 {
				analyses = append(analyses, analysis)
			}
		}
	}

	return analyses, nil
}

func extractTemplateName(content string) string {
	pattern := regexp.MustCompile(`\{\{define\s+"([^"]+)"\}\}`)
	matches := pattern.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func analyzeTemplate(content string, filePath string, templateName string) TemplateAnalysis {
	// Determine if this is a content block (embedded in base template) or a partial
	isContentBlock := templateName == "content" || strings.Contains(content, `{{define "content"}}`)
	isPartialBlock := strings.HasPrefix(templateName, "kind-") ||
		strings.HasPrefix(templateName, "render-") ||
		strings.HasPrefix(templateName, "author-") ||
		strings.HasPrefix(templateName, "note-") ||
		strings.HasPrefix(templateName, "quoted-") ||
		strings.HasPrefix(templateName, "flash-") ||
		strings.HasPrefix(templateName, "new-notes-") ||
		strings.HasPrefix(templateName, "gif-") ||
		strings.HasPrefix(templateName, "mentions-") ||
		strings.HasPrefix(templateName, "oob-") ||
		strings.HasSuffix(templateName, "-append") ||
		strings.HasSuffix(templateName, "-response") ||
		strings.HasSuffix(templateName, "-fragment") ||
		strings.HasSuffix(templateName, "-preview") ||
		templateName == "header" ||
		templateName == "footer" ||
		templateName == "pagination" ||
		templateName == "follow-button" ||
		templateName == "mute-button" ||
		templateName == "nav-oob" ||
		templateName == "event-dispatcher" ||
		templateName == "fragment" ||
		templateName == "wallet-info" ||
		templateName == "wavlake-player" ||
		templateName == "link-preview"

	analysis := TemplateAnalysis{
		File:           filePath,
		TemplateName:   templateName,
		Checks:         []CheckResult{},
		Links:          []LinkInfo{},
		Forms:          []FormInfo{},
		Scripts:        []string{},
		IsContentBlock: isContentBlock,
		IsPartialBlock: isPartialBlock,
	}

	// Strip Go template syntax for HTML parsing
	cleanHTML := stripGoTemplates(content)

	// Parse HTML
	doc, err := html.Parse(strings.NewReader(cleanHTML))
	if err != nil {
		nodes, err := html.ParseFragment(strings.NewReader(cleanHTML), nil)
		if err != nil {
			return analysis
		}
		for _, node := range nodes {
			extractElements(node, &analysis, content)
		}
	} else {
		extractElements(doc, &analysis, content)
	}

	// Run HATEOAS checks
	runChecks(&analysis, content)

	return analysis
}

func stripGoTemplates(content string) string {
	patterns := []struct {
		pattern *regexp.Regexp
		replace string
	}{
		{regexp.MustCompile(`\{\{if[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{else[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{end\}\}`), ""},
		{regexp.MustCompile(`\{\{range[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{template[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{block[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{[^}]+\}\}`), "placeholder"},
	}

	result := content
	for _, p := range patterns {
		result = p.pattern.ReplaceAllString(result, p.replace)
	}

	return result
}

func extractElements(n *html.Node, analysis *TemplateAnalysis, originalContent string) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "nav":
			analysis.HasNav = true

		case "main":
			analysis.HasMain = true

		case "title":
			analysis.HasTitle = true

		case "a":
			href := getAttr(n, "href")
			text := extractText(n)
			rel := getAttr(n, "rel")
			isExternal := strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") ||
				hasAttr(n, "target") && getAttr(n, "target") == "_blank"
			link := LinkInfo{
				Href:         href,
				Text:         text,
				HasAriaLabel: hasAttr(n, "aria-label"),
				IsEmpty:      strings.TrimSpace(text) == "" && !hasAttr(n, "aria-label") && !hasChildElement(n, "img"),
				HasRel:       hasAttr(n, "rel"),
				Rel:          rel,
				IsExternal:   isExternal,
				Line:         findLineNumber(originalContent, n),
			}
			analysis.Links = append(analysis.Links, link)

		case "form":
			form := FormInfo{
				Action: getAttr(n, "action"),
				Method: strings.ToUpper(getAttr(n, "method")),
				Line:   findLineNumber(originalContent, n),
				Inputs: []InputInfo{},
			}
			if form.Method == "" {
				form.Method = "GET"
			}
			form.HasCSRF = hasCSRFToken(n)
			form.HasConfirm = hasConfirmAttr(n)
			form.Inputs = extractInputs(n, originalContent)
			analysis.Forms = append(analysis.Forms, form)

		case "script":
			src := getAttr(n, "src")
			if src != "" {
				analysis.Scripts = append(analysis.Scripts, src)
			} else if n.FirstChild != nil {
				analysis.Scripts = append(analysis.Scripts, "[inline]")
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractElements(c, analysis, originalContent)
	}
}

func hasCSRFToken(n *html.Node) bool {
	if n.Type == html.ElementNode && n.Data == "input" {
		name := strings.ToLower(getAttr(n, "name"))
		if strings.Contains(name, "csrf") || strings.Contains(name, "token") {
			return true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasCSRFToken(c) {
			return true
		}
	}
	return false
}

// hasConfirmAttr checks if form or its submit button has h-confirm attribute
func hasConfirmAttr(n *html.Node) bool {
	// Check form itself
	if getAttr(n, "h-confirm") != "" {
		return true
	}
	// Check for h-confirm on buttons inside the form
	if n.Type == html.ElementNode && (n.Data == "button" || n.Data == "input") {
		if getAttr(n, "h-confirm") != "" {
			return true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasConfirmAttr(c) {
			return true
		}
	}
	return false
}

func extractInputs(n *html.Node, originalContent string) []InputInfo {
	return extractInputsWithContext(n, originalContent, false)
}

func extractInputsWithContext(n *html.Node, originalContent string, insideLabel bool) []InputInfo {
	var inputs []InputInfo

	// Track if we're inside a label element
	if n.Type == html.ElementNode && n.Data == "label" {
		insideLabel = true
	}

	if n.Type == html.ElementNode && (n.Data == "input" || n.Data == "textarea" || n.Data == "select") {
		input := InputInfo{
			Type:             getAttr(n, "type"),
			Name:             getAttr(n, "name"),
			ID:               getAttr(n, "id"),
			HasLabel:         hasAttr(n, "aria-label") || hasAttr(n, "aria-labelledby"),
			HasWrappingLabel: insideLabel,
			Placeholder:      getAttr(n, "placeholder"),
			Line:             findLineNumber(originalContent, n),
		}
		inputs = append(inputs, input)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		inputs = append(inputs, extractInputsWithContext(c, originalContent, insideLabel)...)
	}
	return inputs
}

func findLineNumber(content string, n *html.Node) int {
	tag := "<" + n.Data
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, tag) {
			return i + 1
		}
	}
	return 0
}

func extractText(n *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(text.String())
}

func hasChildElement(n *html.Node, tag string) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return true
		}
		if hasChildElement(c, tag) {
			return true
		}
	}
	return false
}

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func runChecks(analysis *TemplateAnalysis, originalContent string) {
	file := analysis.File
	templateName := analysis.TemplateName

	// === Navigation Checks ===

	// Check: Template has navigation links
	// Skip for partial templates as they're embedded in pages that have navigation
	navLinks := 0
	for _, link := range analysis.Links {
		if strings.HasPrefix(link.Href, "/") || link.Href == "/" {
			navLinks++
		}
	}
	if len(analysis.Links) > 0 && !analysis.IsPartialBlock && !analysis.IsContentBlock {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryNavigation,
			Rule:     "Template contains navigation links",
			Passed:   navLinks > 0,
			Message:  fmt.Sprintf("Found %d navigation links", navLinks),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Has nav element (check on header template since that's where nav lives, or base)
	// The base template includes header via {{template "header"}}
	if templateName == "base" || templateName == "header" {
		hasNav := analysis.HasNav || strings.Contains(originalContent, "<nav")
		// If base, it may use {{template "header"}} which provides nav
		if templateName == "base" && strings.Contains(originalContent, `{{template "header"`) {
			hasNav = true // Base includes header which provides nav
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryNavigation,
			Rule:     "Uses semantic <nav> element",
			Passed:   hasNav,
			Message:  boolToMsg(hasNav, "Has <nav> element (via header)", "Missing <nav> element"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check: Has main element (only for base template - content blocks inherit from base)
	// Skip content blocks and partial templates as they're embedded in base which has main
	if templateName == "base" {
		hasMain := analysis.HasMain || strings.Contains(originalContent, "<main") || strings.Contains(originalContent, `role="main"`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryNavigation,
			Rule:     "Uses semantic <main> element",
			Passed:   hasMain,
			Message:  boolToMsg(hasMain, "Has <main> element", "Missing <main> element"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// === Forms & Actions Checks ===

	// Check: State-changing forms use POST
	for _, form := range analysis.Forms {
		// Skip search forms
		if strings.Contains(form.Action, "search") {
			continue
		}
		isStateChanging := strings.Contains(form.Action, "/post") ||
			strings.Contains(form.Action, "/reply") ||
			strings.Contains(form.Action, "/react") ||
			strings.Contains(form.Action, "/login") ||
			strings.Contains(form.Action, "/logout") ||
			strings.Contains(form.Action, "/repost") ||
			strings.Contains(form.Action, "/quote") ||
			strings.Contains(form.Action, "/delete") ||
			strings.Contains(form.Action, "/bookmark") ||
			strings.Contains(form.Action, "/follow") ||
			strings.Contains(form.Action, "/profile")

		if isStateChanging {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryForms,
				Rule:     "State-changing actions use POST",
				Passed:   form.Method == "POST",
				Message:  fmt.Sprintf("Form %s uses %s", form.Action, form.Method),
				File:     file,
				Line:     form.Line,
				Element:  fmt.Sprintf("form[action=%s]", form.Action),
				Severity: SeverityError,
			})
		}
	}

	// Check: POST forms have CSRF tokens
	// Also check for dynamic field injection patterns like {{range .Fields}}
	hasDynamicFields := strings.Contains(originalContent, "{{range .Fields}}") ||
		strings.Contains(originalContent, "{{range $.Fields}}")
	for _, form := range analysis.Forms {
		if form.Method == "POST" {
			// If the template has dynamic field injection, assume CSRF is handled at runtime
			hasCSRF := form.HasCSRF || hasDynamicFields
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryForms,
				Rule:     "POST forms include CSRF token",
				Passed:   hasCSRF,
				Message:  fmt.Sprintf("Form %s %s CSRF token", form.Action, boolToHas(hasCSRF)),
				File:     file,
				Line:     form.Line,
				Element:  fmt.Sprintf("form[action=%s]", form.Action),
				Severity: SeverityError,
			})
		}
	}

	// Check: Destructive actions have h-confirm
	// These actions should show a confirmation dialog before executing
	destructivePatterns := []string{
		"/mute", "/unmute", "/delete", "/remove", "/disconnect", "/logout",
		"/block", "/unblock", "/ban", "/unfollow",
	}
	for _, form := range analysis.Forms {
		if form.Method == "POST" {
			isDestructive := false
			for _, pattern := range destructivePatterns {
				if strings.Contains(form.Action, pattern) {
					isDestructive = true
					break
				}
			}
			if isDestructive {
				analysis.Checks = append(analysis.Checks, CheckResult{
					Category: CategoryForms,
					Rule:     "Destructive actions have confirmation",
					Passed:   form.HasConfirm,
					Message:  fmt.Sprintf("Form %s %s h-confirm attribute", form.Action, boolToHas(form.HasConfirm)),
					File:     file,
					Line:     form.Line,
					Element:  fmt.Sprintf("form[action=%s]", form.Action),
					Severity: SeverityWarning,
				})
			}
		}
	}

	// Check: No JavaScript event handlers
	jsPatterns := []struct {
		pattern string
		name    string
	}{
		{`onclick\s*=`, "onclick"},
		{`onsubmit\s*=`, "onsubmit"},
		{`onchange\s*=`, "onchange"},
		{`onkeyup\s*=`, "onkeyup"},
		{`onkeydown\s*=`, "onkeydown"},
	}
	hasJSHandler := false
	for _, jp := range jsPatterns {
		if regexp.MustCompile(jp.pattern).MatchString(originalContent) {
			hasJSHandler = true
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryForms,
				Rule:     "No JavaScript event handlers",
				Passed:   false,
				Message:  fmt.Sprintf("Found %s handler", jp.name),
				File:     file,
				Severity: SeverityError,
			})
			break
		}
	}
	if !hasJSHandler && len(analysis.Forms) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryForms,
			Rule:     "No JavaScript event handlers",
			Passed:   true,
			Message:  "No inline JS handlers found",
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check: Forms have submit buttons
	hasSubmitButton := strings.Contains(originalContent, `type="submit"`) || strings.Contains(originalContent, `type='submit'`)
	if len(analysis.Forms) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryForms,
			Rule:     "Forms have submit buttons",
			Passed:   hasSubmitButton,
			Message:  boolToMsg(hasSubmitButton, "Has submit button(s)", "No submit buttons found"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === Links Checks ===

	// Check: No empty links
	emptyLinks := 0
	for _, link := range analysis.Links {
		if link.IsEmpty && !strings.Contains(link.Href, "placeholder") {
			emptyLinks++
		}
	}
	if len(analysis.Links) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "Links have accessible content",
			Passed:   emptyLinks == 0,
			Message:  fmt.Sprintf("%d empty links found", emptyLinks),
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check: Link text is descriptive
	badLinkPatterns := regexp.MustCompile(`(?i)^(click here|here|link|read more|more)$`)
	badLinks := 0
	for _, link := range analysis.Links {
		if link.Text != "" && badLinkPatterns.MatchString(strings.TrimSpace(link.Text)) {
			badLinks++
		}
	}
	if len(analysis.Links) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "Links have descriptive text",
			Passed:   badLinks == 0,
			Message:  boolToMsg(badLinks == 0, "Link text is descriptive", fmt.Sprintf("%d non-descriptive links", badLinks)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: External links have rel="external noopener"
	externalLinksWithRel := 0
	totalExternalLinks := 0
	for _, link := range analysis.Links {
		if link.IsExternal && !strings.Contains(link.Href, "placeholder") {
			totalExternalLinks++
			if strings.Contains(link.Rel, "external") && strings.Contains(link.Rel, "noopener") {
				externalLinksWithRel++
			}
		}
	}
	if totalExternalLinks > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "External links have rel='external noopener'",
			Passed:   externalLinksWithRel == totalExternalLinks,
			Message:  fmt.Sprintf("%d/%d external links have proper rel attribute", externalLinksWithRel, totalExternalLinks),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Navigation links have rel attributes
	navLinksWithRel := 0
	totalNavLinks := 0
	for _, link := range analysis.Links {
		if strings.HasPrefix(link.Href, "/") && !strings.Contains(link.Href, "placeholder") {
			totalNavLinks++
			if link.HasRel {
				navLinksWithRel++
			}
		}
	}
	if totalNavLinks > 3 && !analysis.IsPartialBlock {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "Links have semantic rel attributes",
			Passed:   navLinksWithRel > totalNavLinks/2, // At least half should have rel
			Message:  fmt.Sprintf("%d/%d links have rel attributes", navLinksWithRel, totalNavLinks),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check: Internal links point to valid routes
	invalidRoutes := []string{}
	for _, link := range analysis.Links {
		href := link.Href
		// Skip template placeholders and external links
		if strings.Contains(href, "placeholder") || strings.Contains(href, "{{") ||
			strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") ||
			href == "" || href == "#" || strings.HasPrefix(href, "#") {
			continue
		}
		// Check internal links
		if strings.HasPrefix(href, "/") {
			if !isValidRoute(href) {
				invalidRoutes = append(invalidRoutes, href)
			}
		}
	}
	if len(invalidRoutes) > 0 {
		// Deduplicate
		seen := make(map[string]bool)
		unique := []string{}
		for _, r := range invalidRoutes {
			if !seen[r] {
				seen[r] = true
				unique = append(unique, r)
			}
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "Internal links point to valid routes",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d links to unknown routes: %s", len(unique), strings.Join(unique[:min(5, len(unique))], ", ")),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Anchor links (#id) reference valid IDs
	// Extract all IDs defined in the template
	idPattern := regexp.MustCompile(`id="([^"]+)"`)
	idMatches := idPattern.FindAllStringSubmatch(originalContent, -1)
	definedIDs := make(map[string]bool)
	for _, match := range idMatches {
		definedIDs[match[1]] = true
	}

	// Check anchor links
	invalidAnchors := []string{}
	for _, link := range analysis.Links {
		href := link.Href
		if strings.HasPrefix(href, "#") && len(href) > 1 {
			anchorID := href[1:]
			// Skip template-generated IDs (contain placeholder or look like template vars)
			if strings.Contains(anchorID, "placeholder") || strings.Contains(anchorID, "-placeholder") {
				continue
			}
			// Skip browser-standard anchors like #top
			if anchorID == "top" {
				continue
			}
			// For IDs like "reply-{{.ID}}", the actual ID is dynamic
			// We can't validate these statically, so skip them
			if !definedIDs[anchorID] {
				// Check if there's a pattern match (e.g., reply-* matches reply-{{.ID}})
				hasPatternMatch := false
				for defID := range definedIDs {
					if strings.Contains(defID, "placeholder") {
						prefix := strings.Split(defID, "placeholder")[0]
						if strings.HasPrefix(anchorID, prefix) {
							hasPatternMatch = true
							break
						}
					}
				}
				if !hasPatternMatch {
					invalidAnchors = append(invalidAnchors, href)
				}
			}
		}
	}
	if len(invalidAnchors) > 0 && len(invalidAnchors) <= 5 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryLinks,
			Rule:     "Anchor links reference valid IDs",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d anchor links with potentially missing IDs: %s", len(invalidAnchors), strings.Join(invalidAnchors, ", ")),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// === Self-Describing Checks ===

	// Check: Template has title (for base template)
	if templateName == "base" {
		hasTitle := analysis.HasTitle || strings.Contains(originalContent, "<title")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySelfDescribe,
			Rule:     "Page has title element",
			Passed:   hasTitle,
			Message:  boolToMsg(hasTitle, "Has <title> element", "Missing <title> element"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Inputs have labels or placeholders
	inputsWithLabels := 0
	totalInputs := 0
	for _, form := range analysis.Forms {
		for _, input := range form.Inputs {
			if input.Type == "hidden" || input.Type == "submit" {
				continue
			}
			totalInputs++
			// Input has accessible name if: wrapped in label, has aria-label, has placeholder, or has id (for external label)
			if input.HasWrappingLabel || input.HasLabel || input.Placeholder != "" || input.ID != "" {
				inputsWithLabels++
			}
		}
	}
	if totalInputs > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySelfDescribe,
			Rule:     "Form inputs have labels or placeholders",
			Passed:   inputsWithLabels == totalInputs,
			Message:  fmt.Sprintf("%d/%d inputs have accessible names", inputsWithLabels, totalInputs),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === State Transfer Checks ===

	// Check: No fetch() or XMLHttpRequest
	hasFetch := strings.Contains(originalContent, "fetch(") || strings.Contains(originalContent, "XMLHttpRequest")
	if strings.Contains(originalContent, "<script") {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryStateTransfer,
			Rule:     "No AJAX/fetch for state changes",
			Passed:   !hasFetch,
			Message:  boolToMsg(!hasFetch, "No fetch/XHR found", "Found fetch/XHR calls"),
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check: No JSON API references in HTML
	hasJSONAPI := strings.Contains(originalContent, `"application/json"`) ||
		regexp.MustCompile(`/api/[^"']*`).MatchString(originalContent)
	if len(analysis.Links) > 0 || len(analysis.Forms) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryStateTransfer,
			Rule:     "HTML pages don't reference JSON APIs",
			Passed:   !hasJSONAPI,
			Message:  boolToMsg(!hasJSONAPI, "No JSON API references", "Found JSON API references"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === Accessibility Checks ===

	// Check: Images have alt text
	imgPattern := regexp.MustCompile(`<img[^>]*>`)
	altPattern := regexp.MustCompile(`alt\s*=`)
	imgs := imgPattern.FindAllString(originalContent, -1)
	imgsWithAlt := 0
	for _, img := range imgs {
		if altPattern.MatchString(img) {
			imgsWithAlt++
		}
	}
	if len(imgs) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryAccessibility,
			Rule:     "Images have alt attributes",
			Passed:   imgsWithAlt == len(imgs),
			Message:  fmt.Sprintf("%d/%d images have alt text", imgsWithAlt, len(imgs)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: No external scripts
	// Skip template variables (contain "{{") and placeholder values (from stripGoTemplates)
	for _, script := range analysis.Scripts {
		if script != "[inline]" &&
			script != "placeholder" &&
			!strings.HasPrefix(script, "/") &&
			!strings.HasPrefix(script, ".") &&
			!strings.Contains(script, "{{") {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryAccessibility,
				Rule:     "No external JavaScript dependencies",
				Passed:   false,
				Message:  fmt.Sprintf("External script: %s", script),
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// Check: Uses semantic HTML elements
	// For partial templates (kind-*, note-*, author-*, etc.), lower the threshold
	// since they're embedded in a base template that provides overall structure
	semanticElements := []string{"<header", "<footer", "<article", "<section", "<aside", "<nav", "<main"}
	semanticCount := 0
	for _, elem := range semanticElements {
		if strings.Contains(originalContent, elem) {
			semanticCount++
		}
	}
	if len(analysis.Links) > 3 || len(analysis.Forms) > 0 {
		// Partial templates only need 1 semantic element (or none if very small)
		// Base and content templates should have 2+
		threshold := 2
		if analysis.IsPartialBlock || analysis.IsContentBlock {
			threshold = 0 // Partials inherit structure from base
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryAccessibility,
			Rule:     "Uses semantic HTML elements",
			Passed:   semanticCount >= threshold,
			Message:  fmt.Sprintf("Found %d semantic elements", semanticCount),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check: divs with class="*-section" should use <section> element
	// Exclude CSS-only patterns like sticky-section, login-section, edit-form-section
	divSectionPattern := regexp.MustCompile(`<div[^>]*class="[^"]*-section[^"]*"`)
	sectionDivs := divSectionPattern.FindAllString(originalContent, -1)
	// Filter out CSS-only patterns
	cssOnlyPatterns := []string{"sticky-section", "login-section", "edit-form-section", "bookmarks-section"}
	actualSectionDivs := 0
	for _, div := range sectionDivs {
		isCSSOnly := false
		for _, pattern := range cssOnlyPatterns {
			if strings.Contains(div, pattern) {
				isCSSOnly = true
				break
			}
		}
		if !isCSSOnly {
			actualSectionDivs++
		}
	}
	if actualSectionDivs > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryAccessibility,
			Rule:     "Section-like content uses <section> element",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d <div class=\"*-section\"> that should be <section>", actualSectionDivs),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// === ERROR RESPONSES ===

	// Only check error handling for dedicated error page templates
	// Skip templates that just have error display containers (id="post-error", etc.)
	isErrorPageTemplate := strings.Contains(strings.ToLower(file), "error") ||
		strings.Contains(strings.ToLower(file), "404") ||
		strings.Contains(strings.ToLower(file), "500") ||
		strings.Contains(strings.ToLower(templateName), "error") ||
		strings.Contains(strings.ToLower(templateName), "404") ||
		strings.Contains(strings.ToLower(templateName), "500")

	// Check: Error page templates include navigation back (not dead ends)
	if isErrorPageTemplate {
		hasNavigation := strings.Contains(originalContent, "<a") ||
			strings.Contains(originalContent, "<nav") ||
			strings.Contains(originalContent, "href=") ||
			strings.Contains(originalContent, "return") ||
			strings.Contains(originalContent, "back")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryErrorHandling,
			Rule:     "Error states include navigation",
			Passed:   hasNavigation,
			Message:  boolToMsg(hasNavigation, "Error content includes navigation options", "Error messages should include navigation to recover"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: 404/Not found pages have helpful navigation (only for dedicated 404 templates)
	if strings.Contains(strings.ToLower(templateName), "404") ||
		strings.Contains(strings.ToLower(templateName), "notfound") ||
		strings.Contains(strings.ToLower(file), "404") ||
		strings.Contains(strings.ToLower(file), "notfound") {
		hasHelpfulNav := strings.Contains(originalContent, "home") ||
			strings.Contains(originalContent, "search") ||
			strings.Contains(originalContent, "back") ||
			strings.Contains(originalContent, "<nav")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryErrorHandling,
			Rule:     "404 pages have helpful navigation",
			Passed:   hasHelpfulNav,
			Message:  boolToMsg(hasHelpfulNav, "Not found page includes helpful links", "404 page should include links to home, search, or back"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Form validation errors are associated with fields
	// Only check if template has explicit field-level error patterns, not just error containers
	hasFormWithFieldErrors := strings.Contains(originalContent, "field-error") ||
		strings.Contains(originalContent, "input-error") ||
		strings.Contains(originalContent, "error-field")
	if hasFormWithFieldErrors && strings.Contains(originalContent, "<form") {
		hasFieldAssociation := strings.Contains(originalContent, "aria-describedby") ||
			strings.Contains(originalContent, "aria-invalid")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryErrorHandling,
			Rule:     "Form errors associated with fields",
			Passed:   hasFieldAssociation,
			Message:  boolToMsg(hasFieldAssociation, "Form errors are associated with input fields", "Form validation errors should be linked to specific fields"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// === PAGINATION ===

	// Check: Pagination uses hypermedia links (rel=next/prev), not JS
	// Trust templates that include OR define the shared pagination template
	usesPaginationTemplate := strings.Contains(originalContent, `{{template "pagination"`) ||
		strings.Contains(originalContent, `{{define "pagination"}`)
	paginationPattern := regexp.MustCompile(`(?i)(pagination|page-nav|pager|load-more|next-page|prev-page)`)
	if paginationPattern.MatchString(originalContent) || strings.Contains(originalContent, "?page=") ||
		strings.Contains(originalContent, "&page=") {
		// Check for proper hypermedia pagination
		hasRelLinks := strings.Contains(originalContent, `rel="next"`) ||
			strings.Contains(originalContent, `rel="prev"`) ||
			strings.Contains(originalContent, `rel='next'`) ||
			strings.Contains(originalContent, `rel='prev'`) ||
			usesPaginationTemplate // Trust template includes
		hasHypermediaLinks := strings.Contains(originalContent, "href=") &&
			(strings.Contains(originalContent, "page") || strings.Contains(originalContent, "offset") ||
				strings.Contains(originalContent, "cursor") || strings.Contains(originalContent, "since")) ||
			usesPaginationTemplate // Trust template includes

		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPagination,
			Rule:     "Pagination uses hypermedia links",
			Passed:   hasHypermediaLinks,
			Message:  boolToMsg(hasHypermediaLinks, "Pagination uses href links", "Pagination should use <a href> links, not JavaScript"),
			File:     file,
			Severity: SeverityWarning,
		})

		// Check for rel=next/prev for SEO and accessibility
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPagination,
			Rule:     "Pagination has rel=next/prev",
			Passed:   hasRelLinks,
			Message:  boolToMsg(hasRelLinks, "Pagination links have rel attributes", "Pagination links should have rel=\"next\"/\"prev\" for semantics"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check: Infinite scroll has alternative (load more button or pagination links)
	// Only check if template appears to be paginating a list (not just lazy-loading a single element)
	infiniteScrollPattern := regexp.MustCompile(`(?i)(infinite-scroll|scroll-load|auto-load|intersect.*load)`)
	hasIntersectTrigger := strings.Contains(originalContent, "h-trigger=\"intersect")
	hasListContext := strings.Contains(originalContent, "notes-list") ||
		strings.Contains(originalContent, "{{range") ||
		strings.Contains(originalContent, "append") ||
		strings.Contains(originalContent, "pagination") ||
		strings.Contains(originalContent, "load-more")
	if infiniteScrollPattern.MatchString(originalContent) || (hasIntersectTrigger && hasListContext) {
		hasAlternative := strings.Contains(originalContent, "load-more") ||
			strings.Contains(originalContent, "Load more") ||
			strings.Contains(originalContent, "Show more") ||
			strings.Contains(originalContent, "?page=") ||
			strings.Contains(originalContent, "pagination") ||
			usesPaginationTemplate // Trust template includes
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPagination,
			Rule:     "Infinite scroll has fallback",
			Passed:   hasAlternative,
			Message:  boolToMsg(hasAlternative, "Infinite scroll has load-more or pagination alternative", "Infinite scroll should have a manual load-more option for accessibility"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check: Timeline/feed has "load newer" link for fresh content
	if strings.Contains(templateName, "timeline") || strings.Contains(templateName, "feed") {
		hasRefreshLink := strings.Contains(originalContent, "refresh") ||
			strings.Contains(originalContent, "new-notes") ||
			strings.Contains(originalContent, "load-new") ||
			strings.Contains(originalContent, "newer")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPagination,
			Rule:     "Timeline has refresh/newer option",
			Passed:   hasRefreshLink,
			Message:  boolToMsg(hasRefreshLink, "Timeline has option to load newer content", "Timelines should have a way to load newer items"),
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

func boolToMsg(b bool, trueMsg, falseMsg string) string {
	if b {
		return trueMsg
	}
	return falseMsg
}

func boolToHas(b bool) string {
	if b {
		return "has"
	}
	return "lacks"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isValidRoute checks if a given path matches any known route
func isValidRoute(path string) bool {
	// Strip query string
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}

	for _, route := range knownRoutes {
		// Exact match
		if path == route {
			return true
		}
		// Prefix match for routes ending with /
		if strings.HasSuffix(route, "/") && strings.HasPrefix(path, route) {
			return true
		}
		// Route pattern match (e.g., /thread/ matches /thread/abc123)
		if strings.HasSuffix(route, "/") && strings.HasPrefix(path, strings.TrimSuffix(route, "/")) {
			return true
		}
	}
	return false
}

func calculateSummary(report *Report) {
	categories := map[string]*CategorySummary{}

	for _, tmpl := range report.Templates {
		for _, check := range tmpl.Checks {
			if _, ok := categories[check.Category]; !ok {
				categories[check.Category] = &CategorySummary{}
			}
			cat := categories[check.Category]
			cat.Total++
			if check.Passed {
				cat.Passed++
			} else {
				cat.Failed++
			}
		}
	}

	totalPassed := 0
	totalChecks := 0
	for name, cat := range categories {
		if cat.Total > 0 {
			cat.Score = float64(cat.Passed) / float64(cat.Total) * 100
		}
		report.Summary[name] = *cat
		totalPassed += cat.Passed
		totalChecks += cat.Total
	}

	if totalChecks > 0 {
		report.TotalScore = float64(totalPassed) / float64(totalChecks) * 100
	}
}

func generateHTMLReport(report *Report, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Calculate grade
	grade := "F"
	switch {
	case report.TotalScore >= 95:
		grade = "A+"
	case report.TotalScore >= 90:
		grade = "A"
	case report.TotalScore >= 85:
		grade = "A-"
	case report.TotalScore >= 80:
		grade = "B+"
	case report.TotalScore >= 75:
		grade = "B"
	case report.TotalScore >= 70:
		grade = "B-"
	case report.TotalScore >= 65:
		grade = "C+"
	case report.TotalScore >= 60:
		grade = "C"
	case report.TotalScore >= 55:
		grade = "C-"
	case report.TotalScore >= 50:
		grade = "D"
	}

	scoreColor := "#22c55e" // green
	if report.TotalScore < 70 {
		scoreColor = "#f59e0b" // amber
	}
	if report.TotalScore < 50 {
		scoreColor = "#ef4444" // red
	}

	fmt.Fprintf(f, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>HATEOAS Compliance Report</title>
    <style>
        :root {
            --bg: #0d1117;
            --bg-secondary: #161b22;
            --text: #c9d1d9;
            --text-muted: #8b949e;
            --border: #30363d;
            --green: #238636;
            --red: #da3633;
            --amber: #d29922;
            --blue: #58a6ff;
            --purple: #a371f7;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.6;
            padding: 2rem;
        }
        .container { max-width: 1200px; margin: 0 auto; }
        h1 { font-size: 2rem; margin-bottom: 0.5rem; }
        h2 { font-size: 1.5rem; margin: 2rem 0 1rem; border-bottom: 1px solid var(--border); padding-bottom: 0.5rem; }
        h3 { font-size: 1.2rem; margin: 1.5rem 0 0.5rem; color: var(--text-muted); }
        .meta { color: var(--text-muted); margin-bottom: 2rem; }

        .score-card {
            display: flex;
            align-items: center;
            gap: 2rem;
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 12px;
            padding: 2rem;
            margin-bottom: 2rem;
        }
        .score-circle {
            width: 120px;
            height: 120px;
            border-radius: 50%%;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            font-size: 2.5rem;
            font-weight: bold;
            border: 4px solid;
        }
        .score-label { font-size: 0.8rem; color: var(--text-muted); }
        .score-details { flex: 1; }

        .category-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 1rem;
            margin-bottom: 2rem;
        }
        .category-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 1rem;
        }
        .category-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 0.5rem;
        }
        .category-name { font-weight: 600; }
        .category-score {
            font-size: 1.2rem;
            font-weight: bold;
        }
        .progress-bar {
            height: 8px;
            background: var(--border);
            border-radius: 4px;
            overflow: hidden;
        }
        .progress-fill {
            height: 100%%;
            border-radius: 4px;
            transition: width 0.3s;
        }
        .category-desc {
            margin-top: 0.5rem;
            color: var(--text-muted);
            font-size: 0.85rem;
        }

        .template-section {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            margin-bottom: 1rem;
            overflow: hidden;
        }
        .template-header {
            padding: 1rem;
            border-bottom: 1px solid var(--border);
            cursor: pointer;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .template-header:hover { background: rgba(255,255,255,0.02); }
        .template-name { font-family: monospace; color: var(--blue); }
        .template-file { color: var(--text-muted); font-size: 0.85rem; margin-left: 1rem; }
        .template-stats { display: flex; gap: 1rem; }
        .stat { padding: 0.2rem 0.5rem; border-radius: 4px; font-size: 0.85rem; }
        .stat-pass { background: rgba(35,134,54,0.2); color: var(--green); }
        .stat-fail { background: rgba(218,54,51,0.2); color: var(--red); }

        .checks-list {
            padding: 0;
            display: none;
        }
        .template-section.open .checks-list { display: block; }
        .check-item {
            padding: 0.75rem 1rem;
            border-bottom: 1px solid var(--border);
            display: flex;
            gap: 1rem;
            align-items: flex-start;
        }
        .check-item:last-child { border-bottom: none; }
        .check-icon {
            width: 20px;
            height: 20px;
            border-radius: 50%%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 12px;
            flex-shrink: 0;
        }
        .check-pass { background: var(--green); }
        .check-fail { background: var(--red); }
        .check-details { flex: 1; }
        .check-rule { font-weight: 500; }
        .check-message { color: var(--text-muted); font-size: 0.9rem; }
        .check-location { color: var(--text-muted); font-size: 0.8rem; font-family: monospace; }
        .check-category {
            font-size: 0.75rem;
            background: var(--border);
            padding: 0.15rem 0.5rem;
            border-radius: 3px;
            color: var(--text-muted);
        }

        .toggle-btn {
            background: var(--bg);
            border: 1px solid var(--border);
            color: var(--text);
            padding: 0.5rem 1rem;
            border-radius: 6px;
            cursor: pointer;
            margin-bottom: 1rem;
        }
        .toggle-btn:hover { border-color: var(--text-muted); }

        .hateoas-link {
            color: var(--blue);
            text-decoration: none;
        }
        .hateoas-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>HATEOAS Compliance Report</h1>
        <p class="meta">Generated: %s | Templates analyzed: %d</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: %s; color: %s;">
                %s
                <span class="score-label">%.0f%%</span>
            </div>
            <div class="score-details">
                <h3>Overall Compliance Score</h3>
                <p>Your templates were checked against
                <a href="https://hypermedia.systems/" class="hateoas-link">hypermedia best practices</a>.</p>
                <p style="margin-top: 0.5rem; color: var(--text-muted);">
                    This static analysis scans template source code directly, checking for HATEOAS principles:
                    HTML as the engine of application state, proper form methods, CSRF protection,
                    no JavaScript requirements for core functionality.
                </p>
            </div>
        </div>

        <h2>Category Breakdown</h2>
        <div class="category-grid">
`,
		report.GeneratedAt.Format("2006-01-02 15:04:05"),
		len(report.Templates),
		scoreColor, scoreColor,
		grade,
		report.TotalScore,
	)

	// Category descriptions
	categoryDescs := map[string]string{
		CategoryNavigation:    "Navigation links and semantic structure",
		CategoryForms:         "Form methods, CSRF tokens, and submit buttons",
		CategoryLinks:         "Link accessibility and descriptive text",
		CategorySelfDescribe:  "Page titles, labels, and self-documenting elements",
		CategoryStateTransfer: "State changes via HTML, not JavaScript",
		CategoryAccessibility: "Semantic HTML and accessibility attributes",
		CategoryErrorHandling: "Error pages with navigation and helpful messages",
		CategoryPagination:    "Hypermedia-based pagination with proper semantics",
	}

	categories := []string{CategoryNavigation, CategoryForms, CategoryLinks, CategorySelfDescribe, CategoryStateTransfer, CategoryAccessibility, CategoryErrorHandling, CategoryPagination}
	for _, cat := range categories {
		if summary, ok := report.Summary[cat]; ok {
			color := "#22c55e"
			if summary.Score < 70 {
				color = "#f59e0b"
			}
			if summary.Score < 50 {
				color = "#ef4444"
			}
			fmt.Fprintf(f, `
            <div class="category-card">
                <div class="category-header">
                    <span class="category-name">%s</span>
                    <span class="category-score" style="color: %s">%.0f%%</span>
                </div>
                <div class="progress-bar">
                    <div class="progress-fill" style="width: %.0f%%; background: %s;"></div>
                </div>
                <p class="category-desc">%s</p>
                <p style="margin-top: 0.25rem; color: var(--text-muted); font-size: 0.8rem;">
                    %d passed / %d total checks
                </p>
            </div>
`, cat, color, summary.Score, summary.Score, color, categoryDescs[cat], summary.Passed, summary.Total)
		}
	}

	fmt.Fprintf(f, `
        </div>

        <h2>Detailed Findings</h2>
        <button class="toggle-btn" onclick="document.querySelectorAll('.template-section').forEach(s => s.classList.toggle('open'))">
            Toggle All
        </button>
`)

	// Group all checks by category
	checksByCategory := make(map[string][]CheckResult)
	for _, tmpl := range report.Templates {
		for _, check := range tmpl.Checks {
			checksByCategory[check.Category] = append(checksByCategory[check.Category], check)
		}
	}

	// Output categories in consistent order
	for _, cat := range categories {
		checks, ok := checksByCategory[cat]
		if !ok || len(checks) == 0 {
			continue
		}

		passed := 0
		failed := 0
		for _, check := range checks {
			if check.Passed {
				passed++
			} else {
				failed++
			}
		}

		// Only show categories with failures expanded by default
		openClass := ""
		if failed > 0 {
			openClass = " open"
		}

		fmt.Fprintf(f, `
        <div class="template-section%s">
            <div class="template-header" onclick="this.parentElement.classList.toggle('open')">
                <span class="template-name">%s</span>
                <div class="template-stats">
                    <span class="stat stat-pass">%d passed</span>
                    <span class="stat stat-fail">%d failed</span>
                </div>
            </div>
            <div class="checks-list">
`, openClass, cat, passed, failed)

		// Sort checks: failures first, then by file
		sort.Slice(checks, func(i, j int) bool {
			if checks[i].Passed != checks[j].Passed {
				return !checks[i].Passed
			}
			return checks[i].File < checks[j].File
		})

		for _, check := range checks {
			iconClass := "check-pass"
			iconStr := "✓"
			if !check.Passed {
				iconStr = "✗"
				iconClass = "check-fail"
			}

			location := ""
			if check.File != "" {
				fileName := filepath.Base(check.File)
				if check.Line > 0 {
					location = fmt.Sprintf(`<div class="check-location">%s:%d</div>`, fileName, check.Line)
				} else {
					location = fmt.Sprintf(`<div class="check-location">%s</div>`, fileName)
				}
			}

			fmt.Fprintf(f, `
                <div class="check-item">
                    <div class="check-icon %s">%s</div>
                    <div class="check-details">
                        <div class="check-rule">%s</div>
                        <div class="check-message">%s</div>
                        %s
                    </div>
                </div>
`, iconClass, iconStr, htmlpkg.EscapeString(check.Rule), htmlpkg.EscapeString(check.Message), location)
		}

		fmt.Fprintf(f, `
            </div>
        </div>
`)
	}

	fmt.Fprintf(f, `
        <h2>Resources</h2>
        <ul style="margin-left: 1.5rem; color: var(--text-muted);">
            <li><a href="https://hypermedia.systems/" class="hateoas-link">Hypermedia Systems Book</a></li>
            <li><a href="https://htmx.org/essays/hateoas/" class="hateoas-link">HATEOAS Essay</a></li>
            <li><a href="https://www.w3.org/WAI/WCAG21/quickref/" class="hateoas-link">WCAG 2.1 Quick Reference</a></li>
        </ul>

        <h2>Limitations</h2>
        <p style="color: var(--text-muted);">
            This static analysis cannot check:
        </p>
        <ul style="margin-left: 1.5rem; color: var(--text-muted); margin-top: 0.5rem;">
            <li>HTTP response headers (Content-Type, caching)</li>
            <li>Runtime link discovery and crawlability</li>
            <li>Server-side redirect behavior</li>
            <li>Actual CSRF token validation</li>
        </ul>
    </div>
</body>
</html>
`)

	return nil
}
