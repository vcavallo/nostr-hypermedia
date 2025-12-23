package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Report holds the complete analysis results
type Report struct {
	GeneratedAt       time.Time
	OverallScore      float64
	TotalChecks       int
	PassedChecks      int
	FailedChecks      int
	Categories        []Category
	StringChecks      []StringCheckResult // All string checks for details
	FeatureChecks     []FeatureCheck      // All feature checks for details
}

// StringCheckResult represents the result of checking a string for externalization
type StringCheckResult struct {
	Text      string
	Category  string
	Key       string // i18n key
	Passed    bool
	File      string
	Line      int
}

// Category groups related checks (strings or features)
type Category struct {
	Name        string
	Description string // Optional description for feature categories
	TotalChecks int
	Passed      int
	Failed      int
	Score       float64
}


// I18nStrings represents the structure of the i18n JSON file
type I18nStrings map[string]string

// FeatureCheck represents an i18n feature capability check
type FeatureCheck struct {
	Category    string
	Rule        string
	Passed      bool
	Message     string
	File        string
	Severity    string // "error", "warning", "info"
}

// FeatureCategory groups feature checks by category
type FeatureCategory struct {
	Name        string
	Description string
	Checks      []FeatureCheck
	Passed      int
	Failed      int
	Score       float64
}

// StringToCheck represents a string we want to track for i18n
type StringToCheck struct {
	text     string
	category string
	key      string
}

var (
	projectPath string
	outputFile  string
	verbose     bool
)

func main() {
	flag.StringVar(&projectPath, "path", ".", "Path to the project root")
	flag.StringVar(&outputFile, "output", "i18n-report.html", "Output HTML report file")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.Parse()

	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}
	projectPath = absPath

	fmt.Println("i18n Compliance Checker")
	fmt.Println("=======================")

	report := analyzeProject()
	report.GeneratedAt = time.Now()

	// Calculate overall score from all categories
	for _, cat := range report.Categories {
		report.TotalChecks += cat.TotalChecks
		report.PassedChecks += cat.Passed
		report.FailedChecks += cat.Failed
	}
	if report.TotalChecks > 0 {
		report.OverallScore = float64(report.PassedChecks) / float64(report.TotalChecks) * 100
	}

	// Generate HTML report
	if err := generateReport(report); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nOverall Score: %.1f%%\n", report.OverallScore)
	fmt.Printf("Passed: %d / %d checks\n", report.PassedChecks, report.TotalChecks)
	if report.FailedChecks > 0 {
		fmt.Printf("Failed: %d checks\n", report.FailedChecks)
	}
	fmt.Println()
	for _, cat := range report.Categories {
		fmt.Printf("  %-24s %d/%d (%.0f%%)\n", cat.Name+":", cat.Passed, cat.TotalChecks, cat.Score)
	}

	fmt.Printf("\nReport saved to: %s\n", outputFile)
}

