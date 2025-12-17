// Static Security Checker
// Analyzes Go source and template files for security vulnerabilities
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
)

// Security categories
const (
	CategoryXSS         = "XSS Prevention"
	CategoryCSRF        = "CSRF Protection"
	CategoryHeaders     = "HTTP Security Headers"
	CategorySecrets     = "Secrets & Credentials"
	CategorySession     = "Session Security"
	CategoryInput       = "Input Validation"
	CategoryNostr       = "Nostr Security"
	CategoryRateLimit   = "Rate Limiting"
	CategorySSRF        = "SSRF Prevention"
	CategoryCrypto      = "Cryptography"
	CategoryInfoLeak    = "Information Disclosure"
)

// Severity levels
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityInfo     = "info"
)

// CheckResult represents a single security check result
type CheckResult struct {
	Category    string
	Rule        string
	Passed      bool
	Message     string
	File        string
	Line        int
	Severity    string
	Remediation string
}

// FileAnalysis contains analysis results for a single file
type FileAnalysis struct {
	File   string
	Checks []CheckResult
}

// Report contains the full security report
type Report struct {
	GeneratedAt time.Time
	ProjectPath string
	Files       []FileAnalysis
	Summary     map[string]CategorySummary
	TotalScore  float64
	Critical    int
	High        int
	Medium      int
	Low         int
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
	flag.StringVar(&projectPath, "path", ".", "Path to project root")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.StringVar(&outputFile, "output", "security-report.html", "Output file")
	flag.Parse()

	fmt.Printf("Security Checker\n")
	fmt.Printf("========================================\n")
	fmt.Printf("Project path: %s\n", projectPath)

	report := &Report{
		GeneratedAt: time.Now(),
		ProjectPath: projectPath,
		Files:       []FileAnalysis{},
		Summary:     make(map[string]CategorySummary),
	}

	// Analyze Go files in root
	goFiles, err := filepath.Glob(filepath.Join(projectPath, "*.go"))
	if err != nil {
		fmt.Printf("Error finding Go files: %v\n", err)
		os.Exit(1)
	}

	// Analyze template files
	templateFiles, err := filepath.Glob(filepath.Join(projectPath, "templates", "*.go"))
	if err != nil {
		fmt.Printf("Error finding template files: %v\n", err)
	}

	// Analyze config files
	configFiles, err := filepath.Glob(filepath.Join(projectPath, "config", "*.json"))
	if err != nil {
		fmt.Printf("Error finding config files: %v\n", err)
	}

	allFiles := append(goFiles, templateFiles...)
	allFiles = append(allFiles, configFiles...)

	fmt.Printf("Found %d files to analyze\n\n", len(allFiles))

	// Analyze each file
	for _, file := range allFiles {
		if verbose {
			fmt.Printf("Analyzing: %s\n", file)
		}

		analysis := analyzeFile(file)
		if len(analysis.Checks) > 0 {
			report.Files = append(report.Files, analysis)
		}
	}

	// Run cross-file checks
	crossFileChecks := runCrossFileChecks(projectPath, goFiles, templateFiles)
	if len(crossFileChecks.Checks) > 0 {
		report.Files = append(report.Files, crossFileChecks)
	}

	// Calculate summary
	calculateSummary(report)

	// Generate HTML report
	if err := generateHTMLReport(report, outputFile); err != nil {
		fmt.Printf("Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Analyzed %d files\n", len(allFiles))
	fmt.Printf("Overall Score: %.1f%%\n", report.TotalScore)
	fmt.Printf("Report saved to: %s\n", outputFile)

	// Print severity counts
	fmt.Printf("\nFindings by Severity:\n")
	fmt.Printf("  Critical: %d\n", report.Critical)
	fmt.Printf("  High:     %d\n", report.High)
	fmt.Printf("  Medium:   %d\n", report.Medium)
	fmt.Printf("  Low:      %d\n", report.Low)

	// Print category scores
	fmt.Printf("\nCategory Scores:\n")
	categories := []string{CategoryXSS, CategoryCSRF, CategoryHeaders, CategorySecrets, CategorySession, CategoryInput, CategoryNostr, CategoryRateLimit, CategorySSRF, CategoryCrypto, CategoryInfoLeak}
	for _, cat := range categories {
		if summary, ok := report.Summary[cat]; ok {
			fmt.Printf("  %-25s %3d/%3d (%.0f%%)\n", cat+":", summary.Passed, summary.Total, summary.Score)
		}
	}
}

func analyzeFile(filePath string) FileAnalysis {
	analysis := FileAnalysis{
		File:   filePath,
		Checks: []CheckResult{},
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return analysis
	}

	fileContent := string(content)
	fileName := filepath.Base(filePath)
	isTemplate := strings.Contains(filePath, "templates/")
	isConfig := strings.HasSuffix(filePath, ".json")
	isGoFile := strings.HasSuffix(filePath, ".go")

	if isTemplate {
		analysis.Checks = append(analysis.Checks, checkTemplateXSS(fileContent, filePath)...)
		analysis.Checks = append(analysis.Checks, checkTemplateCSRF(fileContent, filePath)...)
		analysis.Checks = append(analysis.Checks, checkTemplateSecrets(fileContent, filePath)...)
	}

	if isGoFile && !isTemplate {
		analysis.Checks = append(analysis.Checks, checkGoSecurityHeaders(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoSessionSecurity(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoInputValidation(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoSecrets(fileContent, filePath)...)
		analysis.Checks = append(analysis.Checks, checkGoNostrSecurity(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoRateLimiting(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoSSRF(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoCryptography(fileContent, filePath, fileName)...)
		analysis.Checks = append(analysis.Checks, checkGoInfoDisclosure(fileContent, filePath, fileName)...)
	}

	if isConfig {
		analysis.Checks = append(analysis.Checks, checkConfigSecrets(fileContent, filePath)...)
	}

	return analysis
}

// XSS Prevention Checks for Templates
func checkTemplateXSS(content, filePath string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for safeHTML / template.HTML usage (bypasses escaping)
	safeHTMLPattern := regexp.MustCompile(`\{\{[^}]*\|\s*safeHTML[^}]*\}\}`)
	for i, line := range lines {
		if matches := safeHTMLPattern.FindAllString(line, -1); len(matches) > 0 {
			for _, match := range matches {
				// Check if it's user content (contains .Content, .Body, .Description, etc.)
				userContentPattern := regexp.MustCompile(`\.(Content|Body|Description|Text|Name|DisplayName|About|Bio)`)
				isUserContent := userContentPattern.MatchString(match)

				checks = append(checks, CheckResult{
					Category:    CategoryXSS,
					Rule:        "Avoid safeHTML with user content",
					Passed:      !isUserContent,
					Message:     fmt.Sprintf("safeHTML usage: %s", truncate(match, 60)),
					File:        filePath,
					Line:        i + 1,
					Severity:    ternary(isUserContent, SeverityHigh, SeverityMedium),
					Remediation: "Use template auto-escaping instead of safeHTML for user content",
				})
			}
		}
	}

	// Check for template.HTML type assertions in Go templates
	templateHTMLPattern := regexp.MustCompile(`template\.HTML\s*\(`)
	for i, line := range lines {
		if matches := templateHTMLPattern.FindAllString(line, -1); len(matches) > 0 {
			checks = append(checks, CheckResult{
				Category:    CategoryXSS,
				Rule:        "Audit template.HTML conversions",
				Passed:      false,
				Message:     "template.HTML conversion bypasses auto-escaping",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityMedium,
				Remediation: "Ensure input is sanitized before template.HTML conversion",
			})
		}
	}

	// Check for inline event handlers
	eventHandlerPattern := regexp.MustCompile(`(?i)\s(on\w+)\s*=\s*["']`)
	for i, line := range lines {
		if matches := eventHandlerPattern.FindAllStringSubmatch(line, -1); len(matches) > 0 {
			for _, match := range matches {
				handler := match[1]
				// Allow specific safe patterns used by HelmJS
				if strings.ToLower(handler) == "onclick" && strings.Contains(line, "classList.toggle") {
					continue
				}
				checks = append(checks, CheckResult{
					Category:    CategoryXSS,
					Rule:        "Avoid inline event handlers",
					Passed:      false,
					Message:     fmt.Sprintf("Inline event handler: %s", handler),
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityLow,
					Remediation: "Move event handlers to external JavaScript",
				})
			}
		}
	}

	// Check for javascript: URLs
	jsURLPattern := regexp.MustCompile(`(?i)href\s*=\s*["']javascript:`)
	for i, line := range lines {
		if jsURLPattern.MatchString(line) {
			checks = append(checks, CheckResult{
				Category:    CategoryXSS,
				Rule:        "No javascript: URLs",
				Passed:      false,
				Message:     "javascript: URL found in href",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityHigh,
				Remediation: "Use proper event handlers instead of javascript: URLs",
			})
		}
	}

	// Check for data: URLs in src attributes (potential XSS vector)
	dataURLPattern := regexp.MustCompile(`(?i)src\s*=\s*["']data:`)
	for i, line := range lines {
		if dataURLPattern.MatchString(line) {
			// Allow data:image types
			if !strings.Contains(strings.ToLower(line), "data:image/") {
				checks = append(checks, CheckResult{
					Category:    CategoryXSS,
					Rule:        "Audit data: URLs",
					Passed:      false,
					Message:     "Non-image data: URL in src attribute",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityMedium,
					Remediation: "Only allow data:image/* URLs, sanitize other data URLs",
				})
			}
		}
	}

	return checks
}

// CSRF Protection Checks for Templates
func checkTemplateCSRF(content, filePath string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Find all POST forms
	formPattern := regexp.MustCompile(`(?i)<form[^>]*method\s*=\s*["']POST["'][^>]*>`)
	// Match explicit CSRF token OR dynamic fields iteration (which includes csrf_token)
	csrfPattern := regexp.MustCompile(`(?i)name\s*=\s*["']csrf_token["']|\.CSRFToken`)
	// Dynamic fields pattern - forms iterating over .Fields include CSRF token dynamically
	dynamicFieldsPattern := regexp.MustCompile(`\{\{range\s+\.Fields\}\}`)

	inForm := false
	formStartLine := 0
	formContent := ""

	for i, line := range lines {
		if formPattern.MatchString(line) {
			inForm = true
			formStartLine = i + 1
			formContent = line
		}

		if inForm {
			formContent += line
			if strings.Contains(line, "</form>") {
				// Check if form has CSRF token (explicit or via dynamic fields)
				hasCSRF := csrfPattern.MatchString(formContent) || dynamicFieldsPattern.MatchString(formContent)
				checks = append(checks, CheckResult{
					Category:    CategoryCSRF,
					Rule:        "POST forms include CSRF token",
					Passed:      hasCSRF,
					Message:     ternary(hasCSRF, "Form has CSRF protection", "Form missing CSRF token"),
					File:        filePath,
					Line:        formStartLine,
					Severity:    ternary(hasCSRF, SeverityInfo, SeverityHigh),
					Remediation: "Add hidden input with name='csrf_token' value='{{.CSRFToken}}'",
				})
				inForm = false
				formContent = ""
			}
		}
	}

	// Check for GET forms that might be doing mutations
	getMutationPattern := regexp.MustCompile(`(?i)<form[^>]*action\s*=\s*["'][^"']*(delete|remove|update|edit|create|post|submit)[^"']*["'][^>]*>`)
	for i, line := range lines {
		if getMutationPattern.MatchString(line) && !strings.Contains(strings.ToLower(line), `method="post"`) && !strings.Contains(strings.ToLower(line), `method='post'`) {
			checks = append(checks, CheckResult{
				Category:    CategoryCSRF,
				Rule:        "Mutation operations use POST",
				Passed:      false,
				Message:     "Form with mutation action should use POST method",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityMedium,
				Remediation: "Add method='POST' to forms that modify state",
			})
		}
	}

	return checks
}

// Secrets Check for Templates
func checkTemplateSecrets(content, filePath string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Patterns that might indicate secrets in templates
	secretPatterns := []struct {
		pattern *regexp.Regexp
		name    string
	}{
		{regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*["'][^"']+["']`), "API key"},
		{regexp.MustCompile(`(?i)(secret|password|passwd|pwd)\s*[:=]\s*["'][^"']+["']`), "Secret/password"},
		{regexp.MustCompile(`(?i)private[_-]?key\s*[:=]`), "Private key"},
		{regexp.MustCompile(`nsec1[a-z0-9]{58}`), "Nostr private key (nsec)"},
		{regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_-]{20,}`), "Bearer token"},
	}

	for i, line := range lines {
		for _, sp := range secretPatterns {
			if sp.pattern.MatchString(line) {
				checks = append(checks, CheckResult{
					Category:    CategorySecrets,
					Rule:        "No hardcoded secrets in templates",
					Passed:      false,
					Message:     fmt.Sprintf("Potential %s found", sp.name),
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityCritical,
					Remediation: "Move secrets to environment variables",
				})
			}
		}
	}

	return checks
}

// Security Headers Checks for Go code
func checkGoSecurityHeaders(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult

	// Only check main.go or handler files for security headers
	if fileName != "main.go" && !strings.Contains(fileName, "handler") {
		return checks
	}

	// Check for X-Frame-Options or CSP frame-ancestors
	hasFrameProtection := strings.Contains(content, "X-Frame-Options") ||
		strings.Contains(content, "frame-ancestors")

	if fileName == "main.go" {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "Clickjacking protection (X-Frame-Options)",
			Passed:      hasFrameProtection,
			Message:     ternary(hasFrameProtection, "Frame protection header configured", "Missing X-Frame-Options or CSP frame-ancestors"),
			File:        filePath,
			Severity:    ternary(hasFrameProtection, SeverityInfo, SeverityMedium),
			Remediation: "Add X-Frame-Options: DENY or CSP frame-ancestors header",
		})
	}

	// Check for X-Content-Type-Options
	hasContentTypeOptions := strings.Contains(content, "X-Content-Type-Options")
	if fileName == "main.go" {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "MIME sniffing protection",
			Passed:      hasContentTypeOptions,
			Message:     ternary(hasContentTypeOptions, "X-Content-Type-Options configured", "Missing X-Content-Type-Options header"),
			File:        filePath,
			Severity:    ternary(hasContentTypeOptions, SeverityInfo, SeverityLow),
			Remediation: "Add X-Content-Type-Options: nosniff header",
		})
	}

	// Check for HSTS support
	hasHSTS := strings.Contains(content, "Strict-Transport-Security") ||
		strings.Contains(content, "HSTS")
	if fileName == "main.go" {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "HSTS support",
			Passed:      hasHSTS,
			Message:     ternary(hasHSTS, "HSTS configured", "No HSTS configuration found"),
			File:        filePath,
			Severity:    ternary(hasHSTS, SeverityInfo, SeverityLow),
			Remediation: "Add Strict-Transport-Security header for HTTPS deployments",
		})
	}

	// Check for Content-Security-Policy
	hasCSP := strings.Contains(content, "Content-Security-Policy")
	if fileName == "main.go" {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "Content Security Policy",
			Passed:      hasCSP,
			Message:     ternary(hasCSP, "CSP configured", "No Content-Security-Policy found"),
			File:        filePath,
			Severity:    ternary(hasCSP, SeverityInfo, SeverityMedium),
			Remediation: "Add Content-Security-Policy header to prevent XSS",
		})

		// If CSP exists, check for common weaknesses
		if hasCSP {
			checks = append(checks, checkCSPDirectives(content, filePath)...)
		}
	}

	return checks
}

