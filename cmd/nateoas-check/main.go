// NATEOAS Compliance Checker
// Static code analysis tool to measure compliance with the NATEOAS implementation guide.
// Based on: https://github.com/vcavallo/nostr-hypermedia/blob/html-to-nateoas/html-to-nateoas.md

package main

import (
	"bufio"
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

// Phase represents a NATEOAS implementation phase
type Phase struct {
	Number      int
	Name        string
	Description string
	Checks      []Check
	Score       float64
	MaxScore    float64
}

// Check represents a single compliance check
type Check struct {
	Name        string
	Description string
	Status      string // "pass", "fail", "partial", "not-applicable"
	Details     string
	Weight      float64
	Score       float64
	Files       []FileMatch
}

// FileMatch represents a file location for a finding
type FileMatch struct {
	File    string
	Line    int
	Content string
	Issue   string
}

// Report holds the complete analysis results
type Report struct {
	GeneratedAt     time.Time
	ProjectPath     string
	OverallScore    float64
	OverallGrade    string
	Phases          []Phase
	Summary         Summary
	Recommendations []string
}

// Summary holds summary statistics
type Summary struct {
	TotalChecks     int
	PassedChecks    int
	FailedChecks    int
	PartialChecks   int
	Phase1Score     float64
	Phase2Score     float64
	Phase3Score     float64
	Phase4Score     float64
}

var (
	projectPath string
	outputFile  string
)

func main() {
	flag.StringVar(&projectPath, "path", ".", "Path to the project root")
	flag.StringVar(&outputFile, "output", "nateoas-report.html", "Output HTML report file")
	flag.Parse()

	// Resolve absolute path
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}
	projectPath = absPath

	fmt.Printf("NATEOAS Compliance Checker\n")
	fmt.Printf("==========================\n")
	fmt.Printf("Analyzing: %s\n\n", projectPath)

	report := analyzeProject()
	report.ProjectPath = projectPath
	report.GeneratedAt = time.Now()

	// Calculate overall score
	calculateScores(&report)

	// Generate recommendations
	generateRecommendations(&report)

	// Print summary to console
	printSummary(report)

	// Generate HTML report
	if err := generateHTMLReport(report, outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nHTML report generated: %s\n", outputFile)
}

func analyzeProject() Report {
	report := Report{
		Phases: []Phase{
			{
				Number:      1,
				Name:        "Centralize the Mapping",
				Description: "Single source of truth for actions with ActionTemplate struct and GetActionsForEvent function",
				Checks:      runPhase1Checks(),
			},
			{
				Number:      2,
				Name:        "Make It Dynamic",
				Description: "Runtime flexibility via JSON config, contextual actions, SIGHUP hot-reload",
				Checks:      runPhase2Checks(),
			},
			{
				Number:      3,
				Name:        "Decentralize (Nostr-Native)",
				Description: "Source action definitions from Nostr relays via kind 39001 events",
				Checks:      runPhase3Checks(),
			},
			{
				Number:      4,
				Name:        "Full NATEOAS",
				Description: "Complete hypermedia-driven architecture where protocol data governs all capabilities",
				Checks:      runPhase4Checks(),
			},
		},
	}
	return report
}

// =============================================================================
// Phase 1 Checks: Centralize the Mapping
// Requirements:
// - ActionTemplate and FieldTemplate structs defined
// - KindActionsMap or equivalent mapping
// - GetActionsForEvent() function
// - Templates use {{range .Actions}} instead of hardcoded forms
// - No hardcoded action hrefs in templates
// =============================================================================

func runPhase1Checks() []Check {
	checks := []Check{}

	// Check 1.1: ActionTemplate struct exists
	checks = append(checks, checkActionTemplateStruct())

	// Check 1.2: FieldTemplate/FieldDefinition struct exists
	checks = append(checks, checkFieldTemplateStruct())

	// Check 1.3: GetActionsForEvent function exists
	checks = append(checks, checkGetActionsFunction())

	// Check 1.4: Templates use centralized actions ({{range .Actions}})
	checks = append(checks, checkTemplatesUseCentralizedActions())

	// Check 1.5: No hardcoded action URLs in templates
	checks = append(checks, checkNoHardcodedActionURLs())

	// Check 1.6: Generic action template exists (renders forms from ActionTemplate)
	checks = append(checks, checkGenericActionTemplate())

	return checks
}

func checkActionTemplateStruct() Check {
	check := Check{
		Name:        "ActionTemplate struct",
		Description: "ActionTemplate struct with Name, Title, Href, Method, Fields defined",
		Weight:      2.0,
	}

	// Check actions.go or actions_config.go for the struct
	files := []string{
		filepath.Join(projectPath, "actions.go"),
		filepath.Join(projectPath, "actions_config.go"),
	}

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Check for ActionTemplate or ActionDefinition struct with required fields
		hasStruct := strings.Contains(contentStr, "type ActionTemplate struct") ||
			strings.Contains(contentStr, "type ActionDefinition struct")
		hasName := strings.Contains(contentStr, "Name") && strings.Contains(contentStr, "string")
		hasTitle := strings.Contains(contentStr, "Title")
		hasHref := strings.Contains(contentStr, "Href")
		hasMethod := strings.Contains(contentStr, "Method")
		hasFields := strings.Contains(contentStr, "Fields")

		if hasStruct && hasName && hasTitle && hasHref && hasMethod && hasFields {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("ActionTemplate/ActionDefinition struct found in %s with all required fields", filepath.Base(file))
			return check
		} else if hasStruct {
			check.Status = "partial"
			check.Score = check.Weight * 0.5
			missing := []string{}
			if !hasName {
				missing = append(missing, "Name")
			}
			if !hasTitle {
				missing = append(missing, "Title")
			}
			if !hasHref {
				missing = append(missing, "Href")
			}
			if !hasMethod {
				missing = append(missing, "Method")
			}
			if !hasFields {
				missing = append(missing, "Fields")
			}
			check.Details = fmt.Sprintf("Struct found but missing: %s", strings.Join(missing, ", "))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No ActionTemplate or ActionDefinition struct found"
	return check
}

func checkFieldTemplateStruct() Check {
	check := Check{
		Name:        "FieldTemplate struct",
		Description: "FieldTemplate/FieldDefinition struct for form field definitions",
		Weight:      1.5,
	}

	files := []string{
		filepath.Join(projectPath, "actions.go"),
		filepath.Join(projectPath, "actions_config.go"),
	}

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		if strings.Contains(contentStr, "type FieldTemplate struct") ||
			strings.Contains(contentStr, "type FieldDefinition struct") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("FieldTemplate/FieldDefinition struct found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No FieldTemplate or FieldDefinition struct found"
	return check
}

func checkGetActionsFunction() Check {
	check := Check{
		Name:        "GetActionsForEvent function",
		Description: "Function that returns actions based on event kind and context",
		Weight:      2.0,
	}

	actionsPath := filepath.Join(projectPath, "actions.go")
	content, err := os.ReadFile(actionsPath)
	if err != nil {
		check.Status = "fail"
		check.Details = "actions.go not found"
		return check
	}

	contentStr := string(content)

	// Check for the function signature
	if strings.Contains(contentStr, "func GetActionsForEvent") {
		// Check if it takes context parameter
		if strings.Contains(contentStr, "ActionContext") || strings.Contains(contentStr, "context") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "GetActionsForEvent function found with context parameter"
		} else {
			check.Status = "partial"
			check.Score = check.Weight * 0.7
			check.Details = "GetActionsForEvent found but may lack context parameter"
		}
	} else {
		check.Status = "fail"
		check.Details = "GetActionsForEvent function not found"
	}

	return check
}

func checkTemplatesUseCentralizedActions() Check {
	check := Check{
		Name:        "Templates use centralized actions",
		Description: "Templates iterate over .Actions instead of hardcoding action forms",
		Weight:      3.0,
		Files:       []FileMatch{},
	}

	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))

	templatesWithActions := 0
	templatesNeedingActions := 0

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Skip files without template definitions
		if !strings.Contains(contentStr, "{{define") {
			continue
		}

		// Check if this template renders event items (needs actions)
		needsActions := strings.Contains(contentStr, "note-footer") ||
			strings.Contains(contentStr, "action-form") ||
			strings.Contains(contentStr, "article class=")

		if needsActions {
			templatesNeedingActions++
		}

		// Check for {{range .Actions}} or {{range .ActionGroups}} pattern
		if strings.Contains(contentStr, "{{range .Actions}}") ||
			strings.Contains(contentStr, "{{range .ActionGroups}}") ||
			strings.Contains(contentStr, "range .Actions") ||
			strings.Contains(contentStr, "range .ActionGroups") ||
			strings.Contains(contentStr, "{{range .Item.Actions}}") {
			templatesWithActions++
		}
	}

	if templatesNeedingActions == 0 {
		check.Status = "not-applicable"
		check.Details = "No templates requiring actions found"
		return check
	}

	if templatesWithActions > 0 {
		check.Status = "pass"
		check.Score = check.Weight
		check.Details = fmt.Sprintf("%d template(s) use centralized action iteration", templatesWithActions)
	} else {
		check.Status = "fail"
		check.Details = "No templates using {{range .Actions}} - actions may be hardcoded"
	}

	return check
}