func analyzeProject() Report {
	report := Report{
		Categories:    []Category{},
		StringChecks:  []StringCheckResult{},
		FeatureChecks: []FeatureCheck{},
	}

	// Load existing i18n strings
	i18nStrings := loadI18nStrings()
	if verbose {
		fmt.Printf("Loaded %d i18n strings from config\n", len(i18nStrings))
	}

	// Categories for organizing strings
	categoryMap := map[string]*Category{
		"navigation": {Name: "Navigation & Actions"},
		"labels":     {Name: "Labels & Titles"},
		"buttons":    {Name: "Buttons & Links"},
		"messages":   {Name: "Messages & Prompts"},
		"status":     {Name: "Status Indicators"},
		"datetime":   {Name: "Date & Time"},
		"feeds":      {Name: "Feed Modes (config)"},
		"kinds":      {Name: "Kind Filters (config)"},
		"actions":    {Name: "Actions (config)"},
	}

	// ========================================
	// 1. Check template strings
	// ========================================
	templateStrings := []StringToCheck{
		// Navigation
		{"Load More", "navigation", "nav.load_more"},
		{"View note", "navigation", "nav.view_note"},
		{"View quoted note", "navigation", "nav.view_quoted_note"},
		{"View original note", "navigation", "nav.view_original_note"},
		{"Read article", "navigation", "nav.read_article"},
		{"Watch Stream", "navigation", "nav.watch_stream"},
		{"Watch Recording", "navigation", "nav.watch_recording"},
		{"Watch on zap.stream", "navigation", "nav.watch_zap_stream"},
		{"View zapped note", "navigation", "nav.view_zapped_note"},
		{"View thread", "navigation", "nav.view_thread"},
		{"replies", "navigation", "nav.replies"},

		// Buttons
		{"Post", "buttons", "btn.post"},
		{"Post Commentary", "buttons", "btn.post_commentary"},
		{"Reply", "buttons", "btn.reply"},
		{"Search", "buttons", "btn.search"},
		{"Save Profile", "buttons", "btn.save_profile"},
		{"Cancel", "buttons", "btn.cancel"},
		{"Follow", "buttons", "btn.follow"},
		{"Unfollow", "buttons", "btn.unfollow"},
		{"Login", "buttons", "btn.login"},
		{"Logout", "buttons", "btn.logout"},

		// Labels
		{"Quoting as:", "labels", "label.quoting_as"},
		{"Replying as:", "labels", "label.replying_as"},
		{"Posting as:", "labels", "label.posting_as"},
		{"Replies", "labels", "label.replies"},
		{"replying to", "labels", "label.replying_to"},
		{"to reply", "labels", "label.to_reply"},
		{"reposted", "labels", "label.reposted"},
		{"zapped", "labels", "label.zapped"},
		{"Host:", "labels", "label.host"},
		{"Events", "labels", "label.events"},
		{"Articles", "labels", "label.articles"},
		{"Hashtags", "labels", "label.hashtags"},
		{"Links", "labels", "label.links"},
		{"Bookmarks", "labels", "label.bookmarks"},
		{"Notifications", "labels", "label.notifications"},
		{"Profile", "labels", "label.profile"},
		{"Edit Profile", "labels", "label.edit_profile"},
		{"Display Name", "labels", "label.display_name"},
		{"Username", "labels", "label.username"},
		{"About", "labels", "label.about"},
		{"Website", "labels", "label.website"},
		{"Lightning Address", "labels", "label.lightning_address"},
		{"NIP-05", "labels", "label.nip05"},
		{"Banner URL", "labels", "label.banner_url"},
		{"Picture URL", "labels", "label.picture_url"},

		// Status
		{"LIVE", "status", "status.live"},
		{"SCHEDULED", "status", "status.scheduled"},
		{"ENDED", "status", "status.ended"},
		{"watching", "status", "status.watching"},
		{"sold", "status", "status.sold"},
		{"Loading", "status", "status.loading"},

		// Messages
		{"No results found", "messages", "msg.no_results"},
		{"No notifications", "messages", "msg.no_notifications"},
		{"Not available", "messages", "msg.not_available"},
		{"Reposted note not available", "messages", "msg.repost_not_available"},
		{"Video not available", "messages", "msg.video_not_available"},
		{"Your browser does not support", "messages", "msg.browser_not_supported"},
		{"Untitled", "messages", "msg.untitled"},
		{"Live Event", "messages", "msg.live_event"},

		// Time
		{"Started:", "datetime", "time.started"},
		{"Ended:", "datetime", "time.ended"},
		{"Starts:", "datetime", "time.starts"},

		// Accessibility (merged into labels)
		{"Skip to main content", "labels", "a11y.skip_to_main"},
		{"Search results", "labels", "a11y.search_results"},
	}

	// Scan templates (including subdirectories like templates/kinds/)
	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))
	kindTemplates, _ := filepath.Glob(filepath.Join(templatesPath, "kinds", "*.go"))
	templateFiles = append(templateFiles, kindTemplates...)

	// Combine all template content for analysis
	var allTemplateContent strings.Builder
	templateContents := make(map[string]string)

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fileName := filepath.Base(file)
		contentStr := string(content)
		templateContents[fileName] = contentStr
		allTemplateContent.WriteString(contentStr)
	}

	allContent := allTemplateContent.String()

	// Check each template string
	for _, str := range templateStrings {
		// Check if externalized: look for {{i18n "key"}} usage
		i18nPattern := regexp.MustCompile(`\{\{i18n "` + regexp.QuoteMeta(str.key) + `"\}\}`)
		isExternalized := i18nPattern.MatchString(allContent)

		// Skip strings that don't exist in templates at all
		if !isExternalized && !strings.Contains(allContent, str.text) {
			continue
		}

		// Check if the literal text still appears as hardcoded (not in i18n call, not in comments)
		hasLiteralText := false
		for fileName, content := range templateContents {
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				// Skip Go comments
				trimmedLine := strings.TrimSpace(line)
				if strings.HasPrefix(trimmedLine, "//") {
					continue
				}
				// Skip lines that already use i18n for this key
				if strings.Contains(line, `i18n "`+str.key+`"`) {
					continue
				}
				// Check for the literal text in HTML contexts (user-visible text)
				// Must be exact matches in these patterns:
				// - >Text< (element content)
				// - >Text → (link text with arrow)
				// - Button text before </button>
				// Skip if it's part of a URL, CSS class, or Go template variable

				// Skip lines with URLs containing the text
				if strings.Contains(line, "/"+str.text) || strings.Contains(line, str.text+"/") {
					continue
				}
				// Skip lines with CSS classes containing the text
				if strings.Contains(line, `class="`+strings.ToLower(str.text)) {
					continue
				}
				// Skip comparison operations
				if strings.Contains(line, `eq .`) && strings.Contains(line, `"`+str.text+`"`) {
					continue
				}
				// Skip if text is part of a longer phrase (e.g., "Search notes" vs "Search")
				longerPhrasePattern := regexp.MustCompile(`>` + regexp.QuoteMeta(str.text) + `\s+\w`)
				if longerPhrasePattern.MatchString(line) {
					continue
				}

				// Now check for actual user-visible patterns
				literalPatterns := []string{
					`>` + regexp.QuoteMeta(str.text) + `</`,      // >Text</tag>
					`>` + regexp.QuoteMeta(str.text) + ` →</`,   // >Text →</a>
					`>` + regexp.QuoteMeta(str.text) + ` ↓</`,   // >Text ↓</a>
				}
				for _, pattern := range literalPatterns {
					if matched, _ := regexp.MatchString(pattern, line); matched {
						hasLiteralText = true
						if verbose {
							fmt.Printf("  Found hardcoded '%s' in %s: %s\n", str.text, fileName, strings.TrimSpace(line))
						}
						break
					}
				}
				if hasLiteralText {
					break
				}
			}
			if hasLiteralText {
				break
			}
		}

		if isExternalized && !hasLiteralText {
			// Fully externalized - all uses are via i18n
			categoryMap[str.category].TotalChecks++
			categoryMap[str.category].Passed++
			report.StringChecks = append(report.StringChecks, StringCheckResult{
				Text:     str.text,
				Category: categoryMap[str.category].Name,
				Key:      str.key,
				Passed:   true,
				File:     "templates/*.go",
			})
		} else if hasLiteralText {
			// Has hardcoded user-visible text (may or may not also have i18n uses)
			categoryMap[str.category].TotalChecks++
			categoryMap[str.category].Failed++

			// Find location of hardcoded usage
			file := "templates/*.go"
			line := 0
			for fileName, content := range templateContents {
				lines := strings.Split(content, "\n")
				for i, l := range lines {
					trimmedLine := strings.TrimSpace(l)
					if strings.HasPrefix(trimmedLine, "//") {
						continue
					}
					if strings.Contains(l, `i18n "`) {
						continue
					}
					if strings.Contains(l, str.text) {
						file = fileName
						line = i + 1
						break
					}
				}
				if line > 0 {
					break
				}
			}
			report.StringChecks = append(report.StringChecks, StringCheckResult{
				Text:     str.text,
				Category: categoryMap[str.category].Name,
				Key:      str.key,
				Passed:   false,
				File:     file,
				Line:     line,
			})
		} else if isExternalized {
			// Externalized but text also appears in non-user-visible context (like URLs, CSS classes)
			// This is fine - count as externalized
			categoryMap[str.category].TotalChecks++
			categoryMap[str.category].Passed++
			report.StringChecks = append(report.StringChecks, StringCheckResult{
				Text:     str.text,
				Category: categoryMap[str.category].Name,
				Key:      str.key,
				Passed:   true,
				File:     "templates/*.go",
			})
		}
		// If neither externalized nor hasLiteralText, the string doesn't appear in a user-visible
		// context in templates - skip it (not counted in totals)
	}

	// ========================================
	// 2. Check config-driven strings
	// ========================================

	// Check navigation.json (unified config for feeds, utility, kindFilters)
	navCounts := checkNavigationConfig(i18nStrings, categoryMap, &report)
	if verbose && navCounts > 0 {
		fmt.Printf("Checked %d navigation config entries\n", navCounts)
	}

	// Check actions.json for titleKey usage
	actionsConfig := checkActionsConfig(i18nStrings, categoryMap, &report)
	if verbose && actionsConfig > 0 {
		fmt.Printf("Checked %d action config entries\n", actionsConfig)
	}

	// Build categories list (only include non-empty categories)
	for _, cat := range categoryMap {
		if cat.TotalChecks > 0 {
			cat.Score = float64(cat.Passed) / float64(cat.TotalChecks) * 100
			report.Categories = append(report.Categories, *cat)
		}
	}

	// Sort categories by string count
	sort.Slice(report.Categories, func(i, j int) bool {
		return report.Categories[i].TotalChecks > report.Categories[j].TotalChecks
	})

	// Sort string checks by category, then by passed status (failures first), then by text
	sort.Slice(report.StringChecks, func(i, j int) bool {
		if report.StringChecks[i].Category != report.StringChecks[j].Category {
			return report.StringChecks[i].Category < report.StringChecks[j].Category
		}
		if report.StringChecks[i].Passed != report.StringChecks[j].Passed {
			return !report.StringChecks[i].Passed // Failures first
		}
		return report.StringChecks[i].Text < report.StringChecks[j].Text
	})

	// ========================================
	// 3. Run i18n feature capability checks
	// ========================================
	runFeatureChecks(&report)

	return report
}