// checkCSPDirectives validates CSP policy for common security weaknesses
func checkCSPDirectives(content, filePath string) []CheckResult {
	var checks []CheckResult

	// Extract CSP header value - look for patterns like:
	// w.Header().Set("Content-Security-Policy", "...")
	// "Content-Security-Policy": "..."
	cspPattern := regexp.MustCompile(`Content-Security-Policy["']?\s*[,:]\s*["']([^"']+)["']`)
	matches := cspPattern.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		// CSP is set but we can't parse the value (might be dynamic)
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "CSP policy parseable",
			Passed:      true,
			Message:     "CSP configured (policy value not statically parseable)",
			File:        filePath,
			Severity:    SeverityInfo,
			Remediation: "Ensure CSP policy includes script-src, style-src, and default-src directives",
		})
		return checks
	}

	// Analyze the CSP policy
	cspValue := matches[0][1]

	// Check for unsafe-inline in script-src (XSS vulnerability)
	if strings.Contains(cspValue, "script-src") {
		hasUnsafeInline := strings.Contains(cspValue, "'unsafe-inline'") &&
			strings.Contains(cspValue, "script-src")
		// Check if nonce or hash is used (which makes unsafe-inline acceptable)
		hasNonceOrHash := strings.Contains(cspValue, "'nonce-") || strings.Contains(cspValue, "'sha256-") ||
			strings.Contains(cspValue, "'sha384-") || strings.Contains(cspValue, "'sha512-")

		if hasUnsafeInline && !hasNonceOrHash {
			checks = append(checks, CheckResult{
				Category:    CategoryHeaders,
				Rule:        "CSP avoids unsafe-inline for scripts",
				Passed:      false,
				Message:     "CSP script-src includes 'unsafe-inline' without nonce/hash",
				File:        filePath,
				Severity:    SeverityMedium,
				Remediation: "Remove 'unsafe-inline' from script-src or use nonces/hashes",
			})
		} else {
			checks = append(checks, CheckResult{
				Category:    CategoryHeaders,
				Rule:        "CSP avoids unsafe-inline for scripts",
				Passed:      true,
				Message:     "CSP script-src properly configured",
				File:        filePath,
				Severity:    SeverityInfo,
			})
		}
	}

	// Check for unsafe-eval (allows eval(), Function(), etc.)
	hasUnsafeEval := strings.Contains(cspValue, "'unsafe-eval'")
	if hasUnsafeEval {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "CSP avoids unsafe-eval",
			Passed:      false,
			Message:     "CSP includes 'unsafe-eval' which allows eval() and similar",
			File:        filePath,
			Severity:    SeverityMedium,
			Remediation: "Remove 'unsafe-eval' from CSP - refactor code to avoid eval()",
		})
	}

	// Check for default-src directive
	hasDefaultSrc := strings.Contains(cspValue, "default-src")
	checks = append(checks, CheckResult{
		Category:    CategoryHeaders,
		Rule:        "CSP has default-src",
		Passed:      hasDefaultSrc,
		Message:     ternary(hasDefaultSrc, "CSP includes default-src fallback", "CSP missing default-src directive"),
		File:        filePath,
		Severity:    ternary(hasDefaultSrc, SeverityInfo, SeverityLow),
		Remediation: "Add default-src directive as fallback for undefined directives",
	})

	// Check for frame-ancestors (clickjacking protection via CSP)
	hasFrameAncestors := strings.Contains(cspValue, "frame-ancestors")
	if hasFrameAncestors {
		checks = append(checks, CheckResult{
			Category:    CategoryHeaders,
			Rule:        "CSP frame-ancestors",
			Passed:      true,
			Message:     "CSP includes frame-ancestors for clickjacking protection",
			File:        filePath,
			Severity:    SeverityInfo,
		})
	}

	// Check for report-uri or report-to (CSP violation reporting)
	hasReporting := strings.Contains(cspValue, "report-uri") || strings.Contains(cspValue, "report-to")
	checks = append(checks, CheckResult{
		Category:    CategoryHeaders,
		Rule:        "CSP violation reporting",
		Passed:      hasReporting,
		Message:     ternary(hasReporting, "CSP violation reporting configured", "No CSP violation reporting configured"),
		File:        filePath,
		Severity:    SeverityInfo,
		Remediation: "Consider adding report-uri or report-to for CSP violation monitoring",
	})

	return checks
}