func checkNoHardcodedActionURLs() Check {
	check := Check{
		Name:        "No hardcoded action URLs",
		Description: "Action URLs come from ActionTemplate.Href, not hardcoded in templates",
		Weight:      2.5,
		Files:       []FileMatch{},
	}

	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))

	// Patterns for hardcoded action URLs outside of {{range .Actions}} blocks
	// These are the inline event actions that should use centralized actions
	hardcodedPatterns := []*regexp.Regexp{
		regexp.MustCompile(`action="/html/(react|repost|bookmark|delete)"`),
		regexp.MustCompile(`href="/html/(react|repost|bookmark|delete)\?`),
	}

	hardcodedCount := 0

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Skip if file uses centralized actions properly
		if strings.Contains(contentStr, "{{range .Actions}}") {
			// Check if hardcoded URLs appear OUTSIDE the range block
			// Simple heuristic: if the file has range .Actions, assume it's properly centralized
			continue
		}

		lines := strings.Split(contentStr, "\n")
		for lineNum, line := range lines {
			for _, pattern := range hardcodedPatterns {
				if pattern.MatchString(line) {
					hardcodedCount++
					check.Files = append(check.Files, FileMatch{
						File:    filepath.Base(file),
						Line:    lineNum + 1,
						Content: strings.TrimSpace(line),
						Issue:   "Hardcoded action URL (should use {{.Href}} from Actions)",
					})
				}
			}
		}
	}

	if hardcodedCount == 0 {
		check.Status = "pass"
		check.Score = check.Weight
		check.Details = "No hardcoded action URLs found outside centralized action rendering"
	} else if hardcodedCount <= 3 {
		check.Status = "partial"
		check.Score = check.Weight * 0.5
		check.Details = fmt.Sprintf("%d hardcoded action URLs found", hardcodedCount)
	} else {
		check.Status = "fail"
		check.Details = fmt.Sprintf("%d hardcoded action URLs found - migrate to {{range .Actions}}", hardcodedCount)
	}

	return check
}