// checkNavigationConfig checks the unified navigation.json for titleKey usage
// Handles feeds, utility items, and kindFilters with derived titleKey patterns
func checkNavigationConfig(i18nStrings I18nStrings, categoryMap map[string]*Category, report *Report) int {
	configPath := filepath.Join(projectPath, "config", "navigation.json")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return 0
	}

	var config struct {
		Feeds []struct {
			Name     string `json:"name"`
			TitleKey string `json:"titleKey"`
		} `json:"feeds"`
		Utility []struct {
			Name     string `json:"name"`
			TitleKey string `json:"titleKey"`
		} `json:"utility"`
		KindFilters []struct {
			Name     string `json:"name"`
			TitleKey string `json:"titleKey"`
		} `json:"kindFilters"`
	}

	if err := json.Unmarshal(content, &config); err != nil {
		return 0
	}

	count := 0

	// Check feeds (derive "feed.{name}")
	for _, feed := range config.Feeds {
		count++
		categoryMap["feeds"].TotalChecks++

		titleKey := feed.TitleKey
		if titleKey == "" && feed.Name != "" {
			titleKey = "feed." + feed.Name
		}

		if titleKey != "" {
			if _, exists := i18nStrings[titleKey]; exists {
				categoryMap["feeds"].Passed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     feed.Name,
					Category: categoryMap["feeds"].Name,
					Key:      titleKey,
					Passed:   true,
					File:     "config/navigation.json",
				})
			} else {
				categoryMap["feeds"].Failed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     feed.Name,
					Category: categoryMap["feeds"].Name,
					Key:      titleKey,
					Passed:   false,
					File:     "config/navigation.json",
				})
			}
		}
	}

	// Check utility items (derive "nav.{name}")
	for _, nav := range config.Utility {
		count++
		categoryMap["navigation"].TotalChecks++

		titleKey := nav.TitleKey
		if titleKey == "" && nav.Name != "" {
			titleKey = "nav." + nav.Name
		}

		if titleKey != "" {
			if _, exists := i18nStrings[titleKey]; exists {
				categoryMap["navigation"].Passed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     nav.Name,
					Category: categoryMap["navigation"].Name,
					Key:      titleKey,
					Passed:   true,
					File:     "config/navigation.json",
				})
			} else {
				categoryMap["navigation"].Failed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     nav.Name,
					Category: categoryMap["navigation"].Name,
					Key:      titleKey,
					Passed:   false,
					File:     "config/navigation.json",
				})
			}
		}
	}

	// Check kindFilters (derive "kind.{name}")
	for _, kind := range config.KindFilters {
		count++
		categoryMap["kinds"].TotalChecks++

		titleKey := kind.TitleKey
		if titleKey == "" && kind.Name != "" {
			titleKey = "kind." + kind.Name
		}

		if titleKey != "" {
			if _, exists := i18nStrings[titleKey]; exists {
				categoryMap["kinds"].Passed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     kind.Name,
					Category: categoryMap["kinds"].Name,
					Key:      titleKey,
					Passed:   true,
					File:     "config/navigation.json",
				})
			} else {
				categoryMap["kinds"].Failed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     kind.Name,
					Category: categoryMap["kinds"].Name,
					Key:      titleKey,
					Passed:   false,
					File:     "config/navigation.json",
				})
			}
		}
	}

	return count
}