// Session Security Checks for Go code
func checkGoSessionSecurity(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for HttpOnly cookie flag
	cookiePattern := regexp.MustCompile(`http\.Cookie\{`)
	httpOnlyPattern := regexp.MustCompile(`HttpOnly:\s*true`)
	// Match both static true and dynamic patterns like !isLocalhost(r) or shouldSecureCookie(r)
	securePattern := regexp.MustCompile(`Secure:\s*(true|!isLocalhost|!isDev|isProduction|isHTTPS|shouldSecureCookie)`)
	sameSitePattern := regexp.MustCompile(`SameSite:\s*http\.SameSite(Strict|Lax)Mode`)

	hasCookies := cookiePattern.MatchString(content)
	if hasCookies {
		hasHttpOnly := httpOnlyPattern.MatchString(content)
		checks = append(checks, CheckResult{
			Category:    CategorySession,
			Rule:        "Cookies have HttpOnly flag",
			Passed:      hasHttpOnly,
			Message:     ternary(hasHttpOnly, "HttpOnly flag set on cookies", "Cookies missing HttpOnly flag"),
			File:        filePath,
			Severity:    ternary(hasHttpOnly, SeverityInfo, SeverityMedium),
			Remediation: "Set HttpOnly: true on all session cookies",
		})

		hasSecure := securePattern.MatchString(content)
		checks = append(checks, CheckResult{
			Category:    CategorySession,
			Rule:        "Cookies have Secure flag",
			Passed:      hasSecure,
			Message:     ternary(hasSecure, "Secure flag set on cookies", "Cookies missing Secure flag"),
			File:        filePath,
			Severity:    ternary(hasSecure, SeverityInfo, SeverityMedium),
			Remediation: "Set Secure: true on cookies for HTTPS",
		})

		hasSameSite := sameSitePattern.MatchString(content)
		checks = append(checks, CheckResult{
			Category:    CategorySession,
			Rule:        "Cookies have SameSite attribute",
			Passed:      hasSameSite,
			Message:     ternary(hasSameSite, "SameSite attribute set on cookies", "Cookies missing SameSite attribute"),
			File:        filePath,
			Severity:    ternary(hasSameSite, SeverityInfo, SeverityLow),
			Remediation: "Set SameSite: http.SameSiteStrictMode or SameSiteLaxMode",
		})
	}

	// Check for session fixation (regenerate or create new session on login)
	if strings.Contains(fileName, "auth") || strings.Contains(fileName, "login") {
		// Accept regeneration OR new session creation (NIP-46 creates new sessions)
		hasSessionRegen := strings.Contains(content, "regenerate") ||
			strings.Contains(content, "NewSession") ||
			strings.Contains(content, "createSession") ||
			strings.Contains(content, "SessionStore") ||
			strings.Contains(content, "SetCookie") || // New session cookie = new session
			strings.Contains(content, "session.ID") // Using session with ID = session management

		if strings.Contains(content, "login") || strings.Contains(content, "Login") {
			checks = append(checks, CheckResult{
				Category:    CategorySession,
				Rule:        "Session regeneration on login",
				Passed:      hasSessionRegen,
				Message:     ternary(hasSessionRegen, "Session appears to be regenerated on login", "May be missing session regeneration on login"),
				File:        filePath,
				Severity:    SeverityLow,
				Remediation: "Regenerate session ID after successful authentication",
			})
		}
	}

	// Check for sensitive data in error messages
	for i, line := range lines {
		// Check for stack traces in responses
		if strings.Contains(line, "debug.Stack()") && strings.Contains(content, "http.Error") {
			checks = append(checks, CheckResult{
				Category:    CategorySession,
				Rule:        "No stack traces in responses",
				Passed:      false,
				Message:     "Stack trace may be exposed in HTTP response",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityMedium,
				Remediation: "Log stack traces server-side, return generic error to client",
			})
		}
	}

	return checks
}

