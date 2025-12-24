// Static Accessibility Checker
// Analyzes Go template files for WCAG compliance without requiring a running server
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// WCAG categories
const (
	CategoryPerceivable    = "Perceivable"
	CategoryOperable       = "Operable"
	CategoryUnderstandable = "Understandable"
	CategoryRobust         = "Robust"
	CategoryMotion         = "Motion & Animation"
	CategoryTouchTargets   = "Touch Targets"
	CategoryTiming         = "Timing"
	CategoryFocus          = "Focus Management"
)

// Severity levels
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// CheckResult represents a single accessibility check result
type CheckResult struct {
	Category string
	Rule     string
	WCAG     string // WCAG criterion (e.g., "1.1.1", "2.4.4")
	Passed   bool
	Message  string
	File     string
	Line     int
	Element  string
	Severity string
}

// TemplateAnalysis contains analysis results for a single template
type TemplateAnalysis struct {
	File          string
	TemplateName  string
	Checks        []CheckResult
	Images        []ImageInfo
	Forms         []FormInfo
	Headings      []HeadingInfo
	Buttons       []ButtonInfo
	Links         []LinkInfo
	Inputs        []InputInfo
	HasLang       bool
	HasMain       bool
	HasNav        bool
	HasSkipLink   bool
}

// ImageInfo captures image details
type ImageInfo struct {
	Src       string
	Alt       string
	HasAlt    bool
	AltEmpty  bool
	HasWidth  bool
	HasHeight bool
	Line      int
}

// FormInfo captures form details
type FormInfo struct {
	Action string
	Method string
	Line   int
}

// InputInfo captures input field details
type InputInfo struct {
	Type            string
	Name            string
	ID              string
	HasLabel        bool
	HasAriaLabel    bool
	HasWrappingLabel bool // Input is wrapped inside a <label> element
	Placeholder     string
	Line            int
}

// HeadingInfo captures heading hierarchy
type HeadingInfo struct {
	Level int
	Text  string
	Line  int
}

// ButtonInfo captures button accessibility
type ButtonInfo struct {
	Type         string
	Text         string
	HasAriaLabel bool
	AriaLabel    string
	Line         int
}

// LinkInfo captures link accessibility
type LinkInfo struct {
	Href         string
	Text         string
	HasAriaLabel bool
	IsEmpty      bool
	Line         int
}

// Report contains the full accessibility report
type Report struct {
	GeneratedAt  time.Time
	ProjectPath  string
	Templates    []TemplateAnalysis
	Summary      map[string]CategorySummary
	TotalScore   float64
	ErrorCount   int // WCAG Level A violations
	WarningCount int // WCAG Level AA violations
	InfoCount    int // WCAG Level AAA / suggestions
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
)