// checkActionsConfig checks if actions.json uses titleKey and keys exist in i18n
// Supports derived titleKey pattern: if titleKey is empty, derives "action.{name}"
func checkActionsConfig(i18nStrings I18nStrings, categoryMap map[string]*Category, report *Report) int {
	configPath := filepath.Join(projectPath, "config", "actions.json")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return 0
	}

	var config struct {
		Actions map[string]struct {
			Title    string `json:"title"`
			TitleKey string `json:"titleKey"`
		} `json:"actions"`
	}

	if err := json.Unmarshal(content, &config); err != nil {
		return 0
	}

	count := 0
	for name, action := range config.Actions {
		count++
		categoryMap["actions"].TotalChecks++

		// Derive titleKey if not explicitly set
		titleKey := action.TitleKey
		if titleKey == "" && name != "" {
			titleKey = "action." + name
		}

		if titleKey != "" {
			if _, exists := i18nStrings[titleKey]; exists {
				categoryMap["actions"].Passed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     name,
					Category: categoryMap["actions"].Name,
					Key:      titleKey,
					Passed:   true,
					File:     "config/actions.json",
				})
			} else {
				categoryMap["actions"].Failed++
				report.StringChecks = append(report.StringChecks, StringCheckResult{
					Text:     name,
					Category: categoryMap["actions"].Name,
					Key:      titleKey,
					Passed:   false,
					File:     "config/actions.json",
				})
			}
		} else if action.Title != "" {
			// Using hardcoded title (legacy)
			categoryMap["actions"].Failed++
			report.StringChecks = append(report.StringChecks, StringCheckResult{
				Text:     action.Title,
				Category: categoryMap["actions"].Name,
				Key:      "action." + name,
				Passed:   false,
				File:     "config/actions.json",
			})
		}
	}

	return count
}

// ========================================
// i18n Feature Capability Checks
// ========================================