// Input Validation Checks for Go code
func checkGoInputValidation(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for SQL injection (even though not using SQL, good to check)
	sqlPattern := regexp.MustCompile(`(?i)(SELECT|INSERT|UPDATE|DELETE|DROP)\s+.*\+\s*`)
	for i, line := range lines {
		if sqlPattern.MatchString(line) {
			checks = append(checks, CheckResult{
				Category:    CategoryInput,
				Rule:        "No SQL string concatenation",
				Passed:      false,
				Message:     "Potential SQL injection via string concatenation",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityCritical,
				Remediation: "Use parameterized queries",
			})
		}
	}

	// Check for command injection
	execPatterns := []string{"exec.Command", "os.StartProcess"}
	for i, line := range lines {
		for _, pattern := range execPatterns {
			if strings.Contains(line, pattern) {
				// Check if user input flows into command
				checks = append(checks, CheckResult{
					Category:    CategoryInput,
					Rule:        "Audit command execution",
					Passed:      true, // Mark as passed but needs audit
					Message:     fmt.Sprintf("Command execution found: %s - verify no user input", pattern),
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityInfo,
					Remediation: "Ensure user input is not passed to command execution",
				})
			}
		}
	}

	// Check for path traversal
	pathPatterns := regexp.MustCompile(`filepath\.Join\([^)]*r\.(URL|Form|PostForm)`)
	for i, line := range lines {
		if pathPatterns.MatchString(line) {
			checks = append(checks, CheckResult{
				Category:    CategoryInput,
				Rule:        "Path traversal protection",
				Passed:      false,
				Message:     "User input used in file path",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityHigh,
				Remediation: "Validate and sanitize file paths, use filepath.Clean",
			})
		}
	}

	// Check for open redirect
	redirectPattern := regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]+,\s*r\.(URL|Form|PostForm)`)
	for i, line := range lines {
		if redirectPattern.MatchString(line) {
			checks = append(checks, CheckResult{
				Category:    CategoryInput,
				Rule:        "Open redirect protection",
				Passed:      false,
				Message:     "User input used directly in redirect",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityMedium,
				Remediation: "Validate redirect URLs against allowlist or use relative paths",
			})
		}
	}

	// Check for URL validation on user-provided URLs
	if strings.Contains(content, "url.Parse") {
		// Check for scheme/host validation OR known safe validation functions
		hasURLValidation := (strings.Contains(content, "Scheme") || strings.Contains(content, "Host")) ||
			strings.Contains(content, "isURLSafe") ||
			strings.Contains(content, "sanitizeURL") ||
			strings.Contains(content, "sanitizeReturnURL") ||
			strings.Contains(content, "validateURL") ||
			strings.Contains(content, "IsValidURL")

		if !hasURLValidation {
			checks = append(checks, CheckResult{
				Category:    CategoryInput,
				Rule:        "URL validation",
				Passed:      false,
				Message:     "URL parsing without scheme/host validation",
				File:        filePath,
				Severity:    SeverityLow,
				Remediation: "Validate URL scheme and host after parsing",
			})
		}
	}

	return checks
}

// Secrets Check for Go code
func checkGoSecrets(content, filePath string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Skip test files
	if strings.HasSuffix(filePath, "_test.go") {
		return checks
	}

	secretPatterns := []struct {
		pattern *regexp.Regexp
		name    string
	}{
		{regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*"[^"]{10,}"`), "Hardcoded API key"},
		{regexp.MustCompile(`(?i)(secret|password)\s*[:=]\s*"[^"]{8,}"`), "Hardcoded secret"},
		{regexp.MustCompile(`nsec1[a-z0-9]{58}`), "Nostr private key"},
		{regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`), "Private key"},
		{regexp.MustCompile(`(?i)bearer\s+"[a-zA-Z0-9_-]{20,}"`), "Hardcoded bearer token"},
	}

	for i, line := range lines {
		// Skip comments and environment variable lookups
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.Contains(line, "os.Getenv") {
			continue
		}

		for _, sp := range secretPatterns {
			if sp.pattern.MatchString(line) {
				checks = append(checks, CheckResult{
					Category:    CategorySecrets,
					Rule:        "No hardcoded secrets",
					Passed:      false,
					Message:     fmt.Sprintf("Potential %s", sp.name),
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityCritical,
					Remediation: "Use environment variables for secrets",
				})
			}
		}
	}

	// Check for proper environment variable usage for common secrets
	envSecrets := []string{"CSRF_SECRET", "GIPHY_API_KEY", "REDIS_URL"}
	for _, secret := range envSecrets {
		if strings.Contains(content, secret) && !strings.Contains(content, fmt.Sprintf(`os.Getenv("%s")`, secret)) {
			// Check if it's being used properly
			if strings.Contains(content, fmt.Sprintf(`"%s"`, secret)) && !strings.Contains(content, "os.Getenv") {
				checks = append(checks, CheckResult{
					Category:    CategorySecrets,
					Rule:        "Secrets loaded from environment",
					Passed:      false,
					Message:     fmt.Sprintf("%s should be loaded via os.Getenv", secret),
					File:        filePath,
					Severity:    SeverityMedium,
					Remediation: fmt.Sprintf("Use os.Getenv(\"%s\") to load secret", secret),
				})
			}
		}
	}

	return checks
}

// Nostr-specific Security Checks
func checkGoNostrSecurity(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for event signature verification
	if strings.Contains(content, "nostr.Event") || strings.Contains(content, "Event{") {
		hasSigVerification := strings.Contains(content, "CheckSignature") ||
			strings.Contains(content, "Verify") ||
			strings.Contains(content, "checkSig")

		// Only flag if file handles events but doesn't verify
		if strings.Contains(content, "event.PubKey") || strings.Contains(content, "event.Sig") {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "Event signature verification",
				Passed:      hasSigVerification,
				Message:     ternary(hasSigVerification, "Signature verification found", "Events may not be verified before trust"),
				File:        filePath,
				Severity:    ternary(hasSigVerification, SeverityInfo, SeverityHigh),
				Remediation: "Verify event signatures before trusting event data",
			})
		}
	}

	// Check for relay URL validation - user input
	relayURLPattern := regexp.MustCompile(`wss?://[^"'\s]+`)
	for i, line := range lines {
		if relayURLPattern.MatchString(line) {
			// Check if URL comes from user input
			if strings.Contains(line, "r.URL") || strings.Contains(line, "r.Form") {
				checks = append(checks, CheckResult{
					Category:    CategoryNostr,
					Rule:        "Relay URL validation",
					Passed:      false,
					Message:     "Relay URL may come from user input",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityMedium,
					Remediation: "Validate relay URLs against allowlist or verify format",
				})
			}
		}
	}

	// Check for insecure ws:// relay connections (should use wss://)
	insecureRelayPattern := regexp.MustCompile(`["']ws://[^"']+["']`)
	for i, line := range lines {
		// Skip comments
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if insecureRelayPattern.MatchString(line) {
			// Allow localhost for development
			if strings.Contains(line, "localhost") || strings.Contains(line, "127.0.0.1") {
				continue
			}
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "Secure relay connections (wss://)",
				Passed:      false,
				Message:     "Insecure ws:// relay connection found - use wss://",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityMedium,
				Remediation: "Use wss:// for relay connections to encrypt traffic",
			})
		}
	}

	// Check for NIP-04 usage (deprecated, has security weaknesses)
	hasNIP04 := strings.Contains(content, "nip04") || strings.Contains(content, "NIP04") ||
		strings.Contains(content, "nip-04") || strings.Contains(content, "NIP-04")
	if hasNIP04 {
		// Check if NIP-44 is also present (migration in progress is ok)
		hasNIP44 := strings.Contains(content, "nip44") || strings.Contains(content, "NIP44") ||
			strings.Contains(content, "nip-44") || strings.Contains(content, "NIP-44")

		if hasNIP44 {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NIP-04 deprecation",
				Passed:      true,
				Message:     "NIP-04 found but NIP-44 also present - ensure migration to NIP-44",
				File:        filePath,
				Severity:    SeverityInfo,
				Remediation: "Complete migration from NIP-04 to NIP-44 encryption",
			})
		} else {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NIP-04 deprecation",
				Passed:      false,
				Message:     "NIP-04 encryption in use - has known security weaknesses",
				File:        filePath,
				Severity:    SeverityMedium,
				Remediation: "Migrate to NIP-44 encryption (better padding, authenticated encryption)",
			})
		}
	}

	// Check for NIP-46 (remote signing) security
	// Only check files that actually implement NIP-46 (have specific NIP-46 types/patterns)
	hasNIP46Impl := strings.Contains(content, "NIP46Session") || strings.Contains(content, "nip46Session") ||
		strings.Contains(content, "bunker://") || strings.Contains(content, "nostrconnect://") ||
		strings.Contains(content, "RemoteSigner") || strings.Contains(content, "remoteSigner") ||
		(strings.Contains(content, "kind") && strings.Contains(content, "24133"))
	if hasNIP46Impl {
		// Check that actual secrets aren't logged (not just any mention of "secret")
		secretsLogged := false
		for i, line := range lines {
			// Skip comments
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			isLogging := strings.Contains(line, "log.") || strings.Contains(line, "fmt.Print") ||
				strings.Contains(line, "slog.")
			// Look for actual secret variables being logged, not just the word "secret"
			hasSecretVar := strings.Contains(line, "nsec1") || strings.Contains(line, "privKey") ||
				strings.Contains(line, "PrivKey") || strings.Contains(line, "privateKey") ||
				strings.Contains(line, "secretKey") || strings.Contains(line, "SecretKey") ||
				(strings.Contains(line, "bunkerSecret") || strings.Contains(line, "sessionSecret") &&
					!strings.Contains(line, "sessionSecretKey")) // Allow key references
			if isLogging && hasSecretVar {
				secretsLogged = true
				checks = append(checks, CheckResult{
					Category:    CategoryNostr,
					Rule:        "NIP-46 secrets not logged",
					Passed:      false,
					Message:     "Potential logging of NIP-46 secret material",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityHigh,
					Remediation: "Never log private keys, secrets, or NIP-46 credentials",
				})
			}
		}

		// Always report NIP-46 status (pass or fail)
		if !secretsLogged {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NIP-46 secrets not logged",
				Passed:      true,
				Message:     "NIP-46 remote signing in use - no secret logging detected",
				File:        filePath,
				Severity:    SeverityInfo,
				Remediation: "Continue to ensure private keys never touch server logs",
			})
		}
	}

	// Check for NIP-44 encryption usage
	hasNIP44 := strings.Contains(content, "nip44") || strings.Contains(content, "NIP44") ||
		strings.Contains(content, "nip-44") || strings.Contains(content, "NIP-44")
	if hasNIP44 {
		hasEncrypt := strings.Contains(content, "Encrypt")
		hasDecrypt := strings.Contains(content, "Decrypt")

		if hasEncrypt || hasDecrypt {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NIP-44 encryption audit",
				Passed:      true,
				Message:     "NIP-44 encryption in use - modern authenticated encryption",
				File:        filePath,
				Severity:    SeverityInfo,
				Remediation: "Ensure encryption keys are properly derived and stored",
			})
		}
	}

	// Check for NWC (Nostr Wallet Connect / NIP-47) security
	// Only check files that actually implement NWC (have nwc_uri or NWCClient)
	hasNWCImpl := strings.Contains(content, "nwc_uri") || strings.Contains(content, "NWCUri") ||
		strings.Contains(content, "NWCClient") || strings.Contains(content, "nwcClient") ||
		strings.Contains(content, "nostr+walletconnect")
	if hasNWCImpl {
		// Check for NWC secret logging - only flag if actual secrets are logged
		nwcSecretsLogged := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// Check for logging actual wallet secrets (connection string, secret key)
			// Not just any log that mentions NWC
			isLogging := strings.Contains(line, "log.") || strings.Contains(line, "fmt.Print") ||
				strings.Contains(line, "slog.")
			hasSecretVar := strings.Contains(line, "nwc_uri") || strings.Contains(line, "NWCUri") ||
				strings.Contains(line, "walletSecret") || strings.Contains(line, "WalletSecret") ||
				strings.Contains(line, "connectionString") || strings.Contains(line, "nostr+walletconnect")
			if isLogging && hasSecretVar {
				nwcSecretsLogged = true
				checks = append(checks, CheckResult{
					Category:    CategoryNostr,
					Rule:        "NWC secrets not logged",
					Passed:      false,
					Message:     "Potential logging of NWC wallet secret",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityHigh,
					Remediation: "Never log NWC connection strings or wallet secrets",
				})
			}
		}

		if !nwcSecretsLogged {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NWC secrets not logged",
				Passed:      true,
				Message:     "NWC wallet integration found - no secret logging detected",
				File:        filePath,
				Severity:    SeverityInfo,
				Remediation: "Continue to protect NWC connection strings",
			})
		}

		// Check that NWC URI is stored securely (in session/cookie, not plaintext)
		if strings.Contains(content, "nwc_uri") || strings.Contains(content, "NWCUri") {
			hasSecureStorage := strings.Contains(content, "session") || strings.Contains(content, "Session") ||
				strings.Contains(content, "cookie") || strings.Contains(content, "Cookie") ||
				strings.Contains(content, "encrypt") || strings.Contains(content, "Encrypt")
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "NWC URI secure storage",
				Passed:      hasSecureStorage,
				Message:     ternary(hasSecureStorage, "NWC URI appears to use secure storage", "NWC URI storage may not be secure"),
				File:        filePath,
				Severity:    ternary(hasSecureStorage, SeverityInfo, SeverityMedium),
				Remediation: "Store NWC URIs in encrypted session storage, not plaintext",
			})
		}
	}

	// Check for zap (NIP-57) receipt verification at ingestion layer
	// Only flag relay/ingestion code - display code (html.go, kinds_appliers.go) should receive
	// already-verified data, not do verification itself
	isIngestionCode := strings.Contains(fileName, "relay") || strings.Contains(fileName, "fetch") ||
		strings.Contains(fileName, "subscribe") || strings.Contains(fileName, "ingest")

	// Check if this file fetches zap events from relays (ingestion point)
	fetchesZapEvents := isIngestionCode &&
		(strings.Contains(content, "9735") || strings.Contains(content, "kind:9735") ||
			strings.Contains(content, "Kind: 9735"))

	if fetchesZapEvents {
		// Check for zap receipt verification at ingestion
		hasZapVerification := strings.Contains(content, "bolt11") || strings.Contains(content, "Bolt11") ||
			strings.Contains(content, "CheckSignature") || strings.Contains(content, "Verify") ||
			(strings.Contains(content, "parseBolt11") || strings.Contains(content, "ParseBolt11"))

		checks = append(checks, CheckResult{
			Category:    CategoryNostr,
			Rule:        "Zap receipt verification",
			Passed:      hasZapVerification,
			Message:     ternary(hasZapVerification, "Zap verification logic found at ingestion layer", "Zap receipts may not be fully verified at ingestion"),
			File:        filePath,
			Severity:    ternary(hasZapVerification, SeverityInfo, SeverityLow),
			Remediation: "Verify zap receipt signatures and bolt11 amounts at relay ingestion, not display layer",
		})
	}

	// Check for private key handling beyond just nsec detection
	if strings.Contains(content, "PrivateKey") || strings.Contains(content, "privateKey") ||
		strings.Contains(content, "privkey") || strings.Contains(content, "SecretKey") {
		// Check for proper key zeroing after use
		hasKeyZeroing := strings.Contains(content, "= nil") || strings.Contains(content, "clear") ||
			strings.Contains(content, "Clear") || strings.Contains(content, "zero") ||
			strings.Contains(content, "Zero")

		// Only flag if file actually handles keys (not just references)
		if strings.Contains(content, "GeneratePrivateKey") || strings.Contains(content, "DecodePrivateKey") ||
			strings.Contains(content, "ParsePrivKey") {
			checks = append(checks, CheckResult{
				Category:    CategoryNostr,
				Rule:        "Private key memory safety",
				Passed:      hasKeyZeroing,
				Message:     ternary(hasKeyZeroing, "Key zeroing patterns found", "Private keys may not be cleared from memory after use"),
				File:        filePath,
				Severity:    ternary(hasKeyZeroing, SeverityInfo, SeverityLow),
				Remediation: "Zero private key memory after use to prevent memory disclosure",
			})
		}
	}

	return checks
}