func checkGenericActionTemplate() Check {
	check := Check{
		Name:        "Generic action template",
		Description: "Template that renders forms dynamically from ActionTemplate fields",
		Weight:      2.0,
	}

	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Look for generic action rendering patterns
		// Accept both {{range .Actions}} and {{range .ActionGroups}} (grouped pill layout)
		hasRangeActions := strings.Contains(contentStr, "{{range .Actions}}") ||
			strings.Contains(contentStr, "{{range .ActionGroups}}")
		hasMethodCheck := strings.Contains(contentStr, ".Method") &&
			(strings.Contains(contentStr, `eq .Method "GET"`) ||
				strings.Contains(contentStr, `eq .Method "POST"`) ||
				strings.Contains(contentStr, `eq $a.Method "GET"`) ||
				strings.Contains(contentStr, `eq $a.Method "POST"`) ||
				strings.Contains(contentStr, "if eq .Method"))
		hasHrefUsage := strings.Contains(contentStr, "{{.Href}}") ||
			strings.Contains(contentStr, "{{$a.Href}}") ||
			strings.Contains(contentStr, "action=\"{{.Href}}\"") ||
			strings.Contains(contentStr, "action=\"{{$a.Href}}\"")
		hasFieldsRange := strings.Contains(contentStr, "{{range .Fields}}") ||
			strings.Contains(contentStr, "{{range $a.Fields}}")

		if hasRangeActions && hasHrefUsage {
			if hasMethodCheck && hasFieldsRange {
				check.Status = "pass"
				check.Score = check.Weight
				check.Details = fmt.Sprintf("Generic action template found in %s with method switching and field iteration", filepath.Base(file))
				return check
			}
			check.Status = "partial"
			check.Score = check.Weight * 0.7
			check.Details = fmt.Sprintf("Action rendering found in %s but may lack full genericity", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No generic action template found - forms may be hardcoded per action type"
	return check
}

// =============================================================================
// Phase 2 Checks: Make It Dynamic (Runtime)
// Requirements:
// - JSON config file for action definitions
// - LoadActionsFromConfig function
// - SIGHUP handler for hot-reload
// - Contextual action logic (author-only, feature flags)
// - RegisterKindAction for runtime registration
// =============================================================================

func runPhase2Checks() []Check {
	checks := []Check{}

	// Check 2.1: JSON config file exists
	checks = append(checks, checkActionsConfigFile())

	// Check 2.2: Config loading function
	checks = append(checks, checkConfigLoadingFunction())

	// Check 2.3: SIGHUP hot-reload handler
	checks = append(checks, checkSIGHUPHandler())

	// Check 2.4: Contextual action logic (author-only, login-gated)
	checks = append(checks, checkContextualActionLogic())

	// Check 2.5: Kind overrides in config
	checks = append(checks, checkKindOverridesConfig())

	// Check 2.6: Display order configuration
	checks = append(checks, checkDisplayOrderConfig())

	// Check 2.7: Runtime action registration
	checks = append(checks, checkRuntimeActionRegistration())

	return checks
}

func checkActionsConfigFile() Check {
	check := Check{
		Name:        "Actions JSON config file",
		Description: "External JSON file defining actions (not hardcoded in Go)",
		Weight:      2.0,
	}

	configPaths := []string{
		filepath.Join(projectPath, "config", "actions.json"),
		filepath.Join(projectPath, "actions.json"),
	}

	for _, path := range configPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg map[string]interface{}
		if json.Unmarshal(content, &cfg) == nil {
			// Check for required structure
			hasActions := false
			if actions, ok := cfg["actions"]; ok {
				if _, isMap := actions.(map[string]interface{}); isMap {
					hasActions = true
				}
			}

			if hasActions {
				check.Status = "pass"
				check.Score = check.Weight
				check.Details = fmt.Sprintf("Valid actions config found: %s", filepath.Base(path))
				return check
			}
			check.Status = "partial"
			check.Score = check.Weight * 0.5
			check.Details = fmt.Sprintf("Config file found but may lack proper 'actions' structure: %s", filepath.Base(path))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No actions.json config file found in config/ or project root"
	return check
}

func checkConfigLoadingFunction() Check {
	check := Check{
		Name:        "Config loading function",
		Description: "LoadActionsConfig or similar function to load JSON config",
		Weight:      1.5,
	}

	configPath := filepath.Join(projectPath, "actions_config.go")
	content, err := os.ReadFile(configPath)
	if err != nil {
		check.Status = "fail"
		check.Details = "actions_config.go not found"
		return check
	}

	contentStr := string(content)

	if strings.Contains(contentStr, "func LoadActionsConfig") ||
		strings.Contains(contentStr, "func loadActionsConfig") {
		// Check if it actually reads from file
		if strings.Contains(contentStr, "os.ReadFile") || strings.Contains(contentStr, "ioutil.ReadFile") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "LoadActionsConfig function found that reads from file"
		} else {
			check.Status = "partial"
			check.Score = check.Weight * 0.7
			check.Details = "LoadActionsConfig found but may not read from external file"
		}
	} else {
		check.Status = "fail"
		check.Details = "No LoadActionsConfig function found"
	}

	return check
}

func checkSIGHUPHandler() Check {
	check := Check{
		Name:        "SIGHUP hot-reload",
		Description: "Signal handler for config hot-reload without server restart",
		Weight:      1.5,
	}

	mainPath := filepath.Join(projectPath, "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		check.Status = "fail"
		check.Details = "main.go not found"
		return check
	}

	contentStr := string(content)

	hasSIGHUP := strings.Contains(contentStr, "syscall.SIGHUP")
	hasSignalNotify := strings.Contains(contentStr, "signal.Notify")
	hasReload := strings.Contains(contentStr, "Reload") || strings.Contains(contentStr, "reload")

	if hasSIGHUP && hasSignalNotify && hasReload {
		check.Status = "pass"
		check.Score = check.Weight
		check.Details = "SIGHUP handler found with reload functionality"
	} else if hasSIGHUP || (hasSignalNotify && hasReload) {
		check.Status = "partial"
		check.Score = check.Weight * 0.5
		check.Details = "Partial SIGHUP support found"
	} else {
		check.Status = "fail"
		check.Details = "No SIGHUP handler for config hot-reload"
	}

	return check
}

func checkContextualActionLogic() Check {
	check := Check{
		Name:        "Contextual action logic",
		Description: "Actions vary based on context (login state, author, permissions)",
		Weight:      2.0,
	}

	actionsPath := filepath.Join(projectPath, "actions.go")
	content, err := os.ReadFile(actionsPath)
	if err != nil {
		check.Status = "fail"
		check.Details = "actions.go not found"
		return check
	}

	contentStr := string(content)

	// Check for contextual patterns
	hasLoginCheck := strings.Contains(contentStr, "LoggedIn") || strings.Contains(contentStr, "session")
	hasAuthorCheck := strings.Contains(contentStr, "IsAuthor") || strings.Contains(contentStr, "Pubkey")
	hasContextStruct := strings.Contains(contentStr, "ActionContext")

	score := 0.0
	details := []string{}

	if hasContextStruct {
		score += 0.4
		details = append(details, "ActionContext struct")
	}
	if hasLoginCheck {
		score += 0.3
		details = append(details, "login state checks")
	}
	if hasAuthorCheck {
		score += 0.3
		details = append(details, "author checks")
	}

	if score >= 0.9 {
		check.Status = "pass"
		check.Score = check.Weight
		check.Details = fmt.Sprintf("Full contextual logic: %s", strings.Join(details, ", "))
	} else if score > 0 {
		check.Status = "partial"
		check.Score = check.Weight * score
		check.Details = fmt.Sprintf("Partial contextual logic: %s", strings.Join(details, ", "))
	} else {
		check.Status = "fail"
		check.Details = "No contextual action logic found"
	}

	return check
}

func checkKindOverridesConfig() Check {
	check := Check{
		Name:        "Kind-specific overrides",
		Description: "Config supports per-kind action customization (kindOverrides)",
		Weight:      1.0,
	}

	// Check JSON config
	configPath := filepath.Join(projectPath, "config", "actions.json")
	if content, err := os.ReadFile(configPath); err == nil {
		if strings.Contains(string(content), "kindOverrides") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "kindOverrides found in actions.json"
			return check
		}
	}

	// Check Go code
	configGoPath := filepath.Join(projectPath, "actions_config.go")
	if content, err := os.ReadFile(configGoPath); err == nil {
		if strings.Contains(string(content), "KindOverrides") ||
			strings.Contains(string(content), "kindOverrides") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "KindOverrides support found in actions_config.go"
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No kind-specific override support found"
	return check
}

func checkDisplayOrderConfig() Check {
	check := Check{
		Name:        "Display order configuration",
		Description: "Configurable action display order in JSON config",
		Weight:      1.0,
	}

	configPath := filepath.Join(projectPath, "config", "actions.json")
	if content, err := os.ReadFile(configPath); err == nil {
		if strings.Contains(string(content), "displayOrder") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "displayOrder found in actions.json"
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No displayOrder configuration found"
	return check
}

func checkRuntimeActionRegistration() Check {
	check := Check{
		Name:        "Runtime action registration",
		Description: "RegisterKindAction or similar for dynamic action registration",
		Weight:      1.5,
	}

	// Check for actions_registry.go or similar
	registryPath := filepath.Join(projectPath, "actions_registry.go")
	if content, err := os.ReadFile(registryPath); err == nil {
		contentStr := string(content)
		if strings.Contains(contentStr, "RegisterKindAction") ||
			strings.Contains(contentStr, "RegisterAction") {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "Runtime action registration found in actions_registry.go"
			return check
		}
	}

	// Check actions.go as fallback
	actionsPath := filepath.Join(projectPath, "actions.go")
	if content, err := os.ReadFile(actionsPath); err == nil {
		if strings.Contains(string(content), "Register") {
			check.Status = "partial"
			check.Score = check.Weight * 0.5
			check.Details = "Some registration pattern found in actions.go"
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No runtime action registration mechanism found"
	return check
}

// =============================================================================
// Phase 3 Checks: Decentralize (Nostr-Native)
// Requirements:
// - Kind 39001 event format support (or similar kind definition events)
// - FetchKindDefinitionsFromNostr function
// - Tag parsing for action definitions
// - Definition caching with TTL
// - Local override/fallback support
// =============================================================================

func runPhase3Checks() []Check {
	checks := []Check{}

	// Check 3.1: Nostr kind definition fetcher
	checks = append(checks, checkNostrKindFetcher())

	// Check 3.2: Action tag parsing
	checks = append(checks, checkActionTagParsing())

	// Check 3.3: Definition caching
	checks = append(checks, checkDefinitionCaching())

	// Check 3.4: Local fallback support
	checks = append(checks, checkLocalFallback())

	// Check 3.5: Background refresh mechanism
	checks = append(checks, checkBackgroundRefresh())

	return checks
}

func checkNostrKindFetcher() Check {
	check := Check{
		Name:        "Nostr kind definition fetcher",
		Description: "Function to fetch kind definitions from Nostr relays (kind 39001 or similar)",
		Weight:      3.0,
	}

	// Look for nostr_kind_fetcher.go or similar
	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Look for Nostr-based kind fetching patterns
		hasFetchFunc := strings.Contains(contentStr, "FetchKindDefinitions") ||
			strings.Contains(contentStr, "fetchKindDefinitions") ||
			strings.Contains(contentStr, "FetchKindMetadata")
		hasKind39001 := strings.Contains(contentStr, "39001")
		hasRelayFetch := strings.Contains(contentStr, "fetchEventsFromRelays") ||
			strings.Contains(contentStr, "FetchEvents")

		if hasFetchFunc && (hasKind39001 || hasRelayFetch) {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("Kind definition fetcher found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No Nostr kind definition fetcher found (Phase 3 not implemented)"
	return check
}

func checkActionTagParsing() Check {
	check := Check{
		Name:        "Action tag parsing",
		Description: "Parse [\"action\", name, method, href, field_spec...] tags from events",
		Weight:      2.0,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// Look for tag parsing patterns
		hasParseActionTags := strings.Contains(contentStr, "parseActionTags") ||
			strings.Contains(contentStr, "ParseActionTags")
		hasFieldSpecParsing := strings.Contains(contentStr, "parseFieldSpec") ||
			strings.Contains(contentStr, "FieldSpec")
		hasTagIteration := strings.Contains(contentStr, `tag[0] == "action"`) ||
			strings.Contains(contentStr, `"action"`) && strings.Contains(contentStr, "tags")

		if hasParseActionTags || (hasFieldSpecParsing && hasTagIteration) {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("Action tag parsing found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No action tag parsing found (Phase 3 not implemented)"
	return check
}

func checkDefinitionCaching() Check {
	check := Check{
		Name:        "Definition caching",
		Description: "Cache fetched definitions with TTL to avoid repeated relay queries",
		Weight:      1.5,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasCacheStruct := strings.Contains(contentStr, "CachedDefinition") ||
			strings.Contains(contentStr, "definitionCache") ||
			strings.Contains(contentStr, "kindMetadataCache")
		hasTTL := strings.Contains(contentStr, "TTL") || strings.Contains(contentStr, "ttl")
		hasFetchedAt := strings.Contains(contentStr, "FetchedAt") || strings.Contains(contentStr, "lastFetch")

		if hasCacheStruct && (hasTTL || hasFetchedAt) {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("Definition caching with TTL found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No definition caching mechanism found (Phase 3 not implemented)"
	return check
}

func checkLocalFallback() Check {
	check := Check{
		Name:        "Local fallback support",
		Description: "Fallback to local definitions when Nostr fetch fails",
		Weight:      1.5,
	}

	// This is partially satisfied by having JSON config (Phase 2)
	// Check for explicit fallback logic
	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasFallback := strings.Contains(contentStr, "fallback") ||
			strings.Contains(contentStr, "Fallback") ||
			strings.Contains(contentStr, "LoadDefinitionsWithOverrides")
		hasLocalOverride := strings.Contains(contentStr, "localOverride") ||
			strings.Contains(contentStr, "LocalOverride")

		if hasFallback || hasLocalOverride {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "Explicit fallback/override mechanism found"
			return check
		}
	}

	// Check if JSON config exists as implicit fallback
	configPath := filepath.Join(projectPath, "config", "actions.json")
	if _, err := os.Stat(configPath); err == nil {
		check.Status = "partial"
		check.Score = check.Weight * 0.5
		check.Details = "JSON config exists (implicit fallback) but no explicit fallback logic"
		return check
	}

	check.Status = "fail"
	check.Details = "No fallback mechanism found"
	return check
}

func checkBackgroundRefresh() Check {
	check := Check{
		Name:        "Background refresh",
		Description: "Background goroutine to refresh definitions without blocking requests",
		Weight:      1.0,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasGoroutine := strings.Contains(contentStr, "go func()") || strings.Contains(contentStr, "go ")
		hasTicker := strings.Contains(contentStr, "time.NewTicker") || strings.Contains(contentStr, "time.Tick")
		hasRefresh := strings.Contains(contentStr, "Refresh") ||
			strings.Contains(contentStr, "refresh") ||
			strings.Contains(contentStr, "FetchKind")

		if hasGoroutine && hasTicker && hasRefresh {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "Background refresh mechanism found"
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No background refresh mechanism found (Phase 3 not implemented)"
	return check
}

// =============================================================================
// Phase 4 Checks: Full NATEOAS
// Requirements:
// - Events include action-registry tags pointing to naddr
// - Addressable events (kind 30001+) store action definitions
// - BuildHypermediaEntity discovers all metadata from event tags
// - NO kind-specific conditionals in rendering code
// - Universal template renders unknown kinds correctly
// - Server remains protocol-agnostic
// =============================================================================

func runPhase4Checks() []Check {
	checks := []Check{}

	// Check 4.1: Action-registry tag handling
	checks = append(checks, checkActionRegistryTagHandling())

	// Check 4.2: BuildHypermediaEntity or similar
	checks = append(checks, checkHypermediaEntityBuilder())

	// Check 4.3: NO kind-specific conditionals in templates
	checks = append(checks, checkNoKindConditionals())

	// Check 4.4: Universal/generic event renderer
	checks = append(checks, checkUniversalRenderer())

	// Check 4.5: Render-hint tag support
	checks = append(checks, checkRenderHintSupport())

	// Check 4.6: Protocol-agnostic server
	checks = append(checks, checkProtocolAgnostic())

	return checks
}

func checkActionRegistryTagHandling() Check {
	check := Check{
		Name:        "Action-registry tag handling",
		Description: "Events reference action definitions via action-registry tag pointing to naddr",
		Weight:      2.5,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasActionRegistry := strings.Contains(contentStr, "action-registry") ||
			strings.Contains(contentStr, "ActionRegistry")
		hasNaddrHandling := strings.Contains(contentStr, "naddr") ||
			strings.Contains(contentStr, "FetchAddressableEvent")

		if hasActionRegistry && hasNaddrHandling {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("Action-registry tag handling found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No action-registry tag handling found (Phase 4 not implemented)"
	return check
}

func checkHypermediaEntityBuilder() Check {
	check := Check{
		Name:        "Hypermedia entity builder",
		Description: "BuildHypermediaEntity or similar that discovers all metadata from event tags",
		Weight:      2.5,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasHypermediaEntity := strings.Contains(contentStr, "HypermediaEntity") ||
			strings.Contains(contentStr, "BuildHypermediaEntity")
		hasEventDiscovery := strings.Contains(contentStr, "Tags.Find") ||
			(strings.Contains(contentStr, "action-registry") && strings.Contains(contentStr, "render-hint"))

		if hasHypermediaEntity || hasEventDiscovery {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "Hypermedia entity building found"
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No hypermedia entity builder found (Phase 4 not implemented)"
	return check
}

func checkNoKindConditionals() Check {
	check := Check{
		Name:        "No kind-specific conditionals",
		Description: "Templates must NOT contain conditionals based on .Kind values",
		Weight:      3.0,
		Files:       []FileMatch{},
	}

	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))

	// Patterns that indicate kind-specific conditionals (anti-patterns for Phase 4)
	kindPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\{\{if eq \.Kind \d+\}\}`),
		regexp.MustCompile(`\{\{if eq \.Item\.Kind \d+\}\}`),
		regexp.MustCompile(`eq \.Kind`),
		regexp.MustCompile(`\.Kind\s*==\s*\d+`),
	}

	// Also check for TemplateName conditionals (these are kind-derived)
	templateNamePatterns := []*regexp.Regexp{
		regexp.MustCompile(`\{\{if eq \.TemplateName "[^"]+"\}\}`),
		regexp.MustCompile(`\{\{else if eq \.TemplateName`),
	}

	kindConditionals := 0
	templateNameConditionals := 0

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)
		lines := strings.Split(contentStr, "\n")

		for lineNum, line := range lines {
			for _, pattern := range kindPatterns {
				if pattern.MatchString(line) {
					kindConditionals++
					check.Files = append(check.Files, FileMatch{
						File:    filepath.Base(file),
						Line:    lineNum + 1,
						Content: strings.TrimSpace(line),
						Issue:   "Kind-specific conditional (must be eliminated for Phase 4)",
					})
				}
			}
			for _, pattern := range templateNamePatterns {
				if pattern.MatchString(line) {
					templateNameConditionals++
					check.Files = append(check.Files, FileMatch{
						File:    filepath.Base(file),
						Line:    lineNum + 1,
						Content: strings.TrimSpace(line),
						Issue:   "TemplateName conditional (kind-derived, should use render-hint from event)",
					})
				}
			}
		}
	}

	totalConditionals := kindConditionals + templateNameConditionals

	if totalConditionals == 0 {
		check.Status = "pass"
		check.Score = check.Weight
		check.Details = "No kind-specific conditionals found - rendering is fully generic"
	} else if kindConditionals == 0 && templateNameConditionals <= 5 {
		check.Status = "partial"
		check.Score = check.Weight * 0.5
		check.Details = fmt.Sprintf("%d TemplateName conditionals found (migrate to render-hint tags)", templateNameConditionals)
	} else {
		check.Status = "fail"
		check.Details = fmt.Sprintf("%d kind conditionals + %d templateName conditionals - UI not protocol-driven", kindConditionals, templateNameConditionals)
	}

	return check
}

func checkUniversalRenderer() Check {
	check := Check{
		Name:        "Universal event renderer",
		Description: "Single template that renders ANY event type based on event metadata",
		Weight:      2.5,
	}

	templatesPath := filepath.Join(projectPath, "templates")
	templateFiles, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))

	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		// A universal renderer should:
		// 1. NOT have kind-specific template definitions
		// 2. Render content from .Content or .ContentHTML
		// 3. Render actions from .Actions, .ActionGroups, or .AvailableActions
		// 4. Use render hints, not hardcoded layouts

		hasGenericContent := strings.Contains(contentStr, "{{.Content}}") ||
			strings.Contains(contentStr, "{{.ContentHTML}}")
		hasGenericActions := strings.Contains(contentStr, "{{range .Actions}}") ||
			strings.Contains(contentStr, "{{range .ActionGroups}}") ||
			strings.Contains(contentStr, "{{range .AvailableActions}}")
		hasRenderHint := strings.Contains(contentStr, "RenderHint") ||
			strings.Contains(contentStr, "render-hint")

		// Check it's not just a dispatcher with many kind-specific sub-templates
		kindSpecificTemplates := strings.Count(contentStr, "{{define \"kind-")

		if hasGenericContent && hasGenericActions && kindSpecificTemplates == 0 {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = "Universal renderer found without kind-specific templates"
			return check
		}

		if hasGenericContent && hasGenericActions && hasRenderHint {
			check.Status = "partial"
			check.Score = check.Weight * 0.7
			check.Details = "Generic rendering patterns found but may still have kind-specific templates"
			return check
		}
	}

	// Check how many kind-specific templates exist
	kindTemplateCount := 0
	for _, file := range templateFiles {
		content, _ := os.ReadFile(file)
		kindTemplateCount += strings.Count(string(content), "{{define \"kind-")
	}

	if kindTemplateCount > 0 {
		check.Status = "fail"
		check.Details = fmt.Sprintf("%d kind-specific templates found - not a universal renderer", kindTemplateCount)
	} else {
		check.Status = "fail"
		check.Details = "No universal renderer found"
	}

	return check
}

func checkRenderHintSupport() Check {
	check := Check{
		Name:        "Render-hint tag support",
		Description: "Events can specify render-hint tag for presentation format",
		Weight:      1.5,
	}

	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)

		hasRenderHint := strings.Contains(contentStr, "render-hint") ||
			strings.Contains(contentStr, "RenderHint")

		if hasRenderHint {
			check.Status = "pass"
			check.Score = check.Weight
			check.Details = fmt.Sprintf("Render-hint support found in %s", filepath.Base(file))
			return check
		}
	}

	check.Status = "fail"
	check.Details = "No render-hint tag support found (Phase 4 not implemented)"
	return check
}

func checkProtocolAgnostic() Check {
	check := Check{
		Name:        "Protocol-agnostic server",
		Description: "Server code contains no hardcoded knowledge of specific kinds in rendering logic",
		Weight:      2.0,
		Files:       []FileMatch{},
	}

	// Check Go files for hardcoded kind numbers in RENDERING logic
	// We distinguish between:
	// 1. Rendering logic (anti-pattern) - deciding HOW to display based on kind
	// 2. Data fetching logic (acceptable) - deciding WHAT data to fetch
	// 3. Protocol handling (necessary) - implementing Nostr protocol requirements
	files, _ := filepath.Glob(filepath.Join(projectPath, "*.go"))

	renderingKindRefs := 0
	dataFetchingKindRefs := 0

	// Files that legitimately need kind knowledge (protocol requirements, not rendering)
	allowedFiles := map[string]bool{
		"html_auth.go":          true, // Event creation requires specific kinds
		"kinds.go":              true, // Central kind registry (by design)
		"kinds_appliers.go":     true, // Kind-specific data appliers (by design)
		"navigation_config.go":  true, // Navigation config includes kind filters
		"nip46.go":              true, // NIP-46 protocol handling
		"nostrconnect.go":       true, // Nostr Connect protocol
		"nostr_kind_fetcher.go": true, // Kind metadata fetching
		"nostr_action_tags.go":  true, // Action tag parsing
		"relay.go":              true, // Protocol-level relay handling
	}

	// Pattern that indicates kind-specific conditionals
	kindPattern := regexp.MustCompile(`\.Kind\s*==\s*(\d+)`)

	// Patterns that indicate DATA FETCHING context (acceptable)
	// These are deciding WHAT to fetch, not HOW to render
	dataFetchingPatterns := []*regexp.Regexp{
		regexp.MustCompile(`fetch|Fetch`),                       // Fetching data
		regexp.MustCompile(`filter|Filter|filtered`),            // Filtering results
		regexp.MustCompile(`extract|Extract`),                   // Extracting IDs/data
		regexp.MustCompile(`repost.*Refs|Refs.*repost`),         // Repost reference handling
		regexp.MustCompile(`pubkeySet|pubkey.*Set`),             // Collecting pubkeys to fetch
		regexp.MustCompile(`append\(.*IDs`),                     // Collecting IDs to fetch
		regexp.MustCompile(`replies|Replies`),                   // Reply handling
		regexp.MustCompile(`TrimSpace.*Content.*==""`),          // Reference-only repost detection (NIP-18)
		regexp.MustCompile(`Content.*TrimSpace.*==""`),          // Reference-only repost detection (NIP-18)
		regexp.MustCompile(`strings\.TrimSpace\(.*\.Content\)`), // Reference-only repost detection
	}

	for _, file := range files {
		baseName := filepath.Base(file)

		// Skip allowed files, test files, and config files
		if allowedFiles[baseName] ||
			strings.Contains(file, "_test.go") ||
			strings.Contains(file, "config") {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		contentStr := string(content)
		lines := strings.Split(contentStr, "\n")

		for lineNum, line := range lines {
			if !kindPattern.MatchString(line) {
				continue
			}

			// Check if this is in a data fetching context
			isDataFetching := false

			// Check current line and surrounding context (2 lines before/after)
			contextStart := lineNum - 2
			if contextStart < 0 {
				contextStart = 0
			}
			contextEnd := lineNum + 3
			if contextEnd > len(lines) {
				contextEnd = len(lines)
			}
			context := strings.Join(lines[contextStart:contextEnd], "\n")

			for _, pattern := range dataFetchingPatterns {
				if pattern.MatchString(context) {
					isDataFetching = true
					break
				}
			}

			// Also check function name context - look backwards for func declaration
			funcContext := ""
			for i := lineNum; i >= 0 && i >= lineNum-30; i-- {
				if strings.Contains(lines[i], "func ") {
					funcContext = lines[i]
					break
				}
			}

			// Functions with these names are data fetching, not rendering
			dataFetchingFuncs := []string{
				"extract", "Extract", "fetch", "Fetch", "filter", "Filter",
				"getRepost", "GetRepost", "loadRepost", "LoadRepost",
			}
			for _, fn := range dataFetchingFuncs {
				if strings.Contains(funcContext, fn) {
					isDataFetching = true
					break
				}
			}

			if isDataFetching {
				dataFetchingKindRefs++
			} else {
				renderingKindRefs++
				if renderingKindRefs <= 10 { // Limit stored matches
					check.Files = append(check.Files, FileMatch{
						File:    baseName,
						Line:    lineNum + 1,
						Content: strings.TrimSpace(line),
						Issue:   "Kind-specific rendering logic (should use dynamic templates)",
					})
				}
			}
		}
	}

	// Only count RENDERING kind references as violations
	// Data fetching kind checks are acceptable (protocol-level decisions about what to fetch)
	if renderingKindRefs == 0 {
		check.Status = "pass"
		check.Score = check.Weight
		if dataFetchingKindRefs > 0 {
			check.Details = fmt.Sprintf("Rendering is protocol-agnostic (%d data-fetching kind checks are acceptable)", dataFetchingKindRefs)
		} else {
			check.Details = "Rendering logic is fully protocol-agnostic"
		}
	} else if renderingKindRefs <= 5 {
		check.Status = "partial"
		check.Score = check.Weight * 0.7
		check.Details = fmt.Sprintf("%d rendering kind checks found (+ %d acceptable data-fetching checks)", renderingKindRefs, dataFetchingKindRefs)
	} else if renderingKindRefs <= 15 {
		check.Status = "partial"
		check.Score = check.Weight * 0.5
		check.Details = fmt.Sprintf("%d kind-specific conditionals in rendering - consider config-driven approach", renderingKindRefs)
	} else {
		check.Status = "fail"
		check.Details = fmt.Sprintf("%d kind-specific conditionals in rendering - not protocol-agnostic", renderingKindRefs)
	}

	return check
}

// =============================================================================
// Scoring and Reporting
// =============================================================================

func calculateScores(report *Report) {
	totalScore := 0.0
	totalWeight := 0.0

	for i := range report.Phases {
		phase := &report.Phases[i]
		phaseScore := 0.0
		phaseMax := 0.0

		for _, check := range phase.Checks {
			if check.Status != "not-applicable" {
				phaseMax += check.Weight
				phaseScore += check.Score
			}
		}

		if phaseMax > 0 {
			phase.Score = (phaseScore / phaseMax) * 100
		}
		phase.MaxScore = phaseMax

		totalScore += phaseScore
		totalWeight += phaseMax

		// Update summary
		for _, check := range phase.Checks {
			if check.Status != "not-applicable" {
				report.Summary.TotalChecks++
				switch check.Status {
				case "pass":
					report.Summary.PassedChecks++
				case "fail":
					report.Summary.FailedChecks++
				case "partial":
					report.Summary.PartialChecks++
				}
			}
		}

		switch i {
		case 0:
			report.Summary.Phase1Score = phase.Score
		case 1:
			report.Summary.Phase2Score = phase.Score
		case 2:
			report.Summary.Phase3Score = phase.Score
		case 3:
			report.Summary.Phase4Score = phase.Score
		}
	}

	if totalWeight > 0 {
		report.OverallScore = (totalScore / totalWeight) * 100
	}

	report.OverallGrade = scoreToGrade(report.OverallScore)
}

func scoreToGrade(score float64) string {
	switch {
	case score >= 95:
		return "A+"
	case score >= 90:
		return "A"
	case score >= 85:
		return "A-"
	case score >= 80:
		return "B+"
	case score >= 75:
		return "B"
	case score >= 70:
		return "B-"
	case score >= 65:
		return "C+"
	case score >= 60:
		return "C"
	case score >= 55:
		return "C-"
	case score >= 50:
		return "D"
	default:
		return "F"
	}
}

func generateRecommendations(report *Report) {
	recommendations := []string{}

	// Phase 1 recommendations
	if report.Summary.Phase1Score < 100 {
		for _, check := range report.Phases[0].Checks {
			if check.Status == "fail" {
				switch check.Name {
				case "ActionTemplate struct":
					recommendations = append(recommendations, "Create ActionTemplate struct with Name, Title, Href, Method, Fields in actions.go")
				case "FieldTemplate struct":
					recommendations = append(recommendations, "Create FieldTemplate struct with Name, Type, Placeholder, Value fields")
				case "GetActionsForEvent function":
					recommendations = append(recommendations, "Implement GetActionsForEvent(ctx ActionContext) []ActionTemplate function")
				case "Templates use centralized actions":
					recommendations = append(recommendations, "Update templates to use {{range .Actions}} instead of hardcoded forms")
				case "No hardcoded action URLs":
					recommendations = append(recommendations, "Replace hardcoded /html/react, /html/repost URLs with {{.Href}}")
				case "Generic action template":
					recommendations = append(recommendations, "Create a generic action template that renders forms from ActionTemplate.Fields")
				}
			}
		}
	}

	// Phase 2 recommendations
	if report.Summary.Phase2Score < 100 && report.Summary.Phase1Score >= 80 {
		for _, check := range report.Phases[1].Checks {
			if check.Status == "fail" {
				switch check.Name {
				case "Actions JSON config file":
					recommendations = append(recommendations, "Create config/actions.json with action definitions")
				case "Config loading function":
					recommendations = append(recommendations, "Implement LoadActionsConfig() to load from JSON file")
				case "SIGHUP hot-reload":
					recommendations = append(recommendations, "Add SIGHUP signal handler for config hot-reload")
				case "Runtime action registration":
					recommendations = append(recommendations, "Create RegisterKindAction() for dynamic action registration")
				}
			}
		}
	}

	// Phase 3 recommendations
	if report.Summary.Phase3Score < 50 && report.Summary.Phase2Score >= 80 {
		recommendations = append(recommendations, "Phase 3: Implement Nostr-based kind definitions:")
		recommendations = append(recommendations, "  - Create FetchKindDefinitionsFromNostr() to fetch kind 39001 events")
		recommendations = append(recommendations, "  - Parse [\"action\", name, method, href, field_spec...] tags")
		recommendations = append(recommendations, "  - Add definition caching with TTL")
		recommendations = append(recommendations, "  - Implement fallback to local JSON config")
	}

	// Phase 4 recommendations
	if report.Summary.Phase4Score < 50 && report.Summary.Phase3Score >= 50 {
		recommendations = append(recommendations, "Phase 4: Move toward full NATEOAS:")
		recommendations = append(recommendations, "  - Add action-registry tag handling for events")
		recommendations = append(recommendations, "  - Build HypermediaEntity from event tags")
		recommendations = append(recommendations, "  - Eliminate all kind-specific conditionals in templates")
		recommendations = append(recommendations, "  - Create universal renderer that works with any event type")
	}

	report.Recommendations = recommendations
}

func printSummary(report Report) {
	fmt.Printf("Overall Score: %.1f%% (Grade: %s)\n\n", report.OverallScore, report.OverallGrade)

	fmt.Printf("Phase Scores:\n")
	for _, phase := range report.Phases {
		status := "✓"
		if phase.Score < 50 {
			status = "✗"
		} else if phase.Score < 80 {
			status = "~"
		}
		fmt.Printf("  %s Phase %d: %s - %.1f%%\n", status, phase.Number, phase.Name, phase.Score)
	}

	fmt.Printf("\nCheck Summary:\n")
	fmt.Printf("  Passed:  %d\n", report.Summary.PassedChecks)
	fmt.Printf("  Partial: %d\n", report.Summary.PartialChecks)
	fmt.Printf("  Failed:  %d\n", report.Summary.FailedChecks)
	fmt.Printf("  Total:   %d\n", report.Summary.TotalChecks)

	if len(report.Recommendations) > 0 {
		fmt.Printf("\nTop Recommendations:\n")
		for i, rec := range report.Recommendations {
			if i >= 5 {
				break
			}
			fmt.Printf("  • %s\n", rec)
		}
	}
}

func generateHTMLReport(report Report, outputPath string) error {
	tmpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"statusClass": func(status string) string {
			switch status {
			case "pass":
				return "status-pass"
			case "fail":
				return "status-fail"
			case "partial":
				return "status-partial"
			default:
				return "status-na"
			}
		},
		"statusIcon": func(status string) string {
			switch status {
			case "pass":
				return "✓"
			case "fail":
				return "✗"
			case "partial":
				return "~"
			default:
				return "○"
			}
		},
		"gradeClass": func(grade string) string {
			switch {
			case strings.HasPrefix(grade, "A"):
				return "grade-a"
			case strings.HasPrefix(grade, "B"):
				return "grade-b"
			case strings.HasPrefix(grade, "C"):
				return "grade-c"
			case grade == "D":
				return "grade-d"
			default:
				return "grade-f"
			}
		},
		"countPassed": func(checks []Check) int {
			count := 0
			for _, c := range checks {
				if c.Status == "pass" {
					count++
				}
			}
			return count
		},
		"countFailed": func(checks []Check) int {
			count := 0
			for _, c := range checks {
				if c.Status == "fail" {
					count++
				}
			}
			return count
		},
		"countPartial": func(checks []Check) int {
			count := 0
			for _, c := range checks {
				if c.Status == "partial" {
					count++
				}
			}
			return count
		},
	}).Parse(htmlTemplate))

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	return tmpl.Execute(w, report)
}

var htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>NATEOAS Compliance Report</title>
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
            border-radius: 50%;
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

        .phase-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 1rem;
            margin-bottom: 2rem;
        }
        .phase-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 1rem;
        }
        .phase-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 0.5rem;
        }
        .phase-name { font-weight: 600; }
        .phase-score {
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
            height: 100%;
            border-radius: 4px;
            transition: width 0.3s;
        }
        .phase-desc {
            margin-top: 0.5rem;
            color: var(--text-muted);
            font-size: 0.85rem;
        }
        .phase-stats {
            margin-top: 0.5rem;
            font-size: 0.8rem;
            color: var(--text-muted);
        }

        .phase-section {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            margin-bottom: 1rem;
            overflow: hidden;
        }
        .phase-section-header {
            padding: 1rem;
            border-bottom: 1px solid var(--border);
            cursor: pointer;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .phase-section-header:hover { background: rgba(255,255,255,0.02); }
        .phase-title { font-family: monospace; color: var(--blue); }
        .phase-subtitle { color: var(--text-muted); font-size: 0.85rem; margin-left: 1rem; }
        .phase-badges { display: flex; gap: 0.5rem; }
        .badge { padding: 0.2rem 0.5rem; border-radius: 4px; font-size: 0.85rem; }
        .badge-pass { background: rgba(35,134,54,0.2); color: var(--green); }
        .badge-fail { background: rgba(218,54,51,0.2); color: var(--red); }
        .badge-partial { background: rgba(210,153,34,0.2); color: var(--amber); }

        .checks-list {
            padding: 0;
            display: none;
        }
        .phase-section.open .checks-list { display: block; }
        .check-item {
            padding: 0.75rem 1rem;
            border-bottom: 1px solid var(--border);
            display: flex;
            gap: 1rem;
            align-items: flex-start;
        }
        .check-item:last-child { border-bottom: none; }
        .check-icon {
            width: 24px;
            height: 24px;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 14px;
            flex-shrink: 0;
        }
        .check-pass { background: var(--green); color: white; }
        .check-fail { background: var(--red); color: white; }
        .check-partial { background: var(--amber); color: white; }
        .check-details { flex: 1; }
        .check-name { font-weight: 500; }
        .check-desc { color: var(--text-muted); font-size: 0.9rem; }
        .check-result { color: var(--text-muted); font-size: 0.85rem; font-family: monospace; margin-top: 0.25rem; }
        .check-weight {
            font-size: 0.75rem;
            background: var(--border);
            padding: 0.15rem 0.5rem;
            border-radius: 3px;
            color: var(--text-muted);
        }

        .files-list {
            margin-top: 0.5rem;
            padding: 0.5rem;
            background: var(--bg);
            border-radius: 4px;
            font-family: monospace;
            font-size: 0.8rem;
        }
        .file-match {
            padding: 0.25rem 0;
            border-bottom: 1px solid var(--border);
        }
        .file-match:last-child { border-bottom: none; }
        .file-loc { color: var(--blue); }
        .file-issue { color: var(--amber); }

        .recommendations {
            background: var(--bg-secondary);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 1rem;
        }
        .recommendations ul { margin-left: 1.5rem; }
        .recommendations li { margin: 0.5rem 0; color: var(--text-muted); }

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

        .nateoas-link {
            color: var(--blue);
            text-decoration: none;
        }
        .nateoas-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>NATEOAS Compliance Report</h1>
        <p class="meta">Generated: {{.GeneratedAt.Format "2006-01-02 15:04:05"}}</p>

        <div class="score-card">
            <div class="score-circle" style="border-color: {{if ge .OverallScore 70.0}}#22c55e{{else if ge .OverallScore 50.0}}#f59e0b{{else}}#ef4444{{end}}; color: {{if ge .OverallScore 70.0}}#22c55e{{else if ge .OverallScore 50.0}}#f59e0b{{else}}#ef4444{{end}};">
                {{.OverallGrade}}
                <span class="score-label">{{printf "%.0f" .OverallScore}}%</span>
            </div>
            <div class="score-details">
                <h3>Overall NATEOAS Compliance</h3>
                <p>Based on the <a href="https://github.com/vcavallo/nostr-hypermedia/blob/html-to-nateoas/html-to-nateoas.md" class="nateoas-link">NATEOAS Implementation Guide</a></p>
                <p style="margin-top: 0.5rem; color: var(--text-muted);">
                    {{.Summary.PassedChecks}} passed, {{.Summary.PartialChecks}} partial, {{.Summary.FailedChecks}} failed of {{.Summary.TotalChecks}} checks
                </p>
            </div>
        </div>

        <h2>Implementation Phases</h2>
        <div class="phase-grid">
            {{range .Phases}}
            <div class="phase-card">
                <div class="phase-header">
                    <span class="phase-name">Phase {{.Number}}: {{.Name}}</span>
                    <span class="phase-score" style="color: {{if ge .Score 70.0}}#22c55e{{else if ge .Score 50.0}}#f59e0b{{else}}#ef4444{{end}}">{{printf "%.0f" .Score}}%</span>
                </div>
                <div class="progress-bar">
                    <div class="progress-fill" style="width: {{printf "%.0f" .Score}}%; background: {{if ge .Score 70.0}}#22c55e{{else if ge .Score 50.0}}#f59e0b{{else}}#ef4444{{end}};"></div>
                </div>
                <p class="phase-desc">{{.Description}}</p>
                <p class="phase-stats">{{countPassed .Checks}} passed, {{countPartial .Checks}} partial, {{countFailed .Checks}} failed</p>
            </div>
            {{end}}
        </div>

        <h2>Detailed Findings</h2>
        <button class="toggle-btn" onclick="document.querySelectorAll('.phase-section').forEach(s => s.classList.toggle('open'))">
            Toggle All
        </button>

        {{range .Phases}}
        <div class="phase-section">
            <div class="phase-section-header" onclick="this.parentElement.classList.toggle('open')">
                <span>
                    <span class="phase-title">Phase {{.Number}}</span>
                    <span class="phase-subtitle">{{.Name}}</span>
                </span>
                <div class="phase-badges">
                    {{if gt (countPassed .Checks) 0}}<span class="badge badge-pass">{{countPassed .Checks}} passed</span>{{end}}
                    {{if gt (countPartial .Checks) 0}}<span class="badge badge-partial">{{countPartial .Checks}} partial</span>{{end}}
                    {{if gt (countFailed .Checks) 0}}<span class="badge badge-fail">{{countFailed .Checks}} failed</span>{{end}}
                </div>
            </div>
            <div class="checks-list">
                {{range .Checks}}
                <div class="check-item">
                    <div class="check-icon {{if eq .Status "pass"}}check-pass{{else if eq .Status "partial"}}check-partial{{else}}check-fail{{end}}">{{if eq .Status "pass"}}✓{{else if eq .Status "partial"}}~{{else}}✗{{end}}</div>
                    <div class="check-details">
                        <div class="check-name">{{.Name}}</div>
                        <div class="check-desc">{{.Description}}</div>
                        {{if .Details}}<div class="check-result">{{.Details}}</div>{{end}}
                        {{if .Files}}
                        <div class="files-list">
                            {{range .Files}}
                            <div class="file-match">
                                <span class="file-loc">{{.File}}{{if .Line}}:{{.Line}}{{end}}</span>
                                {{if .Issue}}<span class="file-issue"> - {{.Issue}}</span>{{end}}
                            </div>
                            {{end}}
                        </div>
                        {{end}}
                    </div>
                    <span class="check-weight">{{printf "%.1f" .Weight}}</span>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}

        {{if .Recommendations}}
        <h2>Recommendations</h2>
        <div class="recommendations">
            <ul>
                {{range .Recommendations}}
                <li>{{.}}</li>
                {{end}}
            </ul>
        </div>
        {{end}}

        <h2>Resources</h2>
        <ul style="margin-left: 1.5rem; color: var(--text-muted);">
            <li><a href="https://github.com/vcavallo/nostr-hypermedia/blob/html-to-nateoas/html-to-nateoas.md" class="nateoas-link">NATEOAS Implementation Guide</a></li>
            <li><a href="https://hypermedia.systems/" class="nateoas-link">Hypermedia Systems Book</a></li>
            <li><a href="https://github.com/nostr-protocol/nips" class="nateoas-link">Nostr Implementation Possibilities (NIPs)</a></li>
        </ul>
    </div>
</body>
</html>
`

// Helper functions for scanning files

func scanTemplateFiles(projectPath string, patterns []string) []FileMatch {
	matches := []FileMatch{}
	templatesPath := filepath.Join(projectPath, "templates")

	files, _ := filepath.Glob(filepath.Join(templatesPath, "*.go"))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			for _, pattern := range patterns {
				if strings.Contains(line, pattern) {
					matches = append(matches, FileMatch{
						File:    filepath.Base(file),
						Line:    lineNum + 1,
						Content: strings.TrimSpace(line),
					})
				}
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File != matches[j].File {
			return matches[i].File < matches[j].File
		}
		return matches[i].Line < matches[j].Line
	})

	return matches
}