func runFeatureChecks(report *Report) {
	// Initialize feature categories
	featureCats := map[string]*FeatureCategory{
		"pluralization": {
			Name:        "Pluralization",
			Description: "Proper handling of singular/plural forms",
		},
		"datetime": {
			Name:        "Date & Time",
			Description: "Locale-aware date and time formatting",
		},
		"number": {
			Name:        "Number Formatting",
			Description: "Locale-aware number and currency formatting",
		},
	}

	// Read all relevant files (including subdirectories like templates/kinds/)
	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))
	kindTemplates, _ := filepath.Glob(filepath.Join(templatesPath, "kinds", "*.go"))
	templateFiles = append(templateFiles, kindTemplates...)
	var allTemplateContent strings.Builder
	for _, file := range templateFiles {
		if content, err := os.ReadFile(file); err == nil {
			allTemplateContent.WriteString(string(content))
		}
	}
	templateContent := allTemplateContent.String()

	// Read Go source files
	goFiles := []string{"html.go", "html_handlers.go", "html_auth.go", "kinds_appliers.go"}
	var allGoContent strings.Builder
	for _, file := range goFiles {
		if content, err := os.ReadFile(filepath.Join(projectPath, file)); err == nil {
			allGoContent.WriteString(string(content))
		}
	}
	goContent := allGoContent.String()

	// Run pluralization checks
	runPluralizationChecks(featureCats["pluralization"], templateContent, goContent)

	// Run date/time checks
	runDateTimeChecks(featureCats["datetime"], templateContent, goContent)

	// Run number formatting checks
	runNumberChecks(featureCats["number"], templateContent, goContent)

	// Merge feature categories into existing categories or add new ones
	for _, fcat := range featureCats {
		if len(fcat.Checks) == 0 {
			continue
		}

		// Try to find existing category with same name
		found := false
		for i := range report.Categories {
			if report.Categories[i].Name == fcat.Name {
				// Merge into existing category
				report.Categories[i].TotalChecks += len(fcat.Checks)
				report.Categories[i].Passed += fcat.Passed
				report.Categories[i].Failed += len(fcat.Checks) - fcat.Passed
				if report.Categories[i].Description == "" {
					report.Categories[i].Description = fcat.Description
				}
				// Recalculate score
				report.Categories[i].Score = float64(report.Categories[i].Passed) / float64(report.Categories[i].TotalChecks) * 100
				found = true
				break
			}
		}

		if !found {
			// Add as new category
			cat := Category{
				Name:        fcat.Name,
				Description: fcat.Description,
				TotalChecks: len(fcat.Checks),
				Passed:      fcat.Passed,
				Failed:      len(fcat.Checks) - fcat.Passed,
				Score:       float64(fcat.Passed) / float64(len(fcat.Checks)) * 100,
			}
			report.Categories = append(report.Categories, cat)
		}

		// Track all checks for detail display
		report.FeatureChecks = append(report.FeatureChecks, fcat.Checks...)
	}
}

func runPluralizationChecks(cat *FeatureCategory, templateContent string, goContent string) {
	// Design approach: Avoid pluralization by using icon+count patterns (💬 5)
	// and count-independent section headers ("Replies" as category name)

	// Check 1: Uses icon+count pattern (avoids pluralization need)
	iconCountPatterns := []string{"💬", "🔁", "❤", "⚡", "📌"}
	iconPatternCount := 0
	for _, icon := range iconCountPatterns {
		if strings.Contains(templateContent, icon) {
			iconPatternCount++
		}
	}
	hasIconPattern := iconPatternCount >= 2
	addFeatureCheck(cat, "Icon+count patterns", hasIconPattern,
		fmt.Sprintf("Found %d icon patterns for counts (avoids pluralization)", iconPatternCount),
		"templates/*.go", "info")

	// Check 2: No problematic "X items" patterns that need pluralization
	// Bad: "5 replies" Good: "💬 5" or "Replies (5)"
	problematicPatterns := []string{
		`{{.Count}} reply`, `{{.Count}} replies`,
		`{{.Count}} repost`, `{{.Count}} reposts`,
		`{{.ReplyCount}} reply`, `{{.ReplyCount}} replies`,
	}
	hasProblematic := false
	for _, pattern := range problematicPatterns {
		if strings.Contains(templateContent, pattern) {
			hasProblematic = true
			break
		}
	}
	addFeatureCheck(cat, "Avoids count+word patterns", !hasProblematic,
		boolMsg(!hasProblematic, "No problematic count+word patterns found", "Found patterns like 'X replies' that need pluralization"),
		"templates/*.go", "warning")

	// Check 3: Time string variants (manual plural handling for relative time)
	i18nPath := filepath.Join(projectPath, "config", "i18n", "en.json")
	if content, err := os.ReadFile(i18nPath); err == nil {
		i18nContent := string(content)
		hasTimeVariants := strings.Contains(i18nContent, "minute_ago") && strings.Contains(i18nContent, "minutes_ago")
		addFeatureCheck(cat, "Time string variants", hasTimeVariants,
			boolMsg(hasTimeVariants, "Has singular/plural time variants (1m ago, Xm ago)", "Missing time string variants"),
			"config/i18n/en.json", "info")
	}

	// Check 4: Zero state handling
	hasZeroState := strings.Contains(templateContent, "No results") || strings.Contains(templateContent, "No notifications") ||
		strings.Contains(templateContent, "No ") || strings.Contains(templateContent, "no-results")
	addFeatureCheck(cat, "Zero state messages", hasZeroState,
		boolMsg(hasZeroState, "Templates handle zero/empty states", "No zero state handling found"),
		"templates/*.go", "info")
}