// Rate Limiting Checks
func checkGoRateLimiting(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult

	// Check for rate limiting in auth-related files
	if strings.Contains(fileName, "auth") || strings.Contains(fileName, "login") ||
		strings.Contains(fileName, "handler") {

		hasRateLimit := strings.Contains(content, "RateLimit") ||
			strings.Contains(content, "rateLimit") ||
			strings.Contains(content, "rate_limit") ||
			strings.Contains(content, "rateLimiter") ||
			strings.Contains(content, "x/time/rate")

		// Only check files that have POST handlers
		if strings.Contains(content, `r.Method == "POST"`) || strings.Contains(content, `r.Method == http.MethodPost`) {
			checks = append(checks, CheckResult{
				Category:    CategoryRateLimit,
				Rule:        "Rate limiting on mutations",
				Passed:      hasRateLimit,
				Message:     ternary(hasRateLimit, "Rate limiting appears configured", "No rate limiting detected for POST handlers"),
				File:        filePath,
				Severity:    ternary(hasRateLimit, SeverityInfo, SeverityMedium),
				Remediation: "Add rate limiting to prevent abuse of mutation endpoints",
			})
		}
	}

	return checks
}

// SSRF Prevention Checks
func checkGoSSRF(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check files that make HTTP requests (potential SSRF vectors)
	makesHTTPRequests := strings.Contains(content, "http.Get") ||
		strings.Contains(content, "http.Post") ||
		strings.Contains(content, "http.Do") ||
		strings.Contains(content, "http.Client") ||
		strings.Contains(content, "http.NewRequest")

	if !makesHTTPRequests {
		return checks
	}

	// Check for private IP blocking
	hasPrivateIPBlocking := strings.Contains(content, "127.") ||
		strings.Contains(content, "10.") ||
		strings.Contains(content, "192.168") ||
		strings.Contains(content, "169.254") ||
		strings.Contains(content, "172.16") ||
		strings.Contains(content, "isPrivate") ||
		strings.Contains(content, "IsPrivate") ||
		strings.Contains(content, "privateIP") ||
		strings.Contains(content, "PrivateIP") ||
		strings.Contains(content, "isLocalhost") ||
		strings.Contains(content, "localhost")

	// Check for redirect following limits
	hasRedirectControl := strings.Contains(content, "CheckRedirect") ||
		strings.Contains(content, "MaxRedirects") ||
		strings.Contains(content, "redirects") ||
		strings.Contains(content, "FollowRedirects")

	// Check for timeout configuration
	hasTimeout := strings.Contains(content, "Timeout") ||
		strings.Contains(content, "timeout") ||
		strings.Contains(content, "context.WithTimeout") ||
		strings.Contains(content, "time.Second") ||
		strings.Contains(content, "time.Millisecond")

	// Only flag files that fetch external URLs (like link_preview.go)
	fetchesExternalURLs := strings.Contains(fileName, "preview") ||
		strings.Contains(fileName, "fetch") ||
		strings.Contains(fileName, "crawler") ||
		strings.Contains(content, "fetchURL") ||
		strings.Contains(content, "FetchURL") ||
		strings.Contains(content, "getURL") ||
		strings.Contains(content, "GetURL")

	if fetchesExternalURLs {
		// Private IP blocking check
		checks = append(checks, CheckResult{
			Category:    CategorySSRF,
			Rule:        "Private IP blocking",
			Passed:      hasPrivateIPBlocking,
			Message:     ternary(hasPrivateIPBlocking, "Private IP/localhost checks found", "No private IP blocking detected for external URL fetching"),
			File:        filePath,
			Severity:    ternary(hasPrivateIPBlocking, SeverityInfo, SeverityHigh),
			Remediation: "Block requests to private IPs (127.x, 10.x, 192.168.x, 169.254.x, 172.16-31.x)",
		})

		// Redirect control check
		checks = append(checks, CheckResult{
			Category:    CategorySSRF,
			Rule:        "Redirect following limits",
			Passed:      hasRedirectControl,
			Message:     ternary(hasRedirectControl, "Redirect control configured", "No redirect limits detected"),
			File:        filePath,
			Severity:    ternary(hasRedirectControl, SeverityInfo, SeverityMedium),
			Remediation: "Limit redirect following to prevent SSRF via redirects",
		})

		// Timeout check
		checks = append(checks, CheckResult{
			Category:    CategorySSRF,
			Rule:        "Request timeout",
			Passed:      hasTimeout,
			Message:     ternary(hasTimeout, "Request timeout configured", "No timeout detected for external requests"),
			File:        filePath,
			Severity:    ternary(hasTimeout, SeverityInfo, SeverityMedium),
			Remediation: "Set timeouts on HTTP requests to prevent hanging connections",
		})
	}

	// Check for user input flowing to HTTP requests
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Check for URL from user input going directly to HTTP request
		if (strings.Contains(line, "http.Get(") || strings.Contains(line, "http.Post(")) &&
			(strings.Contains(line, "r.URL") || strings.Contains(line, "r.Form") ||
				strings.Contains(line, "r.PostForm") || strings.Contains(line, "query.Get")) {
			checks = append(checks, CheckResult{
				Category:    CategorySSRF,
				Rule:        "User input in HTTP requests",
				Passed:      false,
				Message:     "User input may flow directly to HTTP request",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityHigh,
				Remediation: "Validate and sanitize URLs before making HTTP requests",
			})
		}
	}

	return checks
}

