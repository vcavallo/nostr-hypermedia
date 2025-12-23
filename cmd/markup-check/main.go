// Static Markup Validator
// Analyzes Go template files for valid HTML and CSS without requiring a running server
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

// Validation categories
const (
	CategoryHTML          = "HTML"
	CategoryCSS           = "CSS"
	CategorySemantic      = "Semantic"
	CategoryBestPractices = "Best Practices"
	CategoryDeadCode      = "Dead Code"
	CategoryPerformance   = "Performance"
	CategorySEO           = "SEO & Meta"
	CategoryMobile        = "Mobile"
)

// Severity levels
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// CheckResult represents a single validation check result
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
	File         string
	TemplateName string
	Checks       []CheckResult
	HTMLErrors   []HTMLError
	CSSIssues    []CSSIssue
}

// HTMLError represents an HTML parsing or structure error
type HTMLError struct {
	Message string
	Line    int
	Context string
}

// CSSIssue represents a CSS validation issue
type CSSIssue struct {
	Rule     string
	Property string
	Value    string
	Message  string
	Line     int
	Severity string
}

// Report contains the full validation report
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
)

func main() {
	flag.StringVar(&projectPath, "path", ".", "Path to project root (e.g., ../../. when running from cmd/markup-check)")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.StringVar(&outputFile, "output", "markup-report.html", "Output file")
	flag.Parse()

	fmt.Printf("Markup Validator (HTML/CSS)\n")
	fmt.Printf("===================================\n")
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

	// Analyze external CSS file (static/style.css)
	analyzeExternalCSS(report, projectPath)

	// Run dead code analysis (cross-template checks)
	runDeadCodeAnalysis(report, projectPath)

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
	categories := []string{CategoryHTML, CategoryCSS, CategorySemantic, CategoryBestPractices, CategoryDeadCode, CategoryPerformance, CategorySEO, CategoryMobile}
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

	// Special handling for base.go: combine all fragments
	isBaseFile := strings.HasSuffix(filePath, "base.go")
	if isBaseFile {
		var combinedHTML strings.Builder
		for _, match := range matches {
			if len(match) >= 4 {
				templateStr := fileContent[match[2]:match[3]]
				if strings.Contains(templateStr, "<") || strings.Contains(templateStr, "{") {
					combinedHTML.WriteString(templateStr)
				}
			}
		}

		combined := combinedHTML.String()
		if combined != "" {
			analysis := analyzeTemplate(combined, filePath, "base")
			if len(analysis.Checks) > 0 || len(analysis.HTMLErrors) > 0 || len(analysis.CSSIssues) > 0 {
				analyses = append(analyses, analysis)
			}
		}
		return analyses, nil
	}

	// Standard handling for other template files
	for _, match := range matches {
		if len(match) >= 4 {
			templateStr := fileContent[match[2]:match[3]]

			if !strings.Contains(templateStr, "<") {
				continue
			}

			templateName := extractTemplateName(templateStr)
			if templateName == "" {
				templateName = filepath.Base(filePath)
			}

			analysis := analyzeTemplate(templateStr, filePath, templateName)
			if len(analysis.Checks) > 0 || len(analysis.HTMLErrors) > 0 || len(analysis.CSSIssues) > 0 {
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
		HTMLErrors:   []HTMLError{},
		CSSIssues:    []CSSIssue{},
	}

	// Strip Go template syntax for HTML parsing
	cleanHTML := stripGoTemplates(content)

	// Run HTML validation checks
	runHTMLChecks(&analysis, content, cleanHTML)

	// Run CSS validation checks (look for <style> blocks)
	runCSSChecks(&analysis, content)

	// Run semantic HTML checks
	runSemanticChecks(&analysis, content, cleanHTML)

	// Run best practices checks
	runBestPracticesChecks(&analysis, content, cleanHTML)

	// Run performance checks
	runPerformanceChecks(&analysis, content)

	// Run SEO/meta checks
	runSEOChecks(&analysis, content)

	// Run mobile checks
	runMobileChecks(&analysis, content)

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
		{regexp.MustCompile(`\{\{define[^}]*\}\}`), ""},
		{regexp.MustCompile(`\{\{[^}]+\}\}`), "placeholder"},
	}

	result := content
	for _, p := range patterns {
		result = p.pattern.ReplaceAllString(result, p.replace)
	}

	return result
}

// === HTML VALIDATION ===

func runHTMLChecks(analysis *TemplateAnalysis, original string, cleaned string) {
	file := analysis.File

	// Check 1: Valid HTML structure (can be parsed)
	doc, err := html.Parse(strings.NewReader(cleaned))
	if err != nil {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryHTML,
			Rule:     "Valid HTML structure",
			Passed:   false,
			Message:  fmt.Sprintf("HTML parsing error: %v", err),
			File:     file,
			Severity: SeverityError,
		})
	} else {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryHTML,
			Rule:     "Valid HTML structure",
			Passed:   true,
			Message:  "HTML parses successfully",
			File:     file,
			Severity: SeverityInfo,
		})

		// Extract and check elements
		checkHTMLElements(doc, analysis, original)
	}

	// Check 2: Properly closed tags
	checkUnclosedTags(analysis, original)

	// Check 3: No deprecated tags
	checkDeprecatedTags(analysis, original)

	// Check 4: Proper attribute quoting
	checkAttributeQuoting(analysis, original)

	// Check 5: No duplicate attributes
	checkDuplicateAttributes(analysis, original)

	// Check 6: Valid boolean attributes
	checkBooleanAttributes(analysis, original)
}

func checkHTMLElements(doc *html.Node, analysis *TemplateAnalysis, original string) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Check for empty href/src attributes
			for _, attr := range n.Attr {
				if (attr.Key == "href" || attr.Key == "src") && attr.Val == "" {
					analysis.Checks = append(analysis.Checks, CheckResult{
						Category: CategoryHTML,
						Rule:     "Non-empty URL attributes",
						Passed:   false,
						Message:  fmt.Sprintf("Empty %s attribute on <%s>", attr.Key, n.Data),
						File:     analysis.File,
						Element:  n.Data,
						Severity: SeverityWarning,
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

func checkUnclosedTags(analysis *TemplateAnalysis, content string) {
	// Self-closing tags that don't need closing
	selfClosing := map[string]bool{
		"area": true, "base": true, "br": true, "col": true,
		"embed": true, "hr": true, "img": true, "input": true,
		"link": true, "meta": true, "param": true, "source": true,
		"track": true, "wbr": true,
	}

	// Find all opening tags
	openPattern := regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)[^>]*>`)
	closePattern := regexp.MustCompile(`</([a-zA-Z][a-zA-Z0-9]*)>`)

	opens := openPattern.FindAllStringSubmatch(content, -1)
	closes := closePattern.FindAllStringSubmatch(content, -1)

	openCount := make(map[string]int)
	closeCount := make(map[string]int)

	for _, match := range opens {
		tag := strings.ToLower(match[1])
		if !selfClosing[tag] {
			openCount[tag]++
		}
	}

	for _, match := range closes {
		tag := strings.ToLower(match[1])
		closeCount[tag]++
	}

	// Check for mismatches (only report significant ones)
	for tag, count := range openCount {
		closeC := closeCount[tag]
		if count > closeC+5 { // Allow some tolerance for template conditionals
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryHTML,
				Rule:     "Properly closed tags",
				Passed:   false,
				Message:  fmt.Sprintf("Tag <%s> opened %d times but closed %d times", tag, count, closeC),
				File:     analysis.File,
				Element:  tag,
				Severity: SeverityWarning,
			})
		}
	}
}

func checkDeprecatedTags(analysis *TemplateAnalysis, content string) {
	deprecated := []string{
		"acronym", "applet", "basefont", "big", "blink", "center",
		"dir", "font", "frame", "frameset", "isindex", "keygen",
		"listing", "marquee", "menuitem", "multicol", "nextid",
		"nobr", "noembed", "noframes", "plaintext", "rb", "rtc",
		"spacer", "strike", "tt", "xmp",
	}

	for _, tag := range deprecated {
		pattern := regexp.MustCompile(fmt.Sprintf(`(?i)<%s[>\s]`, tag))
		if pattern.MatchString(content) {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryHTML,
				Rule:     "No deprecated tags",
				Passed:   false,
				Message:  fmt.Sprintf("Deprecated tag <%s> found", tag),
				File:     analysis.File,
				Element:  tag,
				Severity: SeverityWarning,
			})
		}
	}
}

func checkAttributeQuoting(analysis *TemplateAnalysis, content string) {
	// First, strip out all quoted attribute values to avoid false positives
	// from patterns like content="width=device-width, initial-scale=1.0"
	stripQuotedAttrs := regexp.MustCompile(`="[^"]*"`)
	strippedContent := stripQuotedAttrs.ReplaceAllString(content, `=""`)
	stripSingleQuotedAttrs := regexp.MustCompile(`='[^']*'`)
	strippedContent = stripSingleQuotedAttrs.ReplaceAllString(strippedContent, `=''`)

	// Now look for unquoted attribute values (excluding template variables)
	unquotedPattern := regexp.MustCompile(`\s([a-zA-Z-]+)=([^"'\s>][^\s>]*)[\s>]`)
	matches := unquotedPattern.FindAllStringSubmatch(strippedContent, -1)

	for _, match := range matches {
		attr := match[1]
		value := match[2]
		// Skip template placeholders
		if strings.Contains(value, "{{") || strings.Contains(value, "placeholder") {
			continue
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryHTML,
			Rule:     "Quoted attribute values",
			Passed:   false,
			Message:  fmt.Sprintf("Unquoted attribute value: %s=%s", attr, value),
			File:     analysis.File,
			Severity: SeverityWarning,
		})
	}
}

func checkDuplicateAttributes(analysis *TemplateAnalysis, content string) {
	// Find tags with their attributes
	tagPattern := regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)((?:\s+[a-zA-Z-]+(?:="[^"]*"|='[^']*'|=[^\s>]*)?)+)[^>]*>`)
	attrPattern := regexp.MustCompile(`\s([a-zA-Z-]+)(?:=|[\s>])`)

	matches := tagPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		tag := match[1]
		attrs := match[2]

		attrMatches := attrPattern.FindAllStringSubmatch(attrs, -1)
		seen := make(map[string]bool)

		for _, attrMatch := range attrMatches {
			attr := strings.ToLower(attrMatch[1])
			if seen[attr] {
				analysis.Checks = append(analysis.Checks, CheckResult{
					Category: CategoryHTML,
					Rule:     "No duplicate attributes",
					Passed:   false,
					Message:  fmt.Sprintf("Duplicate attribute '%s' on <%s>", attr, tag),
					File:     analysis.File,
					Element:  tag,
					Severity: SeverityError,
				})
			}
			seen[attr] = true
		}
	}
}

func checkBooleanAttributes(analysis *TemplateAnalysis, content string) {
	// Boolean attributes should not have values, or have empty/"true"/same-name values
	boolAttrs := []string{
		"async", "autofocus", "autoplay", "checked", "controls",
		"default", "defer", "disabled", "formnovalidate", "hidden",
		"ismap", "itemscope", "loop", "multiple", "muted", "nomodule",
		"novalidate", "open", "playsinline", "readonly", "required",
		"reversed", "selected", "truespeed",
	}

	for _, attr := range boolAttrs {
		// Pattern for boolean attribute with any value
		pattern := regexp.MustCompile(fmt.Sprintf(`(?i)\s%s="([^"]*)"`, attr))
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			value := match[1]
			// Skip valid values: empty, "true", or same-name
			if value == "" || value == "true" || strings.EqualFold(value, attr) {
				continue
			}
			// Skip template expressions
			if strings.Contains(value, "{{") {
				continue
			}
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryHTML,
				Rule:     "Valid boolean attributes",
				Passed:   false,
				Message:  fmt.Sprintf("Boolean attribute '%s' has non-standard value '%s'", attr, value),
				File:     analysis.File,
				Severity: SeverityInfo,
			})
		}
	}
}