func runDateTimeChecks(cat *FeatureCategory, templateContent string, goContent string) {
	// Check 1: Using time.Format with locale considerations
	// Look for .Format( method calls on time values
	hasTimeFormat := strings.Contains(goContent, ".Format(")
	addFeatureCheck(cat, "Time formatting used", hasTimeFormat,
		boolMsg(hasTimeFormat, "Codebase uses time formatting", "No time formatting found"),
		"*.go", "info")

	// Check 2: ISO 8601 datetime attributes
	hasDatetime := strings.Contains(templateContent, `datetime="`) || strings.Contains(templateContent, `datetime={{`)
	addFeatureCheck(cat, "Datetime attributes", hasDatetime,
		boolMsg(hasDatetime, "HTML time elements use datetime attribute", "No datetime attributes found"),
		"templates/*.go", "warning")

	// Check 3: <time> element usage
	hasTimeElement := strings.Contains(templateContent, "<time")
	addFeatureCheck(cat, "Semantic time elements", hasTimeElement,
		boolMsg(hasTimeElement, "Uses <time> elements for dates", "No <time> elements found"),
		"templates/*.go", "warning")

	// Check 4: Relative time patterns
	relativePatterns := []string{"ago", "yesterday", "today", "tomorrow", "just now"}
	hasRelative := false
	for _, pattern := range relativePatterns {
		if strings.Contains(templateContent, pattern) || strings.Contains(goContent, pattern) {
			hasRelative = true
			break
		}
	}
	addFeatureCheck(cat, "Relative time display", hasRelative,
		boolMsg(hasRelative, "Uses relative time patterns (ago, today, etc.)", "No relative time patterns found"),
		"*.go", "info")

	// Check 5: Timezone handling
	hasTZ := strings.Contains(goContent, "time.Location") || strings.Contains(goContent, "time.LoadLocation") ||
		strings.Contains(goContent, ".UTC(") || strings.Contains(goContent, "time.UTC")
	addFeatureCheck(cat, "Timezone awareness", hasTZ,
		boolMsg(hasTZ, "Code handles timezones", "No timezone handling found"),
		"*.go", "info")

	// Check 6: Unix timestamp usage (good for i18n)
	hasUnix := strings.Contains(goContent, ".Unix()") || strings.Contains(goContent, "time.Unix")
	addFeatureCheck(cat, "Unix timestamps", hasUnix,
		boolMsg(hasUnix, "Uses Unix timestamps (locale-independent storage)", "No Unix timestamp usage found"),
		"*.go", "info")
}

func runNumberChecks(cat *FeatureCategory, templateContent string, goContent string) {
	// Check 1: Number formatting patterns
	hasNumberFormat := strings.Contains(goContent, "strconv.Format") || strings.Contains(goContent, "fmt.Sprintf")
	addFeatureCheck(cat, "Number formatting", hasNumberFormat,
		boolMsg(hasNumberFormat, "Uses number formatting functions", "No number formatting found"),
		"*.go", "info")

	// Check 2: Thousands separator awareness
	hasThousandsSep := strings.Contains(goContent, ",") && (strings.Contains(goContent, "format") || strings.Contains(goContent, "Format"))
	addFeatureCheck(cat, "Thousands separators", hasThousandsSep,
		boolMsg(hasThousandsSep, "May use thousands separators", "No thousands separator patterns found"),
		"*.go", "info")

	// Check 3: Currency/zap amount formatting
	hasZapFormat := strings.Contains(goContent, "ZapTotal") || strings.Contains(goContent, "zap") ||
		strings.Contains(templateContent, "⚡") || strings.Contains(templateContent, "sats")
	addFeatureCheck(cat, "Currency/sats formatting", hasZapFormat,
		boolMsg(hasZapFormat, "Has zap/currency amount display", "No currency formatting found"),
		"*.go", "info")

	// Check 4: Large number abbreviation (K, M, etc.)
	hasAbbrev := strings.Contains(goContent, "1000") || strings.Contains(goContent, "1e") ||
		strings.Contains(templateContent, "K") || strings.Contains(templateContent, "M ")
	addFeatureCheck(cat, "Number abbreviations", hasAbbrev,
		boolMsg(hasAbbrev, "May use number abbreviations (K, M)", "No number abbreviation patterns found"),
		"*.go", "info")
}

func addFeatureCheck(cat *FeatureCategory, rule string, passed bool, message string, file string, severity string) {
	check := FeatureCheck{
		Category: cat.Name,
		Rule:     rule,
		Passed:   passed,
		Message:  message,
		File:     file,
		Severity: severity,
	}
	cat.Checks = append(cat.Checks, check)
	if passed {
		cat.Passed++
	} else {
		cat.Failed++
	}
}

func boolMsg(b bool, trueMsg, falseMsg string) string {
	if b {
		return trueMsg
	}
	return falseMsg
}

func loadI18nStrings() I18nStrings {
	strings := make(I18nStrings)

	i18nPath := filepath.Join(projectPath, "config", "i18n", "en.json")
	content, err := os.ReadFile(i18nPath)
	if err != nil {
		if verbose {
			fmt.Printf("No i18n config found at %s\n", i18nPath)
		}
		return strings
	}

	if err := json.Unmarshal(content, &strings); err != nil {
		fmt.Printf("Warning: Error parsing i18n config: %v\n", err)
		return strings
	}

	return strings
}