// Cryptography Checks
func checkGoCryptography(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for math/rand usage in security contexts (should use crypto/rand)
	hasMathRand := strings.Contains(content, "math/rand") ||
		strings.Contains(content, `"rand"`) && !strings.Contains(content, "crypto/rand")
	hasCryptoRand := strings.Contains(content, "crypto/rand")

	// Check if file does security-sensitive operations
	isSecuritySensitive := strings.Contains(content, "token") || strings.Contains(content, "Token") ||
		strings.Contains(content, "secret") || strings.Contains(content, "Secret") ||
		strings.Contains(content, "key") || strings.Contains(content, "Key") ||
		strings.Contains(content, "session") || strings.Contains(content, "Session") ||
		strings.Contains(content, "csrf") || strings.Contains(content, "CSRF") ||
		strings.Contains(content, "random") || strings.Contains(content, "Random") ||
		strings.Contains(content, "nonce") || strings.Contains(content, "Nonce")

	if hasMathRand && isSecuritySensitive {
		// Check if crypto/rand is also imported (acceptable if both present)
		if !hasCryptoRand {
			checks = append(checks, CheckResult{
				Category:    CategoryCrypto,
				Rule:        "Cryptographic randomness",
				Passed:      false,
				Message:     "math/rand used in security-sensitive file - not cryptographically secure",
				File:        filePath,
				Severity:    SeverityHigh,
				Remediation: "Use crypto/rand for security-sensitive random number generation",
			})
		} else {
			checks = append(checks, CheckResult{
				Category:    CategoryCrypto,
				Rule:        "Cryptographic randomness",
				Passed:      true,
				Message:     "Both math/rand and crypto/rand present - verify crypto/rand used for security",
				File:        filePath,
				Severity:    SeverityInfo,
				Remediation: "Ensure crypto/rand is used for all security-sensitive operations",
			})
		}
	} else if hasCryptoRand && isSecuritySensitive {
		checks = append(checks, CheckResult{
			Category:    CategoryCrypto,
			Rule:        "Cryptographic randomness",
			Passed:      true,
			Message:     "crypto/rand used for secure random generation",
			File:        filePath,
			Severity:    SeverityInfo,
		})
	}

	// Check for weak hash algorithms in security contexts
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// MD5 usage
		if strings.Contains(line, "md5.") || strings.Contains(line, "crypto/md5") {
			// Check if it's for non-security purposes (caching, checksums are ok)
			isSecurityUse := strings.Contains(content, "password") || strings.Contains(content, "Password") ||
				strings.Contains(content, "signature") || strings.Contains(content, "Signature") ||
				strings.Contains(content, "verify") || strings.Contains(content, "Verify")
			if isSecurityUse {
				checks = append(checks, CheckResult{
					Category:    CategoryCrypto,
					Rule:        "Avoid weak hash algorithms",
					Passed:      false,
					Message:     "MD5 used in potentially security-sensitive context",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityMedium,
					Remediation: "Use SHA-256 or stronger for security purposes",
				})
			}
		}

		// SHA1 usage in security contexts
		if strings.Contains(line, "sha1.") || strings.Contains(line, "crypto/sha1") {
			isSecurityUse := strings.Contains(content, "password") || strings.Contains(content, "Password") ||
				strings.Contains(content, "signature") || strings.Contains(content, "Signature")
			if isSecurityUse {
				checks = append(checks, CheckResult{
					Category:    CategoryCrypto,
					Rule:        "Avoid weak hash algorithms",
					Passed:      false,
					Message:     "SHA1 used in potentially security-sensitive context",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityMedium,
					Remediation: "Use SHA-256 or stronger for security purposes",
				})
			}
		}
	}

	// Check for hardcoded IVs or salts
	hardcodedPattern := regexp.MustCompile(`(?i)(iv|salt|nonce)\s*[:=]\s*\[\]byte\{[^}]+\}`)
	for i, line := range lines {
		if hardcodedPattern.MatchString(line) {
			checks = append(checks, CheckResult{
				Category:    CategoryCrypto,
				Rule:        "No hardcoded IVs/salts",
				Passed:      false,
				Message:     "Hardcoded IV, salt, or nonce detected",
				File:        filePath,
				Line:        i + 1,
				Severity:    SeverityHigh,
				Remediation: "Generate IVs and salts randomly for each operation",
			})
		}
	}

	return checks
}

