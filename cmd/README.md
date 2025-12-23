# Quality Check Tools

Six static analysis tools validate compliance with project standards. All produce HTML reports with consistent structure:

1. **Score Card** - Overall percentage score with pass/fail counts
2. **Categories Grid** - Visual breakdown by category with individual scores
3. **Detailed Findings** - Expandable sections grouped by category, with pass/fail indicators and file locations

## Quick Start

Run all checks at once (reports saved to `reports/`):

```bash
./cmd/run_checks.sh
```

## Individual Checks

### Accessibility (WCAG 2.1)

```bash
cd cmd/accessibility-check && go build && ./accessibility-check -path ../.. -output ../../reports/accessibility-report.html
```

**Categories:** Perceivable, Operable, Understandable, Robust, Motion & Animation, Timing, Focus Management.

### HATEOAS Compliance

```bash
cd cmd/hateoas-check && go build && ./hateoas-check -path ../.. -output ../../reports/hateoas-report.html
```

**Categories:** Navigation, Forms & Actions, Links, Self-Describing, State Transfer, Accessibility, Pagination.

### NATEOAS Compliance

```bash
cd cmd/nateoas-check && go build && ./nateoas-check -path ../.. -output ../../reports/nateoas-report.html
```

**Categories:** Phase 1 (Centralize), Phase 2 (Dynamic), Phase 3 (Nostr-Native), Phase 4 (Full NATEOAS).

### Markup Validation

```bash
cd cmd/markup-check && go build && ./markup-check -path ../.. -output ../../reports/markup-report.html
```

**Categories:** HTML, CSS, Semantic, Best Practices, Dead Code, Performance, SEO & Meta, Mobile.

### i18n Coverage

```bash
cd cmd/i18n-check && go build && ./i18n-check -path ../.. -output ../../reports/i18n-report.html
```

**Categories:** Navigation & Actions, Labels & Titles, Buttons & Controls, Messages & Prompts, and many more.

### Security Analysis

```bash
cd cmd/security-check && go build && ./security-check -path ../.. -output ../../reports/security-report.html
```

**Categories:** CSRF Protection, HTTP Security Headers, Session Security, Nostr Security, Rate Limiting, SSRF Prevention, Cryptography, Information Disclosure.

## Report Structure

Each tool generates an HTML report with:

- **Summary header** with overall score and timestamp
- **Category breakdown** showing individual category scores
- **Detailed findings** with file locations and pass/fail status
- **Expandable sections** for easy navigation

Reports are saved to the `reports/` directory (gitignored).