func generateReport(report Report) error {
	funcMap := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
	}
	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportTemplate)
	if err != nil {
		return err
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, report)
}

var reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>i18n Compliance Report</title>
    <style>
        :root {
            --bg: #0d1117;
            --bg-secondary: #161b22;
            --border: #30363d;
            --text: #e6edf3;
            --text-muted: #8b949e;
            --green: #238636;
            --yellow: #9e6a03;
            --red: #da3633;
            --blue: #58a6ff;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.5;
            padding: 2rem;
        }
        .container { max-width: 1200px; margin: 0 auto; }
        h1 { margin-bottom: 0.5rem; }
        .meta { color: var(--text-muted); margin-bottom: 2rem; }

        .score-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 2rem;
            margin-bottom: 2rem;
            display: flex;
            align-items: center;
            gap: 2rem;
        }
        .score-circle {
            width: 120px;
            height: 120px;
            border-radius: 50%;
            border: 8px solid;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            font-size: 2rem;
            font-weight: bold;
        }
        .score-label { font-size: 0.9rem; font-weight: normal; }
        .score-details h3 { margin-bottom: 0.5rem; }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
            margin-bottom: 2rem;
        }
        .stat-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 1.5rem;
        }
        .stat-card h4 { color: var(--text-muted); font-size: 0.85rem; margin-bottom: 0.5rem; }
        .stat-card .value { font-size: 2rem; font-weight: bold; }
        .stat-card .value.green { color: var(--green); }
        .stat-card .value.yellow { color: var(--yellow); }
        .stat-card .value.red { color: var(--red); }

        .section {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            margin-bottom: 1.5rem;
            overflow: hidden;
        }
        .section-header {
            padding: 1rem 1.5rem;
            border-bottom: 1px solid var(--border);
            display: flex;
            justify-content: space-between;
            align-items: center;
            cursor: pointer;
        }
        .section-header:hover { background: rgba(255,255,255,0.02); }
        .section-header h3 { font-size: 1rem; }
        .section-content { display: none; padding: 1rem 1.5rem; }
        .section.open .section-content { display: block; }

        .category-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 0.75rem 0;
            border-bottom: 1px solid var(--border);
        }
        .category-row:last-child { border-bottom: none; }
        .category-name { font-weight: 500; }
        .category-stats { display: flex; gap: 1rem; align-items: center; }
        .category-bar {
            width: 100px;
            height: 8px;
            background: var(--border);
            border-radius: 4px;
            overflow: hidden;
        }
        .category-bar-fill {
            height: 100%;
            background: var(--green);
            transition: width 0.3s;
        }

        .string-table {
            width: 100%;
            border-collapse: collapse;
        }
        .string-table th, .string-table td {
            padding: 0.75rem;
            text-align: left;
            border-bottom: 1px solid var(--border);
        }
        .string-table th {
            color: var(--text-muted);
            font-weight: 500;
            font-size: 0.85rem;
        }
        .string-table tr:last-child td { border-bottom: none; }
        .string-text { font-family: monospace; background: rgba(255,255,255,0.05); padding: 0.2rem 0.4rem; border-radius: 3px; }
        .string-key { color: var(--blue); font-family: monospace; font-size: 0.85rem; }
        .string-file { color: var(--text-muted); font-size: 0.85rem; }
        .string-category {
            font-size: 0.75rem;
            padding: 0.2rem 0.5rem;
            border-radius: 3px;
            background: rgba(88, 166, 255, 0.1);
            color: var(--blue);
        }

        .json-preview {
            background: var(--bg);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 1rem;
            overflow-x: auto;
            font-family: monospace;
            font-size: 0.85rem;
            white-space: pre;
            max-height: 400px;
            overflow-y: auto;
        }

        .copy-btn {
            background: var(--blue);
            color: white;
            border: none;
            padding: 0.5rem 1rem;
            border-radius: 4px;
            cursor: pointer;
            font-size: 0.85rem;
        }
        .copy-btn:hover { opacity: 0.9; }
    </style>