// Information Disclosure Checks
func checkGoInfoDisclosure(content, filePath, fileName string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	// Check for debug mode flags that could leak info in production
	hasDebugMode := strings.Contains(content, "DEBUG") ||
		strings.Contains(content, "DEV_MODE") || strings.Contains(content, "devMode")

	if hasDebugMode {
		// Check if debug mode is properly controlled via environment
		hasEnvControl := strings.Contains(content, "os.Getenv") &&
			(strings.Contains(content, "DEBUG") || strings.Contains(content, "DEV_MODE"))

		checks = append(checks, CheckResult{
			Category:    CategoryInfoLeak,
			Rule:        "Debug mode control",
			Passed:      hasEnvControl,
			Message:     ternary(hasEnvControl, "Debug mode controlled via environment variable", "Debug mode may not be properly controlled"),
			File:        filePath,
			Severity:    ternary(hasEnvControl, SeverityInfo, SeverityMedium),
			Remediation: "Control debug mode via environment variables, disable in production",
		})
	}

	// Check for verbose error messages exposing internals
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Check for error messages containing file paths or internal details
		if strings.Contains(line, "http.Error") || strings.Contains(line, "fmt.Fprintf(w,") {
			if strings.Contains(line, "err.Error()") || strings.Contains(line, "%v", ) ||
				strings.Contains(line, "%+v") || strings.Contains(line, "%#v") {
				// Check if it's in a debug/dev context
				isDebugContext := false
				if i > 0 {
					prevLines := strings.Join(lines[max(0, i-3):i], "\n")
					isDebugContext = strings.Contains(prevLines, "debug") ||
						strings.Contains(prevLines, "DEBUG") ||
						strings.Contains(prevLines, "DEV_MODE")
				}
				if !isDebugContext {
					checks = append(checks, CheckResult{
						Category:    CategoryInfoLeak,
						Rule:        "Verbose error messages",
						Passed:      false,
						Message:     "Error details may be exposed to users",
						File:        filePath,
						Line:        i + 1,
						Severity:    SeverityLow,
						Remediation: "Log detailed errors server-side, return generic messages to users",
					})
				}
			}
		}
	}

	// Check for stack trace exposure
	if strings.Contains(content, "debug.Stack()") {
		for i, line := range lines {
			if strings.Contains(line, "debug.Stack()") {
				// Check if it's being written to response
				nearbyCode := ""
				if i+5 < len(lines) {
					nearbyCode = strings.Join(lines[i:i+5], "\n")
				}
				if strings.Contains(nearbyCode, "http.Error") || strings.Contains(nearbyCode, "Write(") ||
					strings.Contains(nearbyCode, "Fprintf(w") {
					checks = append(checks, CheckResult{
						Category:    CategoryInfoLeak,
						Rule:        "Stack trace exposure",
						Passed:      false,
						Message:     "Stack trace may be exposed in HTTP response",
						File:        filePath,
						Line:        i + 1,
						Severity:    SeverityMedium,
						Remediation: "Log stack traces server-side only, never send to clients",
					})
				}
			}
		}
	}

	// Check for version/server info exposure
	if strings.Contains(content, "X-Powered-By") || strings.Contains(content, "Server:") {
		for i, line := range lines {
			if strings.Contains(line, "X-Powered-By") ||
				(strings.Contains(line, `"Server"`) && strings.Contains(line, "Header")) {
				checks = append(checks, CheckResult{
					Category:    CategoryInfoLeak,
					Rule:        "Server version disclosure",
					Passed:      false,
					Message:     "Server/technology version may be disclosed in headers",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityLow,
					Remediation: "Remove or obscure X-Powered-By and Server headers",
				})
			}
		}
	}

	// Check for source path disclosure in error messages
	sourcePathPattern := regexp.MustCompile(`(?i)(\/home\/|\/var\/|\/usr\/|\/etc\/|C:\\|\.go:\d+)`)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Only flag if it's in a string that might be sent to users
		if (strings.Contains(line, `"`) || strings.Contains(line, "`")) &&
			sourcePathPattern.MatchString(line) &&
			(strings.Contains(line, "Error") || strings.Contains(line, "error") ||
				strings.Contains(line, "Message") || strings.Contains(line, "message")) {
			// Skip if it's clearly a log statement
			if !strings.Contains(line, "log.") && !strings.Contains(line, "slog.") {
				checks = append(checks, CheckResult{
					Category:    CategoryInfoLeak,
					Rule:        "Source path disclosure",
					Passed:      false,
					Message:     "File paths may be exposed in error messages",
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityLow,
					Remediation: "Avoid exposing internal file paths in user-facing messages",
				})
			}
		}
	}

	return checks
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Config file secrets check
func checkConfigSecrets(content, filePath string) []CheckResult {
	var checks []CheckResult
	lines := strings.Split(content, "\n")

	secretPatterns := []struct {
		pattern *regexp.Regexp
		name    string
	}{
		{regexp.MustCompile(`"(api[_-]?key|apikey)"\s*:\s*"[^"]{10,}"`), "API key in config"},
		{regexp.MustCompile(`"(secret|password)"\s*:\s*"[^"]{8,}"`), "Secret in config"},
		{regexp.MustCompile(`"(private[_-]?key)"\s*:\s*"[^"]+"`), "Private key in config"},
		{regexp.MustCompile(`nsec1[a-z0-9]{58}`), "Nostr private key in config"},
	}

	for i, line := range lines {
		for _, sp := range secretPatterns {
			if sp.pattern.MatchString(line) {
				checks = append(checks, CheckResult{
					Category:    CategorySecrets,
					Rule:        "No secrets in config files",
					Passed:      false,
					Message:     fmt.Sprintf("Potential %s", sp.name),
					File:        filePath,
					Line:        i + 1,
					Severity:    SeverityCritical,
					Remediation: "Move secrets to environment variables",
				})
			}
		}
	}

	return checks
}

// Cross-file checks that need to look at multiple files
func runCrossFileChecks(projectPath string, goFiles, templateFiles []string) FileAnalysis {
	analysis := FileAnalysis{
		File:   "Cross-file Analysis",
		Checks: []CheckResult{},
	}

	// Check that CSRF validation exists for POST routes
	var hasCSRFValidation bool
	var hasPostRoutes bool

	for _, file := range goFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fileContent := string(content)

		if strings.Contains(fileContent, "validateCSRF") || strings.Contains(fileContent, "ValidateCSRF") ||
			strings.Contains(fileContent, "csrfToken") || strings.Contains(fileContent, "csrf.Validate") {
			hasCSRFValidation = true
		}

		if strings.Contains(fileContent, `r.Method == "POST"`) || strings.Contains(fileContent, `http.MethodPost`) {
			hasPostRoutes = true
		}
	}

	if hasPostRoutes {
		analysis.Checks = append(analysis.Checks, CheckResult{
			Category:    CategoryCSRF,
			Rule:        "CSRF validation middleware",
			Passed:      hasCSRFValidation,
			Message:     ternary(hasCSRFValidation, "CSRF validation found in codebase", "No CSRF validation found for POST routes"),
			File:        "*.go",
			Severity:    ternary(hasCSRFValidation, SeverityInfo, SeverityHigh),
			Remediation: "Implement CSRF token validation for all POST requests",
		})
	}

	// Check for authentication middleware
	var hasAuthMiddleware bool
	for _, file := range goFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fileContent := string(content)

		if strings.Contains(fileContent, "authMiddleware") || strings.Contains(fileContent, "AuthMiddleware") ||
			strings.Contains(fileContent, "requireAuth") || strings.Contains(fileContent, "RequireAuth") ||
			strings.Contains(fileContent, "isLoggedIn") {
			hasAuthMiddleware = true
			break
		}
	}

	analysis.Checks = append(analysis.Checks, CheckResult{
		Category:    CategorySession,
		Rule:        "Authentication middleware",
		Passed:      hasAuthMiddleware,
		Message:     ternary(hasAuthMiddleware, "Auth middleware found", "No authentication middleware detected"),
		File:        "*.go",
		Severity:    ternary(hasAuthMiddleware, SeverityInfo, SeverityMedium),
		Remediation: "Implement authentication middleware for protected routes",
	})

	// Check for error handling middleware
	var hasErrorHandling bool
	for _, file := range goFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fileContent := string(content)

		if strings.Contains(fileContent, "recover()") || strings.Contains(fileContent, "errorHandler") ||
			strings.Contains(fileContent, "panicHandler") {
			hasErrorHandling = true
			break
		}
	}

	analysis.Checks = append(analysis.Checks, CheckResult{
		Category:    CategorySession,
		Rule:        "Panic recovery",
		Passed:      hasErrorHandling,
		Message:     ternary(hasErrorHandling, "Panic recovery found", "No panic recovery middleware"),
		File:        "*.go",
		Severity:    ternary(hasErrorHandling, SeverityInfo, SeverityLow),
		Remediation: "Add recover() middleware to prevent stack traces in responses",
	})

	return analysis
}