// === CSS VALIDATION ===

func runCSSChecks(analysis *TemplateAnalysis, content string) {
	// Extract CSS from <style> blocks
	stylePattern := regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)
	matches := stylePattern.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return
	}

	for _, match := range matches {
		css := match[1]
		validateCSS(analysis, css)
	}
}

func validateCSS(analysis *TemplateAnalysis, css string) {
	// Check 1: Balanced braces
	openBraces := strings.Count(css, "{")
	closeBraces := strings.Count(css, "}")
	if openBraces != closeBraces {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Balanced braces",
			Passed:   false,
			Message:  fmt.Sprintf("Unbalanced braces: %d open, %d close", openBraces, closeBraces),
			File:     analysis.File,
			Severity: SeverityError,
		})
	} else {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Balanced braces",
			Passed:   true,
			Message:  "CSS braces are balanced",
			File:     analysis.File,
			Severity: SeverityInfo,
		})
	}

	// Check 2: Valid property-value pairs (basic check)
	checkCSSProperties(analysis, css)

	// Check 3: No vendor prefixes without standard property
	checkVendorPrefixes(analysis, css)

	// Check 4: CSS variables used consistently
	checkCSSVariables(analysis, css)

	// Check 5: No !important overuse
	checkImportantUsage(analysis, css)

	// Check 6: Valid color values
	checkColorValues(analysis, css)

	// Check 7: Valid units
	checkCSSUnits(analysis, css)
}

func checkCSSProperties(analysis *TemplateAnalysis, css string) {
	// Extract property-value pairs
	propPattern := regexp.MustCompile(`([a-zA-Z-]+)\s*:\s*([^;{}]+);`)
	matches := propPattern.FindAllStringSubmatch(css, -1)

	// Common CSS properties (not exhaustive, but covers most)
	validProperties := map[string]bool{
		"align-content": true, "align-items": true, "align-self": true,
		"all": true, "animation": true, "animation-delay": true,
		"animation-direction": true, "animation-duration": true,
		"animation-fill-mode": true, "animation-iteration-count": true,
		"animation-name": true, "animation-play-state": true,
		"animation-timing-function": true, "backface-visibility": true,
		"background": true, "background-attachment": true,
		"background-blend-mode": true, "background-clip": true,
		"background-color": true, "background-image": true,
		"background-origin": true, "background-position": true,
		"background-repeat": true, "background-size": true,
		"border": true, "border-bottom": true, "border-bottom-color": true,
		"border-bottom-left-radius": true, "border-bottom-right-radius": true,
		"border-bottom-style": true, "border-bottom-width": true,
		"border-collapse": true, "border-color": true, "border-image": true,
		"border-left": true, "border-left-color": true, "border-left-style": true,
		"border-left-width": true, "border-radius": true, "border-right": true,
		"border-right-color": true, "border-right-style": true,
		"border-right-width": true, "border-spacing": true, "border-style": true,
		"border-top": true, "border-top-color": true, "border-top-left-radius": true,
		"border-top-right-radius": true, "border-top-style": true,
		"border-top-width": true, "border-width": true, "bottom": true,
		"box-decoration-break": true, "box-shadow": true, "box-sizing": true,
		"break-after": true, "break-before": true, "break-inside": true,
		"caption-side": true, "caret-color": true, "clear": true, "clip": true,
		"clip-path": true, "color": true, "column-count": true, "column-fill": true,
		"column-gap": true, "column-rule": true, "column-rule-color": true,
		"column-rule-style": true, "column-rule-width": true, "column-span": true,
		"column-width": true, "columns": true, "content": true,
		"counter-increment": true, "counter-reset": true, "cursor": true,
		"direction": true, "display": true, "empty-cells": true, "filter": true,
		"flex": true, "flex-basis": true, "flex-direction": true, "flex-flow": true,
		"flex-grow": true, "flex-shrink": true, "flex-wrap": true, "float": true,
		"font": true, "font-family": true, "font-feature-settings": true,
		"font-kerning": true, "font-size": true, "font-size-adjust": true,
		"font-stretch": true, "font-style": true, "font-variant": true,
		"font-variant-caps": true, "font-weight": true, "gap": true, "grid": true,
		"grid-area": true, "grid-auto-columns": true, "grid-auto-flow": true,
		"grid-auto-rows": true, "grid-column": true, "grid-column-end": true,
		"grid-column-gap": true, "grid-column-start": true, "grid-gap": true,
		"grid-row": true, "grid-row-end": true, "grid-row-gap": true,
		"grid-row-start": true, "grid-template": true, "grid-template-areas": true,
		"grid-template-columns": true, "grid-template-rows": true, "height": true,
		"hyphens": true, "image-rendering": true, "isolation": true,
		"justify-content": true, "justify-items": true, "justify-self": true,
		"left": true, "letter-spacing": true, "line-height": true, "list-style": true,
		"list-style-image": true, "list-style-position": true, "list-style-type": true,
		"margin": true, "margin-bottom": true, "margin-left": true, "margin-right": true,
		"margin-top": true, "max-height": true, "max-width": true, "min-height": true,
		"min-width": true, "mix-blend-mode": true, "object-fit": true,
		"object-position": true, "opacity": true, "order": true, "orphans": true,
		"outline": true, "outline-color": true, "outline-offset": true,
		"outline-style": true, "outline-width": true, "overflow": true,
		"overflow-wrap": true, "overflow-x": true, "overflow-y": true,
		"padding": true, "padding-bottom": true, "padding-left": true,
		"padding-right": true, "padding-top": true, "page-break-after": true,
		"page-break-before": true, "page-break-inside": true, "perspective": true,
		"perspective-origin": true, "place-content": true, "place-items": true,
		"place-self": true, "pointer-events": true, "position": true, "quotes": true,
		"resize": true, "right": true, "row-gap": true, "scroll-behavior": true,
		"scroll-margin": true, "scroll-padding": true, "scroll-snap-align": true,
		"scroll-snap-stop": true, "scroll-snap-type": true, "shape-image-threshold": true,
		"shape-margin": true, "shape-outside": true, "tab-size": true,
		"table-layout": true, "text-align": true, "text-align-last": true,
		"text-decoration": true, "text-decoration-color": true,
		"text-decoration-line": true, "text-decoration-style": true,
		"text-indent": true, "text-justify": true, "text-overflow": true,
		"text-shadow": true, "text-transform": true, "top": true, "transform": true,
		"transform-origin": true, "transform-style": true, "transition": true,
		"transition-delay": true, "transition-duration": true,
		"transition-property": true, "transition-timing-function": true,
		"unicode-bidi": true, "user-select": true, "vertical-align": true,
		"visibility": true, "white-space": true, "widows": true, "width": true,
		"will-change": true, "word-break": true, "word-spacing": true,
		"word-wrap": true, "writing-mode": true, "z-index": true,
		// Vendor prefixed
		"-webkit-appearance": true, "-moz-appearance": true,
		"-webkit-font-smoothing": true, "-moz-osx-font-smoothing": true,
		"-webkit-overflow-scrolling": true, "-webkit-tap-highlight-color": true,
		"-webkit-text-size-adjust": true, "-webkit-line-clamp": true,
		"-webkit-box-orient": true,
		// Custom properties (CSS variables)
		"--": true,
	}

	unknownProps := make(map[string]bool)
	for _, match := range matches {
		prop := strings.TrimSpace(strings.ToLower(match[1]))
		// Skip CSS variables and vendor prefixes
		if strings.HasPrefix(prop, "--") || strings.HasPrefix(prop, "-webkit-") ||
			strings.HasPrefix(prop, "-moz-") || strings.HasPrefix(prop, "-ms-") ||
			strings.HasPrefix(prop, "-o-") {
			continue
		}
		if !validProperties[prop] {
			unknownProps[prop] = true
		}
	}

	if len(unknownProps) > 0 {
		props := make([]string, 0, len(unknownProps))
		for p := range unknownProps {
			props = append(props, p)
		}
		sort.Strings(props)
		// Only report first 5
		if len(props) > 5 {
			props = props[:5]
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Valid CSS properties",
			Passed:   false,
			Message:  fmt.Sprintf("Unknown properties: %s", strings.Join(props, ", ")),
			File:     analysis.File,
			Severity: SeverityWarning,
		})
	} else if len(matches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Valid CSS properties",
			Passed:   true,
			Message:  "All CSS properties recognized",
			File:     analysis.File,
			Severity: SeverityInfo,
		})
	}
}