</head>
<body>
    <div class="container">
        <h1>i18n Compliance Report</h1>
        <p class="meta">Generated: {{.GeneratedAt.Format "2006-01-02 15:04:05"}}</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: {{if ge .OverallScore 80.0}}var(--green){{else if ge .OverallScore 50.0}}var(--yellow){{else}}var(--red){{end}}; color: {{if ge .OverallScore 80.0}}var(--green){{else if ge .OverallScore 50.0}}var(--yellow){{else}}var(--red){{end}};">
                {{printf "%.0f" .OverallScore}}%
                <span class="score-label">score</span>
            </div>
            <div class="score-details">
                <h3>i18n Compliance</h3>
                <p style="color: var(--text-muted);">
                    {{.PassedChecks}} of {{.TotalChecks}} checks passed
                </p>
            </div>
        </div>

        <div class="stats-grid">
            <div class="stat-card">
                <h4>Total Checks</h4>
                <div class="value">{{.TotalChecks}}</div>
            </div>
            <div class="stat-card">
                <h4>Passed</h4>
                <div class="value green">{{.PassedChecks}}</div>
            </div>
            <div class="stat-card">
                <h4>Failed</h4>
                <div class="value {{if gt .FailedChecks 0}}red{{else}}green{{end}}">{{.FailedChecks}}</div>
            </div>
            <div class="stat-card">
                <h4>Categories</h4>
                <div class="value">{{len .Categories}}</div>
            </div>
        </div>

        {{if .Categories}}
        <h2 style="font-size: 1.5rem; margin: 2rem 0 1rem; border-bottom: 1px solid var(--border); padding-bottom: 0.5rem;">Category Breakdown</h2>
        <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; margin-bottom: 2rem;">
            {{range .Categories}}
            <div style="background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 8px; padding: 1rem;">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
                    <span style="font-weight: 600;">{{.Name}}</span>
                    <span style="font-size: 1.2rem; font-weight: bold; color: {{if ge .Score 80.0}}var(--green){{else if ge .Score 50.0}}var(--yellow){{else}}var(--red){{end}};">{{printf "%.0f" .Score}}%</span>
                </div>
                <div class="category-bar">
                    <div class="category-bar-fill" style="width: {{printf "%.0f" .Score}}%; background: {{if ge .Score 80.0}}var(--green){{else if ge .Score 50.0}}var(--yellow){{else}}var(--red){{end}};"></div>
                </div>
                {{if .Description}}<p style="margin-top: 0.25rem; color: var(--text-muted); font-size: 0.8rem;">{{.Description}}</p>{{end}}
                <p style="margin-top: 0.25rem; color: var(--text-muted); font-size: 0.8rem;">{{.Passed}} passed / {{.TotalChecks}} checks</p>
            </div>
            {{end}}
        </div>
        {{end}}

        {{if .Categories}}
        <h2 style="font-size: 1.5rem; margin: 2rem 0 1rem; border-bottom: 1px solid var(--border); padding-bottom: 0.5rem;">Detailed Findings</h2>
        <button class="toggle-btn" style="background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; margin-bottom: 1rem;" onclick="document.querySelectorAll('.section').forEach(s => s.classList.toggle('open'))">
            Toggle All
        </button>
        {{range .Categories}}
        {{$cat := .}}
        <div class="section{{if gt .Failed 0}} open{{end}}">
            <div class="section-header" onclick="this.parentElement.classList.toggle('open')">
                <span>
                    <span style="font-family: monospace; color: var(--blue);">{{.Name}}</span>
                </span>
                <div style="display: flex; gap: 1rem;">
                    <span style="padding: 0.2rem 0.5rem; border-radius: 4px; font-size: 0.85rem; background: rgba(35,134,54,0.2); color: var(--green);">{{.Passed}} passed</span>
                    {{if gt .Failed 0}}<span style="padding: 0.2rem 0.5rem; border-radius: 4px; font-size: 0.85rem; background: rgba(218,54,51,0.2); color: var(--red);">{{.Failed}} failed</span>{{end}}
                </div>
            </div>
            <div class="section-content">
                {{range $.StringChecks}}
                {{if eq .Category $cat.Name}}
                <div style="padding: 0.75rem 0; border-bottom: 1px solid var(--border); display: flex; gap: 1rem; align-items: flex-start;">
                    <div style="width: 20px; height: 20px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 12px; flex-shrink: 0; background: {{if .Passed}}var(--green){{else}}var(--red){{end}}; color: white;">{{if .Passed}}✓{{else}}✗{{end}}</div>
                    <div style="flex: 1;">
                        <div style="font-weight: 500;"><span class="string-text">{{.Text}}</span></div>
                        <div style="color: var(--text-muted); font-size: 0.9rem;">Key: <span class="string-key">{{.Key}}</span></div>
                        <div style="color: var(--text-muted); font-size: 0.8rem; font-family: monospace;">{{.File}}{{if .Line}}:{{.Line}}{{end}}</div>
                    </div>
                </div>
                {{end}}
                {{end}}
                {{range $.FeatureChecks}}
                {{if eq .Category $cat.Name}}
                <div style="padding: 0.75rem 0; border-bottom: 1px solid var(--border); display: flex; gap: 1rem; align-items: flex-start;">
                    <div style="width: 20px; height: 20px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 12px; flex-shrink: 0; background: {{if .Passed}}var(--green){{else if eq .Severity "warning"}}var(--yellow){{else}}var(--red){{end}}; color: white;">{{if .Passed}}✓{{else}}✗{{end}}</div>
                    <div style="flex: 1;">
                        <div style="font-weight: 500;">{{.Rule}}</div>
                        <div style="color: var(--text-muted); font-size: 0.9rem;">{{.Message}}</div>
                        <div style="color: var(--text-muted); font-size: 0.8rem; font-family: monospace;">{{.File}}</div>
                    </div>
                </div>
                {{end}}
                {{end}}
            </div>
        </div>
        {{end}}
        {{end}}
    </div>

    <script>
        // Simple copy functionality could be added here
    </script>
</body>
</html>
`