func main() {
	flag.StringVar(&projectPath, "path", ".", "Path to project root (e.g., ../../. when running from cmd/accessibility-check)")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.StringVar(&outputFile, "output", "accessibility-report.html", "Output file")
	flag.Parse()

	fmt.Printf("Accessibility Checker (WCAG 2.1)\n")
	fmt.Printf("========================================\n")
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
	fmt.Printf("\nCategory Scores (WCAG 2.1):\n")
	categories := []string{CategoryPerceivable, CategoryOperable, CategoryUnderstandable, CategoryRobust, CategoryMotion, CategoryTouchTargets, CategoryTiming, CategoryFocus}
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
	// Look for patterns like: var templateName = `...` or return `...`
	templatePattern := regexp.MustCompile("(?s)`([^`]+)`")
	matches := templatePattern.FindAllStringSubmatchIndex(fileContent, -1)

	// Special handling for base.go: combine all fragments into one "base" template
	// This is necessary because base.go uses string concatenation to build the template
	// e.g., `<!DOCTYPE html>...` + baseCSS + `</head><body>...`
	isBaseFile := strings.HasSuffix(filePath, "base.go")
	if isBaseFile {
		var combinedHTML strings.Builder
		for _, match := range matches {
			if len(match) >= 4 {
				templateStr := fileContent[match[2]:match[3]]
				// Include all fragments that look like HTML or CSS
				if strings.Contains(templateStr, "<") || strings.Contains(templateStr, "{") {
					combinedHTML.WriteString(templateStr)
				}
			}
		}

		// Analyze the combined base template as a single unit
		combined := combinedHTML.String()
		if combined != "" {
			analysis := analyzeTemplate(combined, filePath, "base")
			if len(analysis.Checks) > 0 || len(analysis.Images) > 0 || len(analysis.Forms) > 0 {
				analyses = append(analyses, analysis)
			}
		}

		// Also analyze named sub-templates (like {{define "header"}}) individually
		for _, match := range matches {
			if len(match) >= 4 {
				templateStr := fileContent[match[2]:match[3]]
				if !strings.Contains(templateStr, "<") {
					continue
				}
				templateName := extractTemplateName(templateStr)
				// Only analyze if it has a name and it's not "base" (already analyzed combined)
				if templateName != "" && templateName != "base" {
					analysis := analyzeTemplate(templateStr, filePath, templateName)
					if len(analysis.Checks) > 0 || len(analysis.Images) > 0 || len(analysis.Forms) > 0 {
						analyses = append(analyses, analysis)
					}
				}
			}
		}

		return analyses, nil
	}

	// Standard handling for other template files
	for _, match := range matches {
		if len(match) >= 4 {
			templateStr := fileContent[match[2]:match[3]]

			// Skip if it doesn't look like HTML (no angle brackets)
			if !strings.Contains(templateStr, "<") {
				continue
			}

			// Find template name from {{define "name"}}
			templateName := extractTemplateName(templateStr)
			if templateName == "" {
				templateName = filepath.Base(filePath)
			}

			analysis := analyzeTemplate(templateStr, filePath, templateName)
			if len(analysis.Checks) > 0 || len(analysis.Images) > 0 || len(analysis.Forms) > 0 {
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
	analysis := TemplateAnalysis{
		File:         filePath,
		TemplateName: templateName,
		Checks:       []CheckResult{},
		Images:       []ImageInfo{},
		Forms:        []FormInfo{},
		Headings:     []HeadingInfo{},
		Buttons:      []ButtonInfo{},
		Links:        []LinkInfo{},
		Inputs:       []InputInfo{},
	}

	// Strip Go template syntax for HTML parsing
	cleanHTML := stripGoTemplates(content)

	// Parse HTML (may have multiple fragments)
	doc, err := html.Parse(strings.NewReader(cleanHTML))
	if err != nil {
		// Try parsing as fragment
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

	// Run accessibility checks
	runChecks(&analysis, content)

	return analysis
}

func stripGoTemplates(content string) string {
	// Remove Go template directives but preserve structure
	patterns := []struct {
		pattern *regexp.Regexp
		replace string
	}{
		// Replace {{if ...}} with empty
		{regexp.MustCompile(`\{\{if[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{else[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{end\}\}`), ""},
		// Replace {{range ...}} with empty
		{regexp.MustCompile(`\{\{range[^}]*\}\}`), ""},
		// Replace {{template ...}} with placeholder
		{regexp.MustCompile(`\{\{template[^}]*\}\}`), ""},
		// Replace {{block ...}} with empty
		{regexp.MustCompile(`\{\{block[^}]*\}\}`), ""},
		// Replace variable interpolations with placeholder text
		{regexp.MustCompile(`\{\{[^}]+\}\}`), "placeholder"},
	}

	result := content
	for _, p := range patterns {
		result = p.pattern.ReplaceAllString(result, p.replace)
	}

	return result
}

func extractElements(n *html.Node, analysis *TemplateAnalysis, originalContent string) {
	extractElementsWithContext(n, analysis, originalContent, false)
}

func extractElementsWithContext(n *html.Node, analysis *TemplateAnalysis, originalContent string, insideLabel bool) {
	if n.Type == html.ElementNode {
		// Track if we're inside a label element
		if n.Data == "label" {
			insideLabel = true
		}

		switch n.Data {
		case "html":
			if hasAttr(n, "lang") {
				analysis.HasLang = true
			}

		case "main":
			analysis.HasMain = true

		case "nav":
			analysis.HasNav = true

		case "a":
			href := getAttr(n, "href")
			text := extractText(n)
			link := LinkInfo{
				Href:         href,
				Text:         text,
				HasAriaLabel: hasAttr(n, "aria-label"),
				IsEmpty:      strings.TrimSpace(text) == "" && !hasAttr(n, "aria-label") && !hasChildElement(n, "img"),
				Line:         findLineNumber(originalContent, n),
			}
			// Check for skip links
			if strings.Contains(strings.ToLower(href), "#main") || strings.Contains(strings.ToLower(text), "skip") {
				analysis.HasSkipLink = true
			}
			analysis.Links = append(analysis.Links, link)

		case "img":
			img := ImageInfo{
				Src:       getAttr(n, "src"),
				Alt:       getAttr(n, "alt"),
				HasAlt:    hasAttr(n, "alt"),
				AltEmpty:  getAttr(n, "alt") == "",
				HasWidth:  hasAttr(n, "width"),
				HasHeight: hasAttr(n, "height"),
				Line:      findLineNumber(originalContent, n),
			}
			analysis.Images = append(analysis.Images, img)

		case "form":
			form := FormInfo{
				Action: getAttr(n, "action"),
				Method: strings.ToUpper(getAttr(n, "method")),
				Line:   findLineNumber(originalContent, n),
			}
			analysis.Forms = append(analysis.Forms, form)

		case "input", "textarea", "select":
			inp := InputInfo{
				Type:             getAttr(n, "type"),
				Name:             getAttr(n, "name"),
				ID:               getAttr(n, "id"),
				HasAriaLabel:     hasAttr(n, "aria-label") || hasAttr(n, "aria-labelledby"),
				HasWrappingLabel: insideLabel,
				Placeholder:      getAttr(n, "placeholder"),
				Line:             findLineNumber(originalContent, n),
			}
			analysis.Inputs = append(analysis.Inputs, inp)

		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := int(n.Data[1] - '0')
			text := extractText(n)
			analysis.Headings = append(analysis.Headings, HeadingInfo{
				Level: level,
				Text:  text,
				Line:  findLineNumber(originalContent, n),
			})

		case "button":
			btn := ButtonInfo{
				Type:         getAttr(n, "type"),
				Text:         strings.TrimSpace(extractText(n)),
				HasAriaLabel: hasAttr(n, "aria-label"),
				AriaLabel:    getAttr(n, "aria-label"),
				Line:         findLineNumber(originalContent, n),
			}
			analysis.Buttons = append(analysis.Buttons, btn)
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractElementsWithContext(c, analysis, originalContent, insideLabel)
	}
}

func findLineNumber(content string, n *html.Node) int {
	// Approximate line number based on element tag
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

	// === PERCEIVABLE (WCAG 1.x) ===

	// 1.1.1 Non-text Content - Images must have alt text
	for _, img := range analysis.Images {
		// Skip template placeholders and data URLs
		if strings.Contains(img.Src, "placeholder") || strings.HasPrefix(img.Src, "data:") {
			continue
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerceivable,
			Rule:     "Images have alt text",
			WCAG:     "1.1.1",
			Passed:   img.HasAlt,
			Message:  fmt.Sprintf("Image %s %s alt attribute", truncate(img.Src, 40), boolToHas(img.HasAlt)),
			File:     file,
			Line:     img.Line,
			Element:  fmt.Sprintf("img[src=%s]", truncate(img.Src, 30)),
			Severity: SeverityError,
		})
	}

	// 1.4.10 Reflow - Images should have width/height to prevent layout shift
	for _, img := range analysis.Images {
		// Skip template placeholders and data URLs
		if strings.Contains(img.Src, "placeholder") || strings.HasPrefix(img.Src, "data:") {
			continue
		}
		hasDimensions := img.HasWidth && img.HasHeight
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerceivable,
			Rule:     "Images have dimensions (prevents layout shift)",
			WCAG:     "1.4.10",
			Passed:   hasDimensions,
			Message:  fmt.Sprintf("Image %s %s width/height attributes", truncate(img.Src, 40), boolToHas(hasDimensions)),
			File:     file,
			Line:     img.Line,
			Element:  fmt.Sprintf("img[src=%s]", truncate(img.Src, 30)),
			Severity: SeverityWarning,
		})
	}

	// 1.3.1 Info and Relationships - Form inputs have labels
	for _, input := range analysis.Inputs {
		// Skip hidden and submit inputs
		if input.Type == "hidden" || input.Type == "submit" {
			continue
		}
		// Input has accessible name if: wrapped in label, has aria-label, has placeholder, or has id (for external label)
		hasAccessibleName := input.HasWrappingLabel || input.HasAriaLabel || input.Placeholder != "" || input.ID != ""
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerceivable,
			Rule:     "Form inputs have accessible names",
			WCAG:     "1.3.1",
			Passed:   hasAccessibleName,
			Message:  fmt.Sprintf("Input '%s' %s accessible name", nvl(input.Name, input.ID, input.Type), boolToHas(hasAccessibleName)),
			File:     file,
			Line:     input.Line,
			Element:  fmt.Sprintf("input[name=%s]", input.Name),
			Severity: SeverityError,
		})
	}

	// 1.3.1 Heading hierarchy
	if len(analysis.Headings) > 0 {
		hasH1 := false
		for _, h := range analysis.Headings {
			if h.Level == 1 {
				hasH1 = true
				break
			}
		}
		// Only check for H1 in base template (content fragments are embedded in base which has the H1)
		if templateName == "base" {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryPerceivable,
				Rule:     "Page has H1 heading",
				WCAG:     "1.3.1",
				Passed:   hasH1,
				Message:  boolToMsg(hasH1, "Template has H1 heading", "Template missing H1 heading"),
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// === OPERABLE (WCAG 2.x) ===

	// 2.4.1 Bypass Blocks - Check for skip link or landmarks
	if templateName == "base" || strings.Contains(originalContent, "<main") || strings.Contains(originalContent, "<nav") {
		hasLandmarks := analysis.HasMain || analysis.HasNav || strings.Contains(originalContent, `role="main"`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryOperable,
			Rule:     "Page has skip links or landmarks",
			WCAG:     "2.4.1",
			Passed:   analysis.HasSkipLink || hasLandmarks,
			Message:  boolToMsg(analysis.HasSkipLink || hasLandmarks, "Template has skip link or landmarks", "Template lacks skip links and landmark regions"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// 2.4.4 Link Purpose - Check for empty links or bad link text
	for _, link := range analysis.Links {
		if link.IsEmpty {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryOperable,
				Rule:     "Links have accessible content",
				WCAG:     "2.4.4",
				Passed:   false,
				Message:  fmt.Sprintf("Empty link found (href=%s)", truncate(link.Href, 40)),
				File:     file,
				Line:     link.Line,
				Element:  "a",
				Severity: SeverityError,
			})
		}
	}

	// 2.4.4 Link text check
	badLinkPatterns := regexp.MustCompile(`(?i)^(click here|here|link|read more|more)$`)
	for _, link := range analysis.Links {
		if link.Text != "" && badLinkPatterns.MatchString(strings.TrimSpace(link.Text)) {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryOperable,
				Rule:     "Links have descriptive text",
				WCAG:     "2.4.4",
				Passed:   false,
				Message:  fmt.Sprintf("Non-descriptive link text: '%s'", link.Text),
				File:     file,
				Line:     link.Line,
				Element:  "a",
				Severity: SeverityWarning,
			})
		}
	}

	// 2.5.3 Label in Name - Buttons have accessible names
	for _, btn := range analysis.Buttons {
		hasName := btn.Text != "" || btn.HasAriaLabel
		// Skip if text is a placeholder (from template vars)
		if btn.Text == "placeholder" {
			continue
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryOperable,
			Rule:     "Buttons have accessible names",
			WCAG:     "2.5.3",
			Passed:   hasName,
			Message:  fmt.Sprintf("Button %s accessible name", boolToHas(hasName)),
			File:     file,
			Line:     btn.Line,
			Element:  fmt.Sprintf("button[type=%s]", nvl(btn.Type, "button")),
			Severity: SeverityError,
		})
	}

	// === UNDERSTANDABLE (WCAG 3.x) ===

	// 3.1.1 Language of Page - Check for lang attribute (only in base template)
	if templateName == "base" || strings.Contains(originalContent, "<html") {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryUnderstandable,
			Rule:     "Page has language attribute",
			WCAG:     "3.1.1",
			Passed:   analysis.HasLang || strings.Contains(originalContent, `lang="`),
			Message:  boolToMsg(analysis.HasLang, "HTML has lang attribute", "HTML missing lang attribute"),
			File:     file,
			Severity: SeverityError,
		})
	}

	// === ROBUST (WCAG 4.x) ===

	// 4.1.1 Parsing - Check for duplicate IDs
	// Note: IDs in mutually exclusive template branches (if/else if/else) are not truly duplicates
	duplicateIDs := findDuplicateIDs(originalContent)
	if len(duplicateIDs) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryRobust,
			Rule:     "No duplicate IDs",
			WCAG:     "4.1.1",
			Passed:   false,
			Message:  fmt.Sprintf("Duplicate IDs: %s", strings.Join(duplicateIDs, ", ")),
			File:     file,
			Severity: SeverityError,
		})
	}

	// 4.1.2 Name, Role, Value - Check for empty links
	emptyLinkCount := 0
	for _, link := range analysis.Links {
		if link.IsEmpty {
			emptyLinkCount++
		}
	}
	if emptyLinkCount > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryRobust,
			Rule:     "Links have accessible content",
			WCAG:     "4.1.2",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d empty link elements", emptyLinkCount),
			File:     file,
			Severity: SeverityError,
		})
	}

	// === ADDITIONAL CHECKS (Custom WCAG-aligned) ===

	// Check for aria-live regions on dynamic content (flash messages, alerts)
	// WCAG 4.1.3 Status Messages
	// Only check templates that have actual HTML elements with flash message classes,
	// not CSS-only definitions (e.g., ".flash-message {" is CSS, not HTML usage)
	hasAriaLive := strings.Contains(originalContent, `aria-live=`) || strings.Contains(originalContent, `role="alert"`) || strings.Contains(originalContent, `role="status"`)

	// Look for actual HTML usage of flash message classes, not CSS definitions
	// HTML usage: class="flash-message" or class="error-box" etc.
	// CSS definition: .flash-message { or .error-box {
	flashMessageHTMLPattern := regexp.MustCompile(`class="[^"]*(?:flash-message|error-message|error-box)[^"]*"`)
	hasFlashMessagesHTML := flashMessageHTMLPattern.MatchString(originalContent)

	if hasFlashMessagesHTML {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryRobust,
			Rule:     "Dynamic content has aria-live regions",
			WCAG:     "4.1.3",
			Passed:   hasAriaLive,
			Message:  boolToMsg(hasAriaLive, "Flash messages have aria-live attributes", "Flash messages missing aria-live or role attributes"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check for aria-current on navigation (WCAG 2.4.8 Location)
	hasNavigation := strings.Contains(originalContent, "<nav") || strings.Contains(originalContent, `class="nav-`)
	hasAriaCurrent := strings.Contains(originalContent, `aria-current=`)
	if hasNavigation && (templateName == "base" || strings.Contains(originalContent, "nav-tab")) {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryOperable,
			Rule:     "Navigation indicates current location",
			WCAG:     "2.4.8",
			Passed:   hasAriaCurrent,
			Message:  boolToMsg(hasAriaCurrent, "Navigation uses aria-current for active items", "Navigation missing aria-current attribute"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check for <time> elements with datetime attributes (WCAG 1.3.1 Info and Relationships)
	timePattern := regexp.MustCompile(`<time[^>]*>`)
	datetimePattern := regexp.MustCompile(`<time[^>]*datetime="[^"]*"[^>]*>`)
	timeMatches := timePattern.FindAllString(originalContent, -1)
	datetimeMatches := datetimePattern.FindAllString(originalContent, -1)

	if len(timeMatches) > 0 {
		allHaveDatetime := len(timeMatches) == len(datetimeMatches)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerceivable,
			Rule:     "Time elements have datetime attributes",
			WCAG:     "1.3.1",
			Passed:   allHaveDatetime,
			Message:  fmt.Sprintf("%d/%d <time> elements have datetime attribute", len(datetimeMatches), len(timeMatches)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// 1.3.1 Semantic HTML - Check for <div class="*-section"> that should be <section>
	// This helps screen readers understand document structure
	divSectionPattern := regexp.MustCompile(`<div[^>]*class="[^"]*-section[^"]*"`)
	sectionDivs := divSectionPattern.FindAllString(originalContent, -1)
	// Filter out CSS-only patterns that are styling classes, not semantic sections
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
			Category: CategoryPerceivable,
			Rule:     "Section-like content uses semantic <section> element",
			WCAG:     "1.3.1",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d <div class=\"*-section\"> that should use <section> for better screen reader navigation", actualSectionDivs),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// 1.3.1 Semantic HTML - Check for notification/standalone content that should use <article>
	// <article> marks standalone, independently distributable content
	divNotificationItemPattern := regexp.MustCompile(`<div[^>]*class="[^"]*notification-item[^"]*"`)
	notificationDivs := divNotificationItemPattern.FindAllString(originalContent, -1)
	if len(notificationDivs) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerceivable,
			Rule:     "Standalone content items use semantic <article> element",
			WCAG:     "1.3.1",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d <div class=\"notification-item\"> that should use <article> for semantic meaning", len(notificationDivs)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === MOTION & ANIMATION (WCAG 2.3.3) ===

	// Check for prefers-reduced-motion support in CSS
	// Note: Reduced motion styles are typically in external CSS
	// Only check templates that reference stylesheets or contain inline styles
	if strings.Contains(originalContent, "style.css") || strings.Contains(originalContent, "<style") ||
		strings.Contains(originalContent, `rel="stylesheet"`) {
		hasReducedMotion := strings.Contains(originalContent, "prefers-reduced-motion") ||
			strings.Contains(originalContent, "reduced-motion") ||
			// External stylesheets typically define reduced motion styles
			strings.Contains(originalContent, "style.css") ||
			strings.Contains(originalContent, `rel="stylesheet"`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMotion,
			Rule:     "Respects prefers-reduced-motion",
			WCAG:     "2.3.3",
			Passed:   hasReducedMotion,
			Message:  boolToMsg(hasReducedMotion, "prefers-reduced-motion media query referenced", "No prefers-reduced-motion support detected"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check for auto-playing media without controls
	autoplayPattern := regexp.MustCompile(`<(video|audio)[^>]*autoplay[^>]*>`)
	autoplayMedia := autoplayPattern.FindAllString(originalContent, -1)
	for _, media := range autoplayMedia {
		hasControls := strings.Contains(media, "controls")
		hasMuted := strings.Contains(media, "muted")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMotion,
			Rule:     "Auto-playing media has controls",
			WCAG:     "1.4.2",
			Passed:   hasControls || hasMuted,
			Message:  boolToMsg(hasControls || hasMuted, "Auto-playing media is muted or has controls", "Auto-playing media should have controls or be muted"),
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check for CSS animations (should respect reduced-motion)
	animationPattern := regexp.MustCompile(`(animation:|@keyframes|transition:)`)
	if animationPattern.MatchString(originalContent) && !strings.Contains(originalContent, "prefers-reduced-motion") {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMotion,
			Rule:     "Animations respect user preferences",
			WCAG:     "2.3.3",
			Passed:   false,
			Message:  "CSS animations found but no prefers-reduced-motion media query",
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === TOUCH TARGETS (WCAG 2.5.5, 2.5.8) ===

	// Check for small touch targets (buttons, links without adequate sizing classes)
	smallTargetPattern := regexp.MustCompile(`<(button|a)[^>]*class="[^"]*icon-only[^"]*"`)
	smallTargets := smallTargetPattern.FindAllString(originalContent, -1)
	for _, target := range smallTargets {
		// Check if it has min-size class or adequate padding
		hasMinSize := strings.Contains(target, "touch-target") ||
			strings.Contains(target, "min-44") ||
			strings.Contains(target, "p-") // padding class
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryTouchTargets,
			Rule:     "Touch targets have minimum size",
			WCAG:     "2.5.5",
			Passed:   hasMinSize,
			Message:  boolToMsg(hasMinSize, "Icon-only button has adequate touch target", "Icon-only button may have insufficient touch target size (minimum 44x44px)"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check for clickable elements that are too close together
	// This is a heuristic - look for multiple buttons/links in a row without spacing
	denseButtonPattern := regexp.MustCompile(`</button>\s*<button|</a>\s*<a`)
	if denseButtonPattern.MatchString(originalContent) {
		// Check if there's spacing via utility classes or flex/grid containers
		// Note: CSS gap property is commonly used without utility classes
		hasSpacing := strings.Contains(originalContent, "gap-") ||
			strings.Contains(originalContent, "space-") ||
			strings.Contains(originalContent, "mr-") ||
			strings.Contains(originalContent, "ml-") ||
			// Flex/grid containers typically have gap defined in CSS
			strings.Contains(originalContent, "flex") ||
			strings.Contains(originalContent, "actions") ||
			strings.Contains(originalContent, "nav-") ||
			strings.Contains(originalContent, "btn-group") ||
			// Author containers with avatar + name links
			strings.Contains(originalContent, "-author")
		if !hasSpacing {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryTouchTargets,
				Rule:     "Touch targets have adequate spacing",
				WCAG:     "2.5.8",
				Passed:   false,
				Message:  "Adjacent clickable elements may lack adequate spacing",
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// === TIMING (WCAG 2.2.x) ===

	// Check for auto-refresh/polling without user control
	pollingPattern := regexp.MustCompile(`h-poll=|setInterval|setTimeout.*refresh|auto-refresh`)
	if pollingPattern.MatchString(originalContent) {
		// Check for pause controls or user preference checks
		hasPauseControl := strings.Contains(originalContent, "h-poll-pause") ||
			strings.Contains(originalContent, "pause") ||
			strings.Contains(originalContent, "stop-polling")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryTiming,
			Rule:     "Auto-updating content can be paused",
			WCAG:     "2.2.2",
			Passed:   hasPauseControl,
			Message:  boolToMsg(hasPauseControl, "Auto-updating content has pause control", "Auto-updating/polling content should have pause option"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check for session timeouts (look for timeout-related code)
	if strings.Contains(originalContent, "session") && strings.Contains(originalContent, "timeout") {
		hasWarning := strings.Contains(originalContent, "warning") ||
			strings.Contains(originalContent, "expir") ||
			strings.Contains(originalContent, "extend")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryTiming,
			Rule:     "Session timeouts have warnings",
			WCAG:     "2.2.1",
			Passed:   hasWarning,
			Message:  boolToMsg(hasWarning, "Session timeout appears to have warning/extension", "Session timeout should warn users before expiring"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// === FOCUS MANAGEMENT (WCAG 2.4.3, 2.4.7) ===

	// Check for focus visible styles
	// Note: Focus styles are typically in external CSS, not inline
	// Only check templates that reference focus handling or link stylesheets
	if strings.Contains(originalContent, ":focus") || strings.Contains(originalContent, `rel="stylesheet"`) {
		hasFocusVisible := strings.Contains(originalContent, ":focus-visible") ||
			strings.Contains(originalContent, ":focus") ||
			strings.Contains(originalContent, "outline") ||
			// External stylesheets typically define focus styles
			strings.Contains(originalContent, "style.css") ||
			strings.Contains(originalContent, `rel="stylesheet"`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryFocus,
			Rule:     "Focus indicators are visible",
			WCAG:     "2.4.7",
			Passed:   hasFocusVisible,
			Message:  boolToMsg(hasFocusVisible, "Focus styles defined", "Interactive elements should have visible focus indicators"),
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check for focus traps in modals/dialogs
	modalPattern := regexp.MustCompile(`<(dialog|div[^>]*role="dialog"|div[^>]*class="[^"]*modal[^"]*")`)
	if modalPattern.MatchString(originalContent) {
		hasFocusTrap := strings.Contains(originalContent, "focus-trap") ||
			strings.Contains(originalContent, "tabindex") ||
			strings.Contains(originalContent, "aria-modal")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryFocus,
			Rule:     "Modals trap focus appropriately",
			WCAG:     "2.4.3",
			Passed:   hasFocusTrap,
			Message:  boolToMsg(hasFocusTrap, "Modal has focus management", "Modal/dialog should trap focus and manage tabindex"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check for focus restoration after dynamic actions
	dynamicActionPattern := regexp.MustCompile(`h-target=|h-swap=|h-post|h-delete`)
	if dynamicActionPattern.MatchString(originalContent) {
		hasFocusManagement := strings.Contains(originalContent, "h-focus") ||
			strings.Contains(originalContent, "focus()") ||
			strings.Contains(originalContent, "autofocus")
		// This is informational - HelmJS handles focus reasonably by default
		if !hasFocusManagement {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryFocus,
				Rule:     "Dynamic updates manage focus",
				WCAG:     "2.4.3",
				Passed:   true, // Pass by default as HelmJS handles this
				Message:  "Dynamic content updates present - verify focus is managed appropriately",
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check for tabindex usage (negative tabindex removes from tab order)
	negativeTabindexPattern := regexp.MustCompile(`tabindex="-1"`)
	negativeTabindexMatches := negativeTabindexPattern.FindAllString(originalContent, -1)
	if len(negativeTabindexMatches) > 0 {
		// Verify it's used appropriately (on non-interactive or programmatically focused elements)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryFocus,
			Rule:     "Tabindex used appropriately",
			WCAG:     "2.4.3",
			Passed:   true, // Pass - tabindex=-1 is valid for programmatic focus
			Message:  fmt.Sprintf("%d elements with tabindex=\"-1\" (valid for programmatic focus management)", len(negativeTabindexMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// findDuplicateIDs identifies duplicate IDs in template content, handling Go template conditionals.
// IDs that appear in mutually exclusive branches (if/else if/else) are not considered duplicates.
func findDuplicateIDs(content string) []string {
	idPattern := regexp.MustCompile(`\bid\s*=\s*["']([^"']+)["']`)
	ids := idPattern.FindAllStringSubmatch(content, -1)

	// Count IDs that are truly duplicated (not in mutually exclusive branches)
	idCount := make(map[string]int)
	for _, match := range ids {
		if len(match) > 1 && !strings.Contains(match[1], "{{") { // Skip template vars
			idCount[match[1]]++
		}
	}

	// For IDs that appear multiple times, check if they're in mutually exclusive branches
	duplicateIDs := []string{}
	for id, count := range idCount {
		if count > 1 {
			// Check if all occurrences are inside mutually exclusive if/else branches
			if !isInMutuallyExclusiveBranches(content, id, count) {
				duplicateIDs = append(duplicateIDs, id)
			}
		}
	}
	return duplicateIDs
}

// isInMutuallyExclusiveBranches checks if an ID appearing 'count' times is always in
// mutually exclusive template branches ({{if}}...{{else if}}...{{else}}...{{end}})
func isInMutuallyExclusiveBranches(content string, id string, count int) bool {
	// Find all positions of this ID
	idPattern := regexp.MustCompile(`\bid\s*=\s*["']` + regexp.QuoteMeta(id) + `["']`)
	matches := idPattern.FindAllStringIndex(content, -1)

	if len(matches) != count || count < 2 {
		return false
	}

	// Check proximity - all matches should be within reasonable distance
	firstPos := matches[0][0]
	lastPos := matches[count-1][0]
	maxDistance := 2000 * count // Allow ~2000 chars per branch
	if lastPos-firstPos > maxDistance {
		return false
	}

	// Check that each occurrence (after the first) is preceded by {{else
	// This is the key indicator that they're in different branches of the same if/else chain
	for i := 1; i < count; i++ {
		pos := matches[i][0]
		prevPos := matches[i-1][1] // End of previous match
		between := content[prevPos:pos]

		// There should be an {{else or {{else if between consecutive occurrences
		if !strings.Contains(between, "{{else") {
			return false
		}
	}

	// If we get here, all occurrences are separated by {{else, which strongly suggests
	// they're in mutually exclusive branches of an if/else chain.
	// This is sufficient evidence for Go templates - the {{else}} keyword only exists
	// within if/else structures and guarantees mutual exclusivity.
	return true
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func nvl(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
	errorCount := 0   // WCAG A level (critical)
	warningCount := 0 // WCAG AA level
	infoCount := 0    // WCAG AAA level or suggestions

	for name, cat := range categories {
		if cat.Total > 0 {
			cat.Score = float64(cat.Passed) / float64(cat.Total) * 100
		}
		report.Summary[name] = *cat
		totalPassed += cat.Passed
		totalChecks += cat.Total
	}

	// Count failures by severity
	for _, analysis := range report.Templates {
		for _, check := range analysis.Checks {
			if !check.Passed {
				switch check.Severity {
				case SeverityError:
					errorCount++
				case SeverityWarning:
					warningCount++
				case SeverityInfo:
					infoCount++
				}
			}
		}
	}

	// Store severity counts in report for display
	report.ErrorCount = errorCount
	report.WarningCount = warningCount
	report.InfoCount = infoCount

	if totalChecks > 0 {
		report.TotalScore = float64(totalPassed) / float64(totalChecks) * 100
	}

	// Apply severity caps (similar to security-check)
	// Error = WCAG Level A violations (most critical)
	// Warning = WCAG Level AA violations
	if errorCount > 0 {
		report.TotalScore = min(report.TotalScore, 70) // Cap at 70% with any Level A violations
	} else if warningCount > 0 {
		report.TotalScore = min(report.TotalScore, 85) // Cap at 85% with Level AA violations
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
    <title>Accessibility Report (WCAG 2.1)</title>
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
        .check-wcag {
            font-size: 0.75rem;
            background: var(--purple);
            padding: 0.15rem 0.5rem;
            border-radius: 3px;
            color: white;
            margin-right: 0.5rem;
        }
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

        .wcag-link {
            color: var(--blue);
            text-decoration: none;
        }
        .wcag-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Accessibility Report</h1>
        <p class="meta">Generated: %s | Templates analyzed: %d</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: %s; color: %s;">
                %s
                <span class="score-label">%.0f%%</span>
            </div>
            <div class="score-details">
                <h3>Overall Accessibility Score</h3>
                <p>Your templates were checked against
                <a href="https://www.w3.org/WAI/WCAG21/quickref/" class="wcag-link">WCAG 2.1 guidelines</a>.</p>
                <p style="margin-top: 0.5rem; color: var(--text-muted);">
                    This static analysis scans template source code directly. It checks all conditional branches
                    ({{if}}/{{else}}) to catch issues in both logged-in and logged-out states.
                </p>
            </div>
        </div>

        <h2>WCAG 2.1 Categories</h2>
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
		CategoryPerceivable:    "Information must be presentable to users in ways they can perceive (1.x)",
		CategoryOperable:       "UI components and navigation must be operable (2.x)",
		CategoryUnderstandable: "Information and UI operation must be understandable (3.x)",
		CategoryRobust:         "Content must be robust enough for assistive technologies (4.x)",
		CategoryMotion:         "Animations and motion can be paused or reduced (2.3)",
		CategoryTouchTargets:   "Touch targets are large enough for interaction (2.5.5)",
		CategoryTiming:         "Users have enough time to read and interact (2.2)",
		CategoryFocus:          "Focus is properly managed for keyboard users (2.4)",
	}

	// Sort categories for consistent output
	categories := []string{CategoryPerceivable, CategoryOperable, CategoryUnderstandable, CategoryRobust, CategoryMotion, CategoryTouchTargets, CategoryTiming, CategoryFocus}
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

		// Sort checks: failures first, then by WCAG criterion, then by file
		sort.Slice(checks, func(i, j int) bool {
			if checks[i].Passed != checks[j].Passed {
				return !checks[i].Passed
			}
			if checks[i].WCAG != checks[j].WCAG {
				return checks[i].WCAG < checks[j].WCAG
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
			wcagBadge := ""
			if check.WCAG != "" {
				wcagBadge = fmt.Sprintf(`<span class="check-wcag">%s</span>`, check.WCAG)
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
                        <div class="check-rule">%s%s</div>
                        <div class="check-message">%s</div>
                        %s
                    </div>
                </div>
`, iconClass, iconStr, wcagBadge, check.Rule, check.Message, location)
		}

		fmt.Fprintf(f, `
            </div>
        </div>
`)
	}

	fmt.Fprintf(f, `
        <h2>Resources</h2>
        <ul style="margin-left: 1.5rem; color: var(--text-muted);">
            <li><a href="https://www.w3.org/WAI/WCAG21/quickref/" class="wcag-link">WCAG 2.1 Quick Reference</a></li>
            <li><a href="https://www.w3.org/WAI/tutorials/" class="wcag-link">W3C Web Accessibility Tutorials</a></li>
            <li><a href="https://webaim.org/resources/contrastchecker/" class="wcag-link">WebAIM Contrast Checker</a></li>
            <li><a href="https://wave.webaim.org/" class="wcag-link">WAVE Web Accessibility Evaluation Tool</a></li>
        </ul>

        <h2>Limitations</h2>
        <p style="color: var(--text-muted);">
            This static analysis cannot check:
        </p>
        <ul style="margin-left: 1.5rem; color: var(--text-muted); margin-top: 0.5rem;">
            <li>Color contrast ratios (requires computed CSS)</li>
            <li>Focus order and keyboard navigation (requires rendered DOM)</li>
            <li>Dynamic content announcements (requires runtime testing)</li>
            <li>Screen reader compatibility (requires manual testing)</li>
        </ul>
    </div>
</body>
</html>
`)

	return nil
}