func checkVendorPrefixes(analysis *TemplateAnalysis, css string) {
	// Look for vendor prefixes without standard property
	prefixPattern := regexp.MustCompile(`(-webkit-|-moz-|-ms-|-o-)([a-zA-Z-]+)\s*:`)
	matches := prefixPattern.FindAllStringSubmatch(css, -1)

	prefixedProps := make(map[string]bool)
	for _, match := range matches {
		prop := match[2]
		prefixedProps[prop] = true
	}

	// Check if standard versions exist
	for prop := range prefixedProps {
		standardPattern := regexp.MustCompile(fmt.Sprintf(`[^-]%s\s*:`, prop))
		if !standardPattern.MatchString(css) {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryCSS,
				Rule:     "Include standard property with vendor prefix",
				Passed:   false,
				Message:  fmt.Sprintf("Vendor-prefixed '%s' without standard property", prop),
				File:     analysis.File,
				Severity: SeverityInfo,
			})
		}
	}
}

func checkCSSVariables(analysis *TemplateAnalysis, css string) {
	// Find variable definitions
	defPattern := regexp.MustCompile(`--([a-zA-Z0-9-]+)\s*:`)
	defined := make(map[string]bool)
	matches := defPattern.FindAllStringSubmatch(css, -1)
	for _, match := range matches {
		defined[match[1]] = true
	}

	// Find variable usages
	usePattern := regexp.MustCompile(`var\(--([a-zA-Z0-9-]+)`)
	used := make(map[string]bool)
	useMatches := usePattern.FindAllStringSubmatch(css, -1)
	for _, match := range useMatches {
		used[match[1]] = true
	}

	// Check for undefined variables
	undefined := []string{}
	for varName := range used {
		if !defined[varName] {
			undefined = append(undefined, varName)
		}
	}

	if len(undefined) > 0 && len(undefined) <= 10 {
		// Allow some undefined (may be from :root in separate file)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "CSS variables defined before use",
			Passed:   true,
			Message:  fmt.Sprintf("%d CSS variables used, %d defined in file", len(used), len(defined)),
			File:     analysis.File,
			Severity: SeverityInfo,
		})
	} else if len(used) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "CSS variables defined before use",
			Passed:   true,
			Message:  fmt.Sprintf("%d CSS variables used correctly", len(used)),
			File:     analysis.File,
			Severity: SeverityInfo,
		})
	}
}

func checkImportantUsage(analysis *TemplateAnalysis, css string) {
	count := strings.Count(css, "!important")

	// Estimate total declarations
	propPattern := regexp.MustCompile(`[a-zA-Z-]+\s*:[^;{}]+;`)
	totalDecls := len(propPattern.FindAllString(css, -1))

	if totalDecls > 0 {
		percentage := float64(count) / float64(totalDecls) * 100
		if percentage > 10 {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryCSS,
				Rule:     "Minimal !important usage",
				Passed:   false,
				Message:  fmt.Sprintf("Overuse of !important: %d times (%.1f%% of declarations)", count, percentage),
				File:     analysis.File,
				Severity: SeverityWarning,
			})
		} else {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryCSS,
				Rule:     "Minimal !important usage",
				Passed:   true,
				Message:  fmt.Sprintf("!important used %d times (%.1f%% of declarations)", count, percentage),
				File:     analysis.File,
				Severity: SeverityInfo,
			})
		}
	}
}

func checkColorValues(analysis *TemplateAnalysis, css string) {
	// Check for invalid hex colors
	hexPattern := regexp.MustCompile(`#([0-9a-fA-F]+)`)
	matches := hexPattern.FindAllStringSubmatch(css, -1)

	invalidHex := []string{}
	for _, match := range matches {
		hex := match[1]
		// Valid: 3, 4, 6, or 8 characters
		if len(hex) != 3 && len(hex) != 4 && len(hex) != 6 && len(hex) != 8 {
			invalidHex = append(invalidHex, "#"+hex)
		}
	}

	if len(invalidHex) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Valid color values",
			Passed:   false,
			Message:  fmt.Sprintf("Invalid hex colors: %s", strings.Join(invalidHex[:min(3, len(invalidHex))], ", ")),
			File:     analysis.File,
			Severity: SeverityError,
		})
	}
}

func checkCSSUnits(analysis *TemplateAnalysis, css string) {
	// Check for zero values with units (should be unitless except for specific cases)
	zeroWithUnit := regexp.MustCompile(`:\s*0(px|em|rem|%|vh|vw|pt|cm|mm|in)[;\s}]`)
	matches := zeroWithUnit.FindAllString(css, -1)

	if len(matches) > 5 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Unitless zero values",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d zero values with unnecessary units (use '0' instead of '0px')", len(matches)),
			File:     analysis.File,
			Severity: SeverityInfo,
		})
	}
}

// === EXTERNAL CSS FILE ANALYSIS ===

func analyzeExternalCSS(report *Report, projectPath string) {
	cssPath := filepath.Join(projectPath, "static", "style.css")
	content, err := os.ReadFile(cssPath)
	if err != nil {
		if verbose {
			fmt.Printf("No external CSS file found at %s\n", cssPath)
		}
		return
	}

	css := string(content)
	analysis := TemplateAnalysis{
		File:         cssPath,
		TemplateName: "style.css",
		Checks:       []CheckResult{},
	}

	// Run all CSS validation checks on external stylesheet
	validateExternalCSS(&analysis, css)

	if len(analysis.Checks) > 0 {
		report.Templates = append(report.Templates, analysis)
	}
}