func calculateSummary(report *Report) {
	categories := map[string]*CategorySummary{}

	for _, file := range report.Files {
		for _, check := range file.Checks {
			if _, ok := categories[check.Category]; !ok {
				categories[check.Category] = &CategorySummary{}
			}
			cat := categories[check.Category]
			cat.Total++
			if check.Passed {
				cat.Passed++
			} else {
				cat.Failed++
				// Count by severity
				switch check.Severity {
				case SeverityCritical:
					report.Critical++
				case SeverityHigh:
					report.High++
				case SeverityMedium:
					report.Medium++
				case SeverityLow:
					report.Low++
				}
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

func ternary(condition bool, trueVal, falseVal string) string {
	if condition {
		return trueVal
	}
	return falseVal
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func generateHTMLReport(report *Report, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Calculate grade based on severity-weighted score
	// Critical issues heavily penalize the score
	weightedScore := report.TotalScore
	if report.Critical > 0 {
		weightedScore = min(weightedScore, 30) // Cap at 30% if any critical
	}
	if report.High > 0 {
		weightedScore = min(weightedScore, 60) // Cap at 60% if any high
	}

	grade := "F"
	switch {
	case weightedScore >= 95:
		grade = "A+"
	case weightedScore >= 90:
		grade = "A"
	case weightedScore >= 85:
		grade = "A-"
	case weightedScore >= 80:
		grade = "B+"
	case weightedScore >= 75:
		grade = "B"
	case weightedScore >= 70:
		grade = "B-"
	case weightedScore >= 65:
		grade = "C+"
	case weightedScore >= 60:
		grade = "C"
	case weightedScore >= 55:
		grade = "C-"
	case weightedScore >= 50:
		grade = "D"
	}

	scoreColor := "#22c55e" // green
	if weightedScore < 70 {
		scoreColor = "#f59e0b" // amber
	}
	if weightedScore < 50 {
		scoreColor = "#ef4444" // red
	}

	fmt.Fprintf(f, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Security Report</title>
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
            --critical: #ff6b6b;
            --high: #ff9f43;
            --medium: #feca57;
            --low: #48dbfb;
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

        .severity-badges {
            display: flex;
            gap: 1rem;
            margin-top: 1rem;
        }
        .severity-badge {
            padding: 0.5rem 1rem;
            border-radius: 6px;
            font-weight: bold;
            font-size: 0.9rem;
        }
        .severity-critical { background: rgba(255,107,107,0.2); color: var(--critical); }
        .severity-high { background: rgba(255,159,67,0.2); color: var(--high); }
        .severity-medium { background: rgba(254,202,87,0.2); color: var(--medium); }
        .severity-low { background: rgba(72,219,251,0.2); color: var(--low); }

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
        .category-stats {
            margin-top: 0.5rem;
            color: var(--text-muted);
            font-size: 0.8rem;
        }

        .file-section {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            margin-bottom: 1rem;
            overflow: hidden;
        }
        .file-header {
            padding: 1rem;
            border-bottom: 1px solid var(--border);
            cursor: pointer;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .file-header:hover { background: rgba(255,255,255,0.02); }
        .file-name { font-family: monospace; color: var(--blue); }
        .file-stats { display: flex; gap: 0.5rem; }
        .stat { padding: 0.2rem 0.5rem; border-radius: 4px; font-size: 0.8rem; }
        .stat-pass { background: rgba(35,134,54,0.2); color: var(--green); }
        .stat-fail { background: rgba(218,54,51,0.2); color: var(--red); }

        .checks-list {
            padding: 0;
            display: none;
        }
        .file-section.open .checks-list { display: block; }
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
        .check-remediation {
            color: var(--blue);
            font-size: 0.85rem;
            margin-top: 0.25rem;
            font-style: italic;
        }
        .check-location { color: var(--text-muted); font-size: 0.8rem; font-family: monospace; }
        .check-severity {
            font-size: 0.7rem;
            padding: 0.15rem 0.5rem;
            border-radius: 3px;
            text-transform: uppercase;
            font-weight: bold;
        }
        .sev-critical { background: var(--critical); color: white; }
        .sev-high { background: var(--high); color: black; }
        .sev-medium { background: var(--medium); color: black; }
        .sev-low { background: var(--low); color: black; }
        .sev-info { background: var(--border); color: var(--text-muted); }

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

        .link { color: var(--blue); text-decoration: none; }
        .link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Security Report</h1>
        <p class="meta">Generated: %s | Files analyzed: %d</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: %s; color: %s;">
                %s
                <span class="score-label">%.0f%%</span>
            </div>
            <div class="score-details">
                <h3>Security Score</h3>
                <p>Static analysis of source code for common security vulnerabilities.</p>
                <div class="severity-badges">
                    <span class="severity-badge severity-critical">%d Critical</span>
                    <span class="severity-badge severity-high">%d High</span>
                    <span class="severity-badge severity-medium">%d Medium</span>
                    <span class="severity-badge severity-low">%d Low</span>
                </div>
            </div>
        </div>

        <h2>Security Categories</h2>
        <div class="category-grid">
`,
		report.GeneratedAt.Format("2006-01-02 15:04:05"),
		len(report.Files),
		scoreColor, scoreColor,
		grade,
		weightedScore,
		report.Critical, report.High, report.Medium, report.Low,
	)

	// Sort categories for consistent output
	categories := []string{CategoryXSS, CategoryCSRF, CategoryHeaders, CategorySecrets, CategorySession, CategoryInput, CategoryNostr, CategoryRateLimit, CategorySSRF, CategoryCrypto, CategoryInfoLeak}
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
                <p class="category-stats">%d passed / %d total checks</p>
            </div>
`, cat, color, summary.Score, summary.Score, color, summary.Passed, summary.Total)
		}
	}

	fmt.Fprintf(f, `
        </div>

        <h2>Detailed Findings</h2>
        <button class="toggle-btn" onclick="document.querySelectorAll('.file-section').forEach(s => s.classList.toggle('open'))">
            Toggle All
        </button>
`)

	// Group all checks by category
	checksByCategory := make(map[string][]CheckResult)
	for _, file := range report.Files {
		for _, check := range file.Checks {
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
        <div class="file-section%s">
            <div class="file-header" onclick="this.parentElement.classList.toggle('open')">
                <span class="file-name">%s</span>
                <div class="file-stats">
                    <span class="stat stat-pass">%d passed</span>
                    <span class="stat stat-fail">%d failed</span>
                </div>
            </div>
            <div class="checks-list">
`, openClass, cat, passed, failed)

		// Sort checks: failures first, then by severity, then by file
		sort.Slice(checks, func(i, j int) bool {
			if checks[i].Passed != checks[j].Passed {
				return !checks[i].Passed
			}
			// Sort by severity
			sevOrder := map[string]int{
				SeverityCritical: 0,
				SeverityHigh:     1,
				SeverityMedium:   2,
				SeverityLow:      3,
				SeverityInfo:     4,
			}
			if sevOrder[checks[i].Severity] != sevOrder[checks[j].Severity] {
				return sevOrder[checks[i].Severity] < sevOrder[checks[j].Severity]
			}
			// Then by file
			return checks[i].File < checks[j].File
		})

		for _, check := range checks {
			iconClass := "check-pass"
			iconStr := "✓"
			if !check.Passed {
				iconStr = "✗"
				iconClass = "check-fail"
			}

			sevClass := "sev-info"
			switch check.Severity {
			case SeverityCritical:
				sevClass = "sev-critical"
			case SeverityHigh:
				sevClass = "sev-high"
			case SeverityMedium:
				sevClass = "sev-medium"
			case SeverityLow:
				sevClass = "sev-low"
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

			remediation := ""
			if !check.Passed && check.Remediation != "" {
				remediation = fmt.Sprintf(`<div class="check-remediation">Fix: %s</div>`, check.Remediation)
			}

			fmt.Fprintf(f, `
                <div class="check-item">
                    <div class="check-icon %s">%s</div>
                    <div class="check-details">
                        <div class="check-rule">%s</div>
                        <div class="check-message">%s</div>
                        %s
                        %s
                    </div>
                    <span class="check-severity %s">%s</span>
                </div>
`, iconClass, iconStr, check.Rule, check.Message, remediation, location, sevClass, check.Severity)
		}

		fmt.Fprintf(f, `
            </div>
        </div>
`)
	}

	fmt.Fprintf(f, `
        <h2>Resources</h2>
        <ul style="margin-left: 1.5rem; color: var(--text-muted);">
            <li><a href="https://owasp.org/www-project-top-ten/" class="link">OWASP Top 10</a></li>
            <li><a href="https://cheatsheetseries.owasp.org/" class="link">OWASP Cheat Sheet Series</a></li>
            <li><a href="https://cwe.mitre.org/top25/" class="link">CWE Top 25</a></li>
            <li><a href="https://github.com/nostr-protocol/nips" class="link">Nostr NIPs</a></li>
        </ul>

        <h2>Limitations</h2>
        <p style="color: var(--text-muted);">
            This static analysis cannot detect:
        </p>
        <ul style="margin-left: 1.5rem; color: var(--text-muted); margin-top: 0.5rem;">
            <li>Runtime vulnerabilities (requires dynamic testing)</li>
            <li>Logic flaws and business logic vulnerabilities</li>
            <li>Authentication bypass via implementation bugs</li>
            <li>Timing attacks and side-channel vulnerabilities</li>
            <li>Infrastructure and deployment security issues</li>
        </ul>
    </div>
</body>
</html>
`)

	return nil
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