func validateExternalCSS(analysis *TemplateAnalysis, css string) {
	file := analysis.File

	// Check 1: Balanced braces
	openBraces := strings.Count(css, "{")
	closeBraces := strings.Count(css, "}")
	if openBraces != closeBraces {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Balanced braces",
			Passed:   false,
			Message:  fmt.Sprintf("Unbalanced braces: %d open, %d close", openBraces, closeBraces),
			File:     file,
			Severity: SeverityError,
		})
	} else {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Balanced braces",
			Passed:   true,
			Message:  fmt.Sprintf("CSS braces are balanced (%d rule blocks)", openBraces),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 2: CSS custom properties (variables) defined in :root
	rootPattern := regexp.MustCompile(`(?s):root\s*\{([^}]+)\}`)
	rootMatches := rootPattern.FindAllStringSubmatch(css, -1)
	varDefCount := 0
	for _, match := range rootMatches {
		varDefPattern := regexp.MustCompile(`--[a-zA-Z0-9-]+\s*:`)
		vars := varDefPattern.FindAllString(match[1], -1)
		varDefCount += len(vars)
	}
	if varDefCount > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "CSS custom properties",
			Passed:   true,
			Message:  fmt.Sprintf("Found %d CSS custom properties defined in :root", varDefCount),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 3: Dark mode support
	hasDarkMode := strings.Contains(css, "prefers-color-scheme: dark") ||
		strings.Contains(css, ".dark") ||
		strings.Contains(css, "html.dark")
	hasLightMode := strings.Contains(css, "prefers-color-scheme: light") ||
		strings.Contains(css, ".light") ||
		strings.Contains(css, "html.light")
	if hasDarkMode && hasLightMode {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Theme support",
			Passed:   true,
			Message:  "Both dark and light theme support detected",
			File:     file,
			Severity: SeverityInfo,
		})
	} else if hasDarkMode || hasLightMode {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Theme support",
			Passed:   true,
			Message:  "Theme support detected (single theme)",
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 4: Media queries for responsiveness
	mediaPattern := regexp.MustCompile(`@media[^{]+\{`)
	mediaMatches := mediaPattern.FindAllString(css, -1)
	if len(mediaMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Responsive design",
			Passed:   true,
			Message:  fmt.Sprintf("Found %d media queries for responsive design", len(mediaMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 5: No !important overuse
	importantCount := strings.Count(css, "!important")
	propPattern := regexp.MustCompile(`[a-zA-Z-]+\s*:[^;{}]+;`)
	totalDecls := len(propPattern.FindAllString(css, -1))
	if totalDecls > 0 {
		percentage := float64(importantCount) / float64(totalDecls) * 100
		if percentage > 5 {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryCSS,
				Rule:     "Minimal !important usage",
				Passed:   false,
				Message:  fmt.Sprintf("High !important usage: %d times (%.1f%% of %d declarations)", importantCount, percentage, totalDecls),
				File:     file,
				Severity: SeverityWarning,
			})
		} else {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryCSS,
				Rule:     "Minimal !important usage",
				Passed:   true,
				Message:  fmt.Sprintf("!important used %d times (%.1f%% of %d declarations)", importantCount, percentage, totalDecls),
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check 6: Valid color values (hex)
	hexPattern := regexp.MustCompile(`#([0-9a-fA-F]+)`)
	hexMatches := hexPattern.FindAllStringSubmatch(css, -1)
	invalidHex := []string{}
	for _, match := range hexMatches {
		hex := match[1]
		if len(hex) != 3 && len(hex) != 4 && len(hex) != 6 && len(hex) != 8 {
			invalidHex = append(invalidHex, "#"+hex)
		}
	}
	if len(invalidHex) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Valid color values",
			Passed:   false,
			Message:  fmt.Sprintf("Invalid hex colors: %s", strings.Join(invalidHex[:min(5, len(invalidHex))], ", ")),
			File:     file,
			Severity: SeverityError,
		})
	} else if len(hexMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Valid color values",
			Passed:   true,
			Message:  fmt.Sprintf("All %d hex color values are valid", len(hexMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 7: No duplicate selectors (basic check)
	selectorPattern := regexp.MustCompile(`(?m)^([.#]?[a-zA-Z][a-zA-Z0-9_-]*(?:\s*,\s*[.#]?[a-zA-Z][a-zA-Z0-9_-]*)*)\s*\{`)
	selectorMatches := selectorPattern.FindAllStringSubmatch(css, -1)
	selectorCount := make(map[string]int)
	for _, match := range selectorMatches {
		sel := strings.TrimSpace(match[1])
		selectorCount[sel]++
	}
	duplicates := []string{}
	for sel, count := range selectorCount {
		if count > 1 && !strings.Contains(sel, "@") && !strings.HasPrefix(sel, ":") {
			duplicates = append(duplicates, fmt.Sprintf("%s (%d)", sel, count))
		}
	}
	if len(duplicates) > 10 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "No duplicate selectors",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d duplicate selectors (consider consolidating)", len(duplicates)),
			File:     file,
			Severity: SeverityInfo,
		})
	} else {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "No duplicate selectors",
			Passed:   true,
			Message:  "Selector definitions are well-organized",
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 8: Zero values with units
	zeroWithUnit := regexp.MustCompile(`:\s*0(px|em|rem|%|vh|vw|pt)[;\s}]`)
	zeroMatches := zeroWithUnit.FindAllString(css, -1)
	if len(zeroMatches) > 10 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Unitless zero values",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d zero values with unnecessary units", len(zeroMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	} else {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryCSS,
			Rule:     "Unitless zero values",
			Passed:   true,
			Message:  "Zero values are properly formatted",
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// === SEMANTIC HTML CHECKS ===

func runSemanticChecks(analysis *TemplateAnalysis, original string, cleaned string) {
	file := analysis.File

	// Check 1: Document structure elements
	hasMain := strings.Contains(original, "<main")
	hasHeader := strings.Contains(original, "<header")
	hasNav := strings.Contains(original, "<nav")
	hasFooter := strings.Contains(original, "<footer")

	if analysis.TemplateName == "base" {
		structureScore := 0
		if hasMain {
			structureScore++
		}
		if hasHeader {
			structureScore++
		}
		if hasNav {
			structureScore++
		}
		if hasFooter {
			structureScore++
		}

		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySemantic,
			Rule:     "Semantic document structure",
			Passed:   structureScore >= 3,
			Message:  fmt.Sprintf("Found %d/4 semantic landmarks (main, header, nav, footer)", structureScore),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check: Semantic <time> elements for timestamps
	timePattern := regexp.MustCompile(`<time[^>]*>`)
	datetimePattern := regexp.MustCompile(`<time[^>]*datetime="[^"]*"[^>]*>`)
	timeMatches := timePattern.FindAllString(original, -1)
	datetimeMatches := datetimePattern.FindAllString(original, -1)

	if len(timeMatches) > 0 {
		allHaveDatetime := len(timeMatches) == len(datetimeMatches)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySemantic,
			Rule:     "Time elements use datetime attribute",
			Passed:   allHaveDatetime,
			Message:  fmt.Sprintf("%d/%d <time> elements have datetime attribute", len(datetimeMatches), len(timeMatches)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 2: Lists use ul/ol appropriately
	// Use more specific patterns to avoid matching <link when looking for <li
	hasLI := strings.Contains(original, "<li>") || strings.Contains(original, "<li ")
	if hasLI {
		hasUL := strings.Contains(original, "<ul>") || strings.Contains(original, "<ul ")
		hasOL := strings.Contains(original, "<ol>") || strings.Contains(original, "<ol ")
		// Skip check for append/prepend fragments - these return list items
		// to be appended to existing containers via HelmJS
		isAppendFragment := strings.Contains(original, `{{define "`) &&
			(strings.Contains(original, `-append"}}`) ||
				strings.Contains(original, `h-oob="prepend"`) ||
				strings.Contains(original, `h-oob="afterbegin"`))
		if !hasUL && !hasOL && !isAppendFragment {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategorySemantic,
				Rule:     "List items in proper containers",
				Passed:   false,
				Message:  "Found <li> elements without <ul> or <ol> parent",
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// Check 3: Tables have proper structure
	if strings.Contains(original, "<table") {
		hasThead := strings.Contains(original, "<thead")
		_ = strings.Contains(original, "<tbody") // hasTbody - tracked but not scored
		hasTh := strings.Contains(original, "<th")

		if !hasThead && !hasTh {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategorySemantic,
				Rule:     "Tables have headers",
				Passed:   false,
				Message:  "Table missing <thead> or <th> elements",
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// Check 4: Using divs where semantic elements would be better
	divCount := strings.Count(original, "<div")
	articleCount := strings.Count(original, "<article")
	sectionCount := strings.Count(original, "<section")
	asideCount := strings.Count(original, "<aside")

	semanticCount := articleCount + sectionCount + asideCount
	if divCount > 20 && semanticCount == 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySemantic,
			Rule:     "Use semantic elements over divs",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d divs but no article/section/aside elements", divCount),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 5: Heading hierarchy (h1 → h2 → h3, no skips)
	checkHeadingHierarchy(analysis, original)

	// Check 6: Forms have proper structure
	if strings.Contains(original, "<form") {
		_ = strings.Contains(original, "<fieldset") // hasFieldset - tracked but not scored
		_ = strings.Contains(original, "<legend")   // hasLegend - tracked but not scored
		hasLabel := strings.Contains(original, "<label")

		// Check if forms only have hidden inputs (no user-visible inputs that need labels)
		// Forms with only hidden inputs + buttons don't need labels
		formsNeedLabels := formHasVisibleInputs(original)

		if !hasLabel && formsNeedLabels {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategorySemantic,
				Rule:     "Forms use labels",
				Passed:   false,
				Message:  "Form missing <label> elements",
				File:     file,
				Severity: SeverityWarning,
			})
		} else {
			msg := "Form contains proper label elements"
			if !formsNeedLabels {
				msg = "Form only has hidden inputs (labels not required)"
			}
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategorySemantic,
				Rule:     "Forms use labels",
				Passed:   true,
				Message:  msg,
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}
}

// formHasVisibleInputs checks if forms contain user-visible input elements that need labels
func formHasVisibleInputs(content string) bool {
	// Find all forms and check their inputs
	formPattern := regexp.MustCompile(`(?s)<form[^>]*>.*?</form>`)
	forms := formPattern.FindAllString(content, -1)

	for _, form := range forms {
		// Check for textarea and select - always visible
		if strings.Contains(form, "<textarea") || strings.Contains(form, "<select") {
			return true
		}

		// Check for visible input types (not hidden)
		// Find all input tags
		inputPattern := regexp.MustCompile(`<input[^>]*>`)
		inputs := inputPattern.FindAllString(form, -1)

		for _, input := range inputs {
			// Skip if it's a hidden input
			if strings.Contains(input, `type="hidden"`) {
				continue
			}
			// Skip if it's a submit button
			if strings.Contains(input, `type="submit"`) {
				continue
			}
			// If input has no type attribute, it defaults to text (visible)
			if !strings.Contains(input, "type=") {
				return true
			}
			// Check for visible input types
			visibleTypes := []string{
				`type="text"`, `type="email"`, `type="password"`, `type="number"`,
				`type="search"`, `type="tel"`, `type="url"`, `type="date"`,
				`type="time"`, `type="datetime-local"`, `type="month"`, `type="week"`,
				`type="color"`, `type="file"`, `type="range"`, `type="checkbox"`, `type="radio"`,
			}
			for _, vt := range visibleTypes {
				if strings.Contains(input, vt) {
					return true
				}
			}
		}
	}

	return false
}

// checkHeadingHierarchy validates heading levels don't skip (e.g., h1 → h3 without h2)
func checkHeadingHierarchy(analysis *TemplateAnalysis, content string) {
	file := analysis.File

	// Extract all headings in order
	headingPattern := regexp.MustCompile(`<h([1-6])[^>]*>`)
	matches := headingPattern.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return // No headings to check
	}

	// Build list of heading levels
	levels := make([]int, 0, len(matches))
	for _, match := range matches {
		level := int(match[1][0] - '0')
		levels = append(levels, level)
	}

	// Check for skipped levels
	skippedLevels := []string{}
	for i := 1; i < len(levels); i++ {
		prev := levels[i-1]
		curr := levels[i]
		// Going to a deeper level should only increment by 1
		// (Going back up any amount is fine: h3 → h1 is valid)
		if curr > prev && curr > prev+1 {
			skippedLevels = append(skippedLevels, fmt.Sprintf("h%d→h%d", prev, curr))
		}
	}

	// Also check if first heading isn't h1 (for main content templates)
	startsWithH1 := len(levels) > 0 && levels[0] == 1

	// Skip this check for fragment templates that may start with h2/h3
	isFragment := strings.Contains(analysis.TemplateName, "fragment") ||
		strings.Contains(analysis.TemplateName, "kind-") ||
		strings.HasSuffix(analysis.TemplateName, "-response") ||
		strings.HasSuffix(analysis.TemplateName, "-preview")

	if len(skippedLevels) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySemantic,
			Rule:     "Heading hierarchy",
			Passed:   false,
			Message:  fmt.Sprintf("Skipped heading levels: %s (should increment by 1)", strings.Join(skippedLevels, ", ")),
			File:     file,
			Severity: SeverityWarning,
		})
	} else if len(levels) > 1 {
		msg := fmt.Sprintf("Heading hierarchy is correct (%d headings)", len(levels))
		if !startsWithH1 && !isFragment {
			msg = fmt.Sprintf("Heading hierarchy correct but doesn't start with h1 (%d headings)", len(levels))
		}
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySemantic,
			Rule:     "Heading hierarchy",
			Passed:   true,
			Message:  msg,
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// === BEST PRACTICES CHECKS ===

func runBestPracticesChecks(analysis *TemplateAnalysis, original string, cleaned string) {
	file := analysis.File

	// Check 1: DOCTYPE declaration (in base template)
	if analysis.TemplateName == "base" {
		hasDoctype := strings.Contains(strings.ToLower(original), "<!doctype html>")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "DOCTYPE declaration",
			Passed:   hasDoctype,
			Message:  boolToMsg(hasDoctype, "DOCTYPE HTML5 present", "Missing DOCTYPE declaration"),
			File:     file,
			Severity: SeverityError,
		})
	}

	// Check 2: Meta viewport (in base template)
	if analysis.TemplateName == "base" {
		hasViewport := strings.Contains(original, `name="viewport"`) || strings.Contains(original, `name='viewport'`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "Viewport meta tag",
			Passed:   hasViewport,
			Message:  boolToMsg(hasViewport, "Viewport meta tag present", "Missing viewport meta tag"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 3: Character encoding (in base template)
	if analysis.TemplateName == "base" {
		hasCharset := strings.Contains(strings.ToLower(original), `charset="utf-8"`) ||
			strings.Contains(strings.ToLower(original), `charset=utf-8`) ||
			strings.Contains(strings.ToLower(original), `charset='utf-8'`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "Character encoding",
			Passed:   hasCharset,
			Message:  boolToMsg(hasCharset, "UTF-8 charset declared", "Missing charset declaration"),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 4: No inline styles
	inlineStyleCount := strings.Count(original, `style="`)
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategoryBestPractices,
		Rule:     "No inline styles",
		Passed:   inlineStyleCount == 0,
		Message:  boolToMsg(inlineStyleCount == 0, "No inline style attributes found", fmt.Sprintf("Found %d inline style attributes - use CSS classes instead", inlineStyleCount)),
		File:     file,
		Severity: SeverityInfo,
	})

	// Check 5: No inline event handlers
	eventHandlers := []string{
		"onclick", "onload", "onerror", "onmouseover", "onmouseout",
		"onkeydown", "onkeyup", "onchange", "onsubmit", "onfocus", "onblur",
	}
	foundHandlers := []string{}
	for _, handler := range eventHandlers {
		pattern := regexp.MustCompile(fmt.Sprintf(`(?i)\s%s=`, handler))
		if pattern.MatchString(original) {
			foundHandlers = append(foundHandlers, handler)
		}
	}
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategoryBestPractices,
		Rule:     "No inline event handlers",
		Passed:   len(foundHandlers) == 0,
		Message:  boolToMsg(len(foundHandlers) == 0, "No inline event handlers found", fmt.Sprintf("Found inline handlers: %s - use HelmJS attributes instead", strings.Join(foundHandlers, ", "))),
		File:     file,
		Severity: SeverityInfo,
	})

	// Check 6: No inline <script> tags
	// Our philosophy: no JS except HelmJS (loaded via <script src="/static/helm.js">)
	scriptPattern := regexp.MustCompile(`(?i)<script[^>]*>`)
	scriptMatches := scriptPattern.FindAllString(original, -1)
	inlineScripts := 0
	for _, match := range scriptMatches {
		// Allow external scripts (src=), disallow inline
		if !strings.Contains(strings.ToLower(match), "src=") {
			inlineScripts++
		}
	}
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategoryBestPractices,
		Rule:     "No inline JavaScript",
		Passed:   inlineScripts == 0,
		Message:  boolToMsg(inlineScripts == 0, "No inline <script> blocks found", fmt.Sprintf("Found %d inline <script> blocks - use HelmJS attributes instead", inlineScripts)),
		File:     file,
		Severity: SeverityWarning,
	})

	// Check 7: External resources have integrity (for CDN resources)
	cdnPattern := regexp.MustCompile(`(?i)(src|href)="https?://[^"]*cdn[^"]*"`)
	cdnMatches := cdnPattern.FindAllString(original, -1)
	if len(cdnMatches) > 0 {
		hasIntegrity := strings.Contains(original, `integrity="`)
		if !hasIntegrity {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryBestPractices,
				Rule:     "Subresource integrity for CDN resources",
				Passed:   false,
				Message:  fmt.Sprintf("Found %d CDN resources without integrity attributes", len(cdnMatches)),
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// Check 7: Links to external sites have rel="noopener"
	externalLinkPattern := regexp.MustCompile(`<a[^>]*href="https?://[^"]*"[^>]*>`)
	externalLinks := externalLinkPattern.FindAllString(original, -1)
	linksWithoutNoopener := 0
	for _, link := range externalLinks {
		if strings.Contains(link, `target="_blank"`) && !strings.Contains(link, "noopener") {
			linksWithoutNoopener++
		}
	}
	if linksWithoutNoopener > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "External links use rel=noopener",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d external target=_blank links without rel=noopener", linksWithoutNoopener),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 8: Links have semantic rel attributes (author, next, prev, etc.)
	// Check for profile links having rel="author"
	profileLinkPattern := regexp.MustCompile(`<a[^>]*href="/profile[^"]*"[^>]*>`)
	profileLinks := profileLinkPattern.FindAllString(original, -1)
	profileLinksWithAuthor := 0
	for _, link := range profileLinks {
		if strings.Contains(link, `rel="`) && strings.Contains(link, "author") {
			profileLinksWithAuthor++
		}
	}
	if len(profileLinks) > 0 {
		hasSemanticRels := profileLinksWithAuthor > 0
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "Profile links use rel=author",
			Passed:   hasSemanticRels,
			Message:  fmt.Sprintf("%d/%d profile links have rel=author", profileLinksWithAuthor, len(profileLinks)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 9: Pagination links use rel="next"/"prev"
	// Only check for pagination if there's actual HTML with pagination class,
	// not CSS-only definitions (e.g., ".pagination {" is CSS, not HTML usage)
	paginationHTMLPattern := regexp.MustCompile(`class="[^"]*pagination[^"]*"`)
	hasPaginationHTML := paginationHTMLPattern.MatchString(original)
	if hasPaginationHTML {
		hasRelNext := strings.Contains(original, `rel="next"`)
		hasRelPrev := strings.Contains(original, `rel="prev"`)
		hasPaginationRels := hasRelNext || hasRelPrev
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryBestPractices,
			Rule:     "Pagination uses rel=next/prev",
			Passed:   hasPaginationRels,
			Message:  boolToMsg(hasPaginationRels, "Pagination links have rel=next/prev attributes", "Pagination links missing rel=next/prev"),
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// === PERFORMANCE CHECKS ===

func runPerformanceChecks(analysis *TemplateAnalysis, content string) {
	file := analysis.File

	// Check 1: Images use lazy loading (loading="lazy")
	imgPattern := regexp.MustCompile(`<img[^>]+>`)
	imgMatches := imgPattern.FindAllString(content, -1)
	lazyLoadedImages := 0
	for _, img := range imgMatches {
		if strings.Contains(img, `loading="lazy"`) {
			lazyLoadedImages++
		}
	}
	if len(imgMatches) > 0 {
		allLazy := lazyLoadedImages == len(imgMatches)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "Images use lazy loading",
			Passed:   allLazy || lazyLoadedImages > 0,
			Message:  fmt.Sprintf("%d/%d images have loading=\"lazy\" attribute", lazyLoadedImages, len(imgMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 2: Iframes use lazy loading
	iframePattern := regexp.MustCompile(`<iframe[^>]+>`)
	iframeMatches := iframePattern.FindAllString(content, -1)
	lazyLoadedIframes := 0
	for _, iframe := range iframeMatches {
		if strings.Contains(iframe, `loading="lazy"`) {
			lazyLoadedIframes++
		}
	}
	if len(iframeMatches) > 0 {
		allLazy := lazyLoadedIframes == len(iframeMatches)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "Iframes use lazy loading",
			Passed:   allLazy,
			Message:  fmt.Sprintf("%d/%d iframes have loading=\"lazy\" attribute", lazyLoadedIframes, len(iframeMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 3: External scripts use async or defer (in base template)
	if analysis.TemplateName == "base" {
		scriptPattern := regexp.MustCompile(`<script[^>]*src="[^"]*"[^>]*>`)
		scriptMatches := scriptPattern.FindAllString(content, -1)
		asyncDefer := 0
		for _, script := range scriptMatches {
			if strings.Contains(script, "async") || strings.Contains(script, "defer") {
				asyncDefer++
			}
		}
		if len(scriptMatches) > 0 {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryPerformance,
				Rule:     "Scripts use async/defer",
				Passed:   asyncDefer == len(scriptMatches),
				Message:  fmt.Sprintf("%d/%d external scripts have async or defer attribute", asyncDefer, len(scriptMatches)),
				File:     file,
				Severity: SeverityWarning,
			})
		}
	}

	// Check 4: Preload hints for critical resources (in base template)
	if analysis.TemplateName == "base" {
		hasPreload := strings.Contains(content, `rel="preload"`)
		hasPrefetch := strings.Contains(content, `rel="prefetch"`) || strings.Contains(content, `rel="dns-prefetch"`)
		hasPreconnect := strings.Contains(content, `rel="preconnect"`)

		hints := 0
		if hasPreload {
			hints++
		}
		if hasPrefetch {
			hints++
		}
		if hasPreconnect {
			hints++
		}

		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "Resource hints present",
			Passed:   hints > 0,
			Message:  boolToMsg(hints > 0, fmt.Sprintf("Found %d resource hint types (preload, prefetch, preconnect)", hints), "No resource hints found (consider preload/prefetch/preconnect for critical resources)"),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 5: Images have width/height to prevent layout shifts
	imagesWithDimensions := 0
	for _, img := range imgMatches {
		hasWidth := strings.Contains(img, "width=") || strings.Contains(img, "width:")
		hasHeight := strings.Contains(img, "height=") || strings.Contains(img, "height:")
		// Template variables for dimensions count as having dimensions
		hasDynamic := strings.Contains(img, "{{")
		if (hasWidth && hasHeight) || hasDynamic {
			imagesWithDimensions++
		}
	}
	if len(imgMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "Images have dimensions",
			Passed:   imagesWithDimensions >= len(imgMatches)/2,
			Message:  fmt.Sprintf("%d/%d images have width/height (prevents layout shift)", imagesWithDimensions, len(imgMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 6: No inline base64 images (performance concern for large images)
	base64Pattern := regexp.MustCompile(`src="data:image/[^"]{1000,}"`)
	base64Matches := base64Pattern.FindAllString(content, -1)
	if len(base64Matches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "No large inline base64 images",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d large base64 images (consider external files)", len(base64Matches)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 7: Video/audio use preload appropriately
	mediaPattern := regexp.MustCompile(`<(video|audio)[^>]*>`)
	mediaMatches := mediaPattern.FindAllString(content, -1)
	mediaWithPreload := 0
	for _, media := range mediaMatches {
		if strings.Contains(media, `preload="`) {
			mediaWithPreload++
		}
	}
	if len(mediaMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryPerformance,
			Rule:     "Media has preload attribute",
			Passed:   mediaWithPreload > 0,
			Message:  fmt.Sprintf("%d/%d video/audio elements have preload attribute", mediaWithPreload, len(mediaMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// === SEO & META CHECKS ===

func runSEOChecks(analysis *TemplateAnalysis, content string) {
	file := analysis.File

	// Only check SEO elements in base template
	if analysis.TemplateName != "base" {
		return
	}

	// Check 1: Title tag exists
	hasTitle := strings.Contains(content, "<title")
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "Title tag present",
		Passed:   hasTitle,
		Message:  boolToMsg(hasTitle, "Page has <title> tag", "Missing <title> tag"),
		File:     file,
		Severity: SeverityError,
	})

	// Check 2: Meta description exists
	hasMetaDesc := strings.Contains(content, `name="description"`) || strings.Contains(content, `name='description'`)
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "Meta description present",
		Passed:   hasMetaDesc,
		Message:  boolToMsg(hasMetaDesc, "Page has meta description", "Missing meta description tag"),
		File:     file,
		Severity: SeverityWarning,
	})

	// Check 3: Open Graph tags for social sharing
	ogTags := []string{"og:title", "og:description", "og:image", "og:url", "og:type"}
	foundOG := 0
	for _, tag := range ogTags {
		if strings.Contains(content, fmt.Sprintf(`property="%s"`, tag)) ||
			strings.Contains(content, fmt.Sprintf(`property='%s'`, tag)) {
			foundOG++
		}
	}
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "Open Graph tags present",
		Passed:   foundOG >= 3,
		Message:  fmt.Sprintf("Found %d/%d Open Graph tags (og:title, og:description, og:image, og:url, og:type)", foundOG, len(ogTags)),
		File:     file,
		Severity: SeverityInfo,
	})

	// Check 4: Canonical URL
	hasCanonical := strings.Contains(content, `rel="canonical"`)
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "Canonical URL present",
		Passed:   hasCanonical,
		Message:  boolToMsg(hasCanonical, "Page has canonical URL", "Missing canonical URL (rel=\"canonical\")"),
		File:     file,
		Severity: SeverityInfo,
	})

	// Check 6: Language attribute on HTML element
	hasLang := regexp.MustCompile(`<html[^>]*\slang=`).MatchString(content)
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "HTML lang attribute",
		Passed:   hasLang,
		Message:  boolToMsg(hasLang, "HTML element has lang attribute", "Missing lang attribute on <html>"),
		File:     file,
		Severity: SeverityWarning,
	})

	// Check 7: Robots meta tag (if restrictive, should be intentional)
	hasRobots := strings.Contains(content, `name="robots"`)
	noIndex := strings.Contains(content, "noindex")
	if hasRobots && noIndex {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategorySEO,
			Rule:     "Robots directive",
			Passed:   true,
			Message:  "Page has robots meta tag with noindex (verify this is intentional)",
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 7: Favicon
	hasFavicon := strings.Contains(content, `rel="icon"`) ||
		strings.Contains(content, `rel="shortcut icon"`) ||
		strings.Contains(content, "favicon")
	analysis.Checks = append(analysis.Checks, CheckResult{
		Category: CategorySEO,
		Rule:     "Favicon present",
		Passed:   hasFavicon,
		Message:  boolToMsg(hasFavicon, "Page has favicon link", "Missing favicon link"),
		File:     file,
		Severity: SeverityInfo,
	})
}

// === MOBILE CHECKS ===

func runMobileChecks(analysis *TemplateAnalysis, content string) {
	file := analysis.File

	// Check 1: Viewport meta tag (in base template)
	if analysis.TemplateName == "base" {
		hasViewport := strings.Contains(content, `name="viewport"`)
		viewportCorrect := strings.Contains(content, "width=device-width")
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Viewport configured",
			Passed:   hasViewport && viewportCorrect,
			Message:  boolToMsg(hasViewport && viewportCorrect, "Viewport meta tag with width=device-width", "Missing or incorrect viewport meta tag"),
			File:     file,
			Severity: SeverityError,
		})

		// Check for user-scalable=no (accessibility concern)
		hasNoScale := strings.Contains(content, "user-scalable=no") || strings.Contains(content, "user-scalable=0")
		if hasNoScale {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryMobile,
				Rule:     "Zoom enabled",
				Passed:   false,
				Message:  "Viewport disables zooming (user-scalable=no) - bad for accessibility",
				File:     file,
				Severity: SeverityWarning,
			})
		} else if hasViewport {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryMobile,
				Rule:     "Zoom enabled",
				Passed:   true,
				Message:  "Viewport allows user scaling/zooming",
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check 2: Touch action CSS (look for touch manipulation patterns)
	hasTouchAction := strings.Contains(content, "touch-action")
	if hasTouchAction {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Touch action defined",
			Passed:   true,
			Message:  "Found touch-action CSS property for touch optimization",
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 3: Click handlers have touch equivalents (check for touchstart/touchend)
	// In hypermedia apps, we mostly use links/forms which work on touch
	clickPattern := regexp.MustCompile(`onclick=`)
	clickMatches := clickPattern.FindAllString(content, -1)
	if len(clickMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Touch-friendly interactions",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d onclick handlers - prefer links/forms or HelmJS attributes for touch support", len(clickMatches)),
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 4: Forms use appropriate input types for mobile keyboards
	inputTypes := map[string]string{
		"email": `type="email"`,
		"tel":   `type="tel"`,
		"url":   `type="url"`,
		"date":  `type="date"`,
	}
	for name, pattern := range inputTypes {
		if strings.Contains(content, pattern) {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryMobile,
				Rule:     "Mobile-optimized input types",
				Passed:   true,
				Message:  fmt.Sprintf("Found %s input type (shows optimized mobile keyboard)", name),
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check 5: Autocomplete attributes for better mobile UX
	if strings.Contains(content, "<input") {
		hasAutocomplete := strings.Contains(content, `autocomplete="`)
		if hasAutocomplete {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryMobile,
				Rule:     "Autocomplete hints present",
				Passed:   true,
				Message:  "Forms use autocomplete attribute for better mobile UX",
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check 6: Text is readable without zoom (font-size >= 16px recommended)
	// Look for small font sizes in inline styles (shouldn't exist per our rules)
	smallFontPattern := regexp.MustCompile(`font-size:\s*(8|9|10|11|12)px`)
	smallFontMatches := smallFontPattern.FindAllString(content, -1)
	if len(smallFontMatches) > 0 {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Readable font sizes",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d small font sizes (< 13px) - may be hard to read on mobile", len(smallFontMatches)),
			File:     file,
			Severity: SeverityWarning,
		})
	}

	// Check 7: Mobile nav patterns (hamburger menu, collapsible nav)
	hasDetails := strings.Contains(content, "<details")
	hasCollapsible := strings.Contains(content, "collapse") || strings.Contains(content, "toggle")
	if analysis.TemplateName == "base" && (hasDetails || hasCollapsible) {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Mobile navigation pattern",
			Passed:   true,
			Message:  "Found collapsible/details elements for mobile navigation",
			File:     file,
			Severity: SeverityInfo,
		})
	}

	// Check 8: Apple mobile web app capable (for PWA-like behavior)
	if analysis.TemplateName == "base" {
		hasAppleMeta := strings.Contains(content, "apple-mobile-web-app-capable") ||
			strings.Contains(content, "apple-touch-icon")
		if hasAppleMeta {
			analysis.Checks = append(analysis.Checks, CheckResult{
				Category: CategoryMobile,
				Rule:     "iOS web app meta tags",
				Passed:   true,
				Message:  "Found Apple mobile web app meta tags",
				File:     file,
				Severity: SeverityInfo,
			})
		}
	}

	// Check 9: Theme color meta tag (affects mobile browser chrome)
	if analysis.TemplateName == "base" {
		hasThemeColor := strings.Contains(content, `name="theme-color"`)
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category: CategoryMobile,
			Rule:     "Theme color meta tag",
			Passed:   hasThemeColor,
			Message:  boolToMsg(hasThemeColor, "Found theme-color meta tag", "Missing theme-color meta tag (affects mobile browser UI)"),
			File:     file,
			Severity: SeverityInfo,
		})
	}
}

// === DEAD CODE ANALYSIS ===

func runDeadCodeAnalysis(report *Report, projectPath string) {
	// Collect all CSS classes defined in style.css and <style> blocks
	definedClasses := make(map[string]bool)
	// Collect all CSS classes used in class="" attributes
	usedClasses := make(map[string]bool)
	// Collect all templates defined
	definedTemplates := make(map[string]bool)
	// Collect all templates referenced
	usedTemplates := make(map[string]bool)

	// Read all template files (including subdirectories like templates/kinds/)
	templatesDir := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesDir, "*.go"))
	// Also include templates/kinds/*.go
	kindTemplates, _ := filepath.Glob(filepath.Join(templatesDir, "kinds", "*.go"))
	templateFiles = append(templateFiles, kindTemplates...)

	var allContent strings.Builder
	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		allContent.WriteString(string(content))
	}

	fullContent := allContent.String()

	// Extract CSS class definitions from external style.css
	cssPath := filepath.Join(projectPath, "static", "style.css")
	if cssContent, err := os.ReadFile(cssPath); err == nil {
		css := string(cssContent)
		// Find class selectors: .class-name (handles complex selectors)
		classDefPattern := regexp.MustCompile(`\.([a-zA-Z_-][a-zA-Z0-9_-]*)`)
		classMatches := classDefPattern.FindAllStringSubmatch(css, -1)
		for _, cm := range classMatches {
			definedClasses[cm[1]] = true
		}
	}

	// Also extract CSS class definitions from <style> blocks in templates
	stylePattern := regexp.MustCompile(`(?s)<style[^>]*>(.*?)</style>`)
	styleMatches := stylePattern.FindAllStringSubmatch(fullContent, -1)
	for _, match := range styleMatches {
		css := match[1]
		// Find class selectors: .class-name
		classDefPattern := regexp.MustCompile(`\.([a-zA-Z_-][a-zA-Z0-9_-]*)`)
		classMatches := classDefPattern.FindAllStringSubmatch(css, -1)
		for _, cm := range classMatches {
			definedClasses[cm[1]] = true
		}
	}

	// Extract CSS classes used in class="" attributes
	// Need to handle Go template expressions which may contain quotes: class="foo{{if eq .X "y"}} bar{{end}}"
	// Use a custom extractor that handles nested template expressions
	classAttrPattern := regexp.MustCompile(`class="`)
	classAttrIndices := classAttrPattern.FindAllStringIndex(fullContent, -1)
	for _, idx := range classAttrIndices {
		start := idx[1] // Position after class="
		// Find the closing quote, but skip quotes inside {{ }}
		classAttr := extractClassAttrValue(fullContent[start:])
		if classAttr == "" {
			continue
		}

		// Handle dynamic class constructions like: class="checkbox-box{{if .IsChecked}} checked{{end}}"
		// First extract static parts before/after template expressions
		cleaned := regexp.MustCompile(`\{\{.*?\}\}`).ReplaceAllString(classAttr, " ")
		classes := strings.Fields(cleaned)
		for _, class := range classes {
			usedClasses[class] = true
		}
		// Also extract class names from within template conditionals: {{if .X}} classname{{end}}
		conditionalPattern := regexp.MustCompile(`\}\}\s*([a-zA-Z_-][a-zA-Z0-9_-]*)`)
		conditionalMatches := conditionalPattern.FindAllStringSubmatch(classAttr, -1)
		for _, cm := range conditionalMatches {
			usedClasses[cm[1]] = true
		}
		// Handle dynamic class prefixes like: class="classified-status-{{.Status}}"
		// This generates classes like classified-status-active, classified-status-sold, etc.
		prefixPattern := regexp.MustCompile(`([a-zA-Z_-][a-zA-Z0-9_-]*-)\{\{`)
		prefixMatches := prefixPattern.FindAllStringSubmatch(classAttr, -1)
		for _, pm := range prefixMatches {
			prefix := pm[1]
			// Mark common dynamic suffixes as used
			commonSuffixes := []string{"active", "inactive", "sold", "pending", "live", "ended", "scheduled", "playing", "paused", "loading", "error", "success", "incoming", "outgoing", "accepted", "declined", "tentative"}
			for _, suffix := range commonSuffixes {
				usedClasses[prefix+suffix] = true
			}
		}
	}

	// Also look for raw class name strings that may be used in Go code for dynamic class building
	rawClassPattern := regexp.MustCompile(`"([a-zA-Z_-][a-zA-Z0-9_-]*)"`)
	rawMatches := rawClassPattern.FindAllStringSubmatch(fullContent, -1)
	for _, match := range rawMatches {
		// Only add if it looks like a CSS class (common patterns)
		class := match[1]
		if strings.Contains(class, "-") && !strings.HasPrefix(class, "/") && len(class) < 30 {
			usedClasses[class] = true
		}
	}

	// Read main Go files for class usage (html.go, kinds.go handlers build HTML directly)
	goFiles := []string{"html.go", "html_handlers.go", "html_auth.go", "kinds.go", "kinds_appliers.go"}
	for _, goFile := range goFiles {
		goPath := filepath.Join(projectPath, goFile)
		if goContent, err := os.ReadFile(goPath); err == nil {
			goStr := string(goContent)
			// Look for class="" in string literals
			classInGo := regexp.MustCompile(`class="([^"]*)"`)
			goClassMatches := classInGo.FindAllStringSubmatch(goStr, -1)
			for _, match := range goClassMatches {
				classes := strings.Fields(match[1])
				for _, class := range classes {
					usedClasses[class] = true
				}
			}
		}
	}

	// Extract template definitions: {{define "name"}}
	definePattern := regexp.MustCompile(`\{\{define\s+"([^"]+)"\}\}`)
	defineMatches := definePattern.FindAllStringSubmatch(fullContent, -1)
	for _, match := range defineMatches {
		definedTemplates[match[1]] = true
	}

	// Extract template usages: {{template "name"}}
	templatePattern := regexp.MustCompile(`\{\{template\s+"([^"]+)"`)
	templateMatches := templatePattern.FindAllStringSubmatch(fullContent, -1)
	for _, match := range templateMatches {
		usedTemplates[match[1]] = true
	}

	// Also check for templates referenced in Go code (multiple files use templates)
	templateGoFiles := []string{"html.go", "html_handlers.go", "html_auth.go", "flash.go", "sse.go"}
	for _, goFile := range templateGoFiles {
		goPath := filepath.Join(projectPath, goFile)
		if goContent, err := os.ReadFile(goPath); err == nil {
			goStr := string(goContent)
			// Look for ExecuteTemplate calls
			execPattern := regexp.MustCompile(`ExecuteTemplate\([^,]+,\s*"([^"]+)"`)
			execMatches := execPattern.FindAllStringSubmatch(goStr, -1)
			for _, match := range execMatches {
				usedTemplates[match[1]] = true
			}
			// Look for template.New("name") calls (fragment templates)
			newPattern := regexp.MustCompile(`template\.New\("([^"]+)"\)`)
			newMatches := newPattern.FindAllStringSubmatch(goStr, -1)
			for _, match := range newMatches {
				usedTemplates[match[1]] = true
			}
			// Look for tmplXxx = "name" constant definitions
			constPattern := regexp.MustCompile(`tmpl\w+\s*=\s*"([^"]+)"`)
			constMatches := constPattern.FindAllStringSubmatch(goStr, -1)
			for _, match := range constMatches {
				usedTemplates[match[1]] = true
			}
			// Look for mustCompileTemplate("name", ...) calls
			compilePattern := regexp.MustCompile(`mustCompileTemplate\("([^"]+)"`)
			compileMatches := compilePattern.FindAllStringSubmatch(goStr, -1)
			for _, match := range compileMatches {
				usedTemplates[match[1]] = true
			}
		}
	}

	// Find unused CSS classes
	unusedClasses := []string{}
	// Classes to skip: JS-controlled states, pseudo-states, common utility patterns
	skipPrefixes := []string{
		"h-",       // HelmJS states
		"active",   // State variants
		"hover",    // Hover states (CSS only)
		"focus",    // Focus states
		"disabled", // Disabled states
		"open",     // Open states
		"dark",     // Theme classes (applied via JS)
		"light",    // Theme classes
		"is-",      // Common state prefix
		"has-",     // Common state prefix
	}
	// Exact matches to skip
	skipExact := map[string]bool{
		"checked":  true, // Checkbox state (dynamic)
		"selected": true, // Selection state (dynamic)
		"hidden":   true, // Visibility state (dynamic)
		"visible":  true, // Visibility state (dynamic)
		"loading":  true, // Loading state (dynamic)
		"error":    true, // Error state (dynamic)
		"success":  true, // Success state (dynamic)
	}
	for class := range definedClasses {
		if !usedClasses[class] {
			// Skip exact matches
			if skipExact[class] {
				continue
			}
			// Skip prefix matches
			shouldSkip := false
			for _, prefix := range skipPrefixes {
				if strings.HasPrefix(class, prefix) {
					shouldSkip = true
					break
				}
			}
			if shouldSkip {
				continue
			}
			unusedClasses = append(unusedClasses, class)
		}
	}
	sort.Strings(unusedClasses)

	// Find unused templates
	unusedTemplates := []string{}
	// These are special templates that are used but not via {{template}}
	specialTemplates := map[string]bool{
		"base": true, "content": true, "fragment": true,
	}
	for tmpl := range definedTemplates {
		if !usedTemplates[tmpl] && !specialTemplates[tmpl] {
			unusedTemplates = append(unusedTemplates, tmpl)
		}
	}
	sort.Strings(unusedTemplates)

	// Create a dead code analysis result
	deadCodeAnalysis := TemplateAnalysis{
		File:         "cross-template",
		TemplateName: "Dead Code Analysis",
		Checks:       []CheckResult{},
	}

	// Report unused CSS classes
	if len(unusedClasses) > 0 {
		// Limit to first 20 for readability
		displayClasses := unusedClasses
		if len(displayClasses) > 20 {
			displayClasses = displayClasses[:20]
		}
		deadCodeAnalysis.Checks = append(deadCodeAnalysis.Checks, CheckResult{
			Category: CategoryDeadCode,
			Rule:     "No unused CSS classes",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d potentially unused CSS classes in style.css: %s", len(unusedClasses), strings.Join(displayClasses, ", ")),
			File:     "static/style.css + templates/*.go",
			Severity: SeverityInfo,
		})
	} else {
		deadCodeAnalysis.Checks = append(deadCodeAnalysis.Checks, CheckResult{
			Category: CategoryDeadCode,
			Rule:     "No unused CSS classes",
			Passed:   true,
			Message:  fmt.Sprintf("All %d CSS classes from style.css appear to be used in templates", len(definedClasses)),
			File:     "static/style.css + templates/*.go",
			Severity: SeverityInfo,
		})
	}

	// Report unused templates
	if len(unusedTemplates) > 0 {
		deadCodeAnalysis.Checks = append(deadCodeAnalysis.Checks, CheckResult{
			Category: CategoryDeadCode,
			Rule:     "No unused templates",
			Passed:   false,
			Message:  fmt.Sprintf("Found %d potentially unused templates: %s", len(unusedTemplates), strings.Join(unusedTemplates, ", ")),
			File:     "templates/*.go",
			Severity: SeverityInfo,
		})
	} else {
		deadCodeAnalysis.Checks = append(deadCodeAnalysis.Checks, CheckResult{
			Category: CategoryDeadCode,
			Rule:     "No unused templates",
			Passed:   true,
			Message:  fmt.Sprintf("All %d templates appear to be used", len(definedTemplates)),
			File:     "templates/*.go",
			Severity: SeverityInfo,
		})
	}

	report.Templates = append(report.Templates, deadCodeAnalysis)
}

// === HELPERS ===

// extractClassAttrValue extracts the class attribute value handling Go template expressions with quotes
// e.g., from: `foo{{if eq .X "y"}} bar{{end}}"...` extracts `foo{{if eq .X "y"}} bar{{end}}`
func extractClassAttrValue(s string) string {
	var result strings.Builder
	inTemplate := false
	templateDepth := 0

	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '{' && s[i+1] == '{' {
			inTemplate = true
			templateDepth++
			result.WriteString("{{")
			i++ // Skip second {
			continue
		}
		if i+1 < len(s) && s[i] == '}' && s[i+1] == '}' {
			templateDepth--
			if templateDepth <= 0 {
				inTemplate = false
				templateDepth = 0
			}
			result.WriteString("}}")
			i++ // Skip second }
			continue
		}
		if s[i] == '"' && !inTemplate {
			// Found closing quote outside template
			return result.String()
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

func boolToMsg(b bool, trueMsg, falseMsg string) string {
	if b {
		return trueMsg
	}
	return falseMsg
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
    <title>Markup Validation Report (HTML/CSS)</title>
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

        .resource-link {
            color: var(--blue);
            text-decoration: none;
        }
        .resource-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Markup Validation Report</h1>
        <p class="meta">Generated: %s | Templates analyzed: %d</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: %s; color: %s;">
                %s
                <span class="score-label">%.0f%%</span>
            </div>
            <div class="score-details">
                <h3>Overall Markup Score</h3>
                <p>Your templates were checked for valid HTML structure, CSS syntax, semantic markup, and best practices.</p>
                <p style="margin-top: 0.5rem; color: var(--text-muted);">
                    This static analysis scans template source code directly without requiring a running server.
                </p>
            </div>
        </div>

        <h2>Validation Categories</h2>
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
		CategoryHTML:          "Valid HTML structure, proper tag nesting, and attribute usage",
		CategoryCSS:           "CSS syntax, valid properties, balanced braces, and color values",
		CategorySemantic:      "Semantic HTML elements for better structure and accessibility",
		CategoryBestPractices: "Modern web standards: DOCTYPE, viewport, charset, security",
		CategoryDeadCode:      "Unused CSS classes and templates that can be removed",
		CategoryPerformance:   "Resource loading optimization, lazy loading, and caching hints",
		CategorySEO:           "Meta tags, Open Graph, and discoverability",
		CategoryMobile:        "Mobile-friendly patterns, responsive design, and touch support",
	}

	// Sort categories for consistent output
	categories := []string{CategoryHTML, CategoryCSS, CategorySemantic, CategoryBestPractices, CategoryDeadCode, CategoryPerformance, CategorySEO, CategoryMobile}
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
            <li><a href="https://validator.w3.org/" class="resource-link">W3C Markup Validation Service</a></li>
            <li><a href="https://jigsaw.w3.org/css-validator/" class="resource-link">W3C CSS Validation Service</a></li>
            <li><a href="https://html.spec.whatwg.org/" class="resource-link">HTML Living Standard</a></li>
            <li><a href="https://developer.mozilla.org/en-US/docs/Web/HTML" class="resource-link">MDN HTML Reference</a></li>
            <li><a href="https://developer.mozilla.org/en-US/docs/Web/CSS" class="resource-link">MDN CSS Reference</a></li>
        </ul>

        <h2>Limitations</h2>
        <p style="color: var(--text-muted);">
            This static analysis cannot check:
        </p>
        <ul style="margin-left: 1.5rem; color: var(--text-muted); margin-top: 0.5rem;">
            <li>Runtime-generated HTML (requires rendered output)</li>
            <li>CSS specificity conflicts (requires computed styles)</li>
            <li>Browser compatibility (requires actual rendering)</li>
            <li>Template variable validation (only checks structure)</li>
        </ul>
    </div>
</body>
</html>
`)

	return nil
}
