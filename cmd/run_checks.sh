#!/bin/bash

# Quality check runner for nostr-server
# Builds and runs all check tools from the cmd directory

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
REPORTS_DIR="$PROJECT_DIR/reports"

# Create reports directory
mkdir -p "$REPORTS_DIR"

echo "========================================"
echo "Building check tools"
echo "========================================"

echo "Building accessibility-check..."
cd "$SCRIPT_DIR/accessibility-check"
go build -o accessibility-check .

echo "Building hateoas-check..."
cd "$SCRIPT_DIR/hateoas-check"
go build -o hateoas-check .

echo "Building i18n-check..."
cd "$SCRIPT_DIR/i18n-check"
go build -o i18n-check .

echo "Building markup-check..."
cd "$SCRIPT_DIR/markup-check"
go build -o markup-check .

echo "Building nateoas-check..."
cd "$SCRIPT_DIR/nateoas-check"
go build -o nateoas-check .

echo "Building security-check..."
cd "$SCRIPT_DIR/security-check"
go build -o security-check .

echo ""
echo "========================================"
echo "Running checks"
echo "========================================"

echo ""
echo "--- Accessibility Check (WCAG 2.1) ---"
cd "$SCRIPT_DIR/accessibility-check"
./accessibility-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/accessibility-report.html"

echo ""
echo "--- HATEOAS Check ---"
cd "$SCRIPT_DIR/hateoas-check"
./hateoas-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/hateoas-report.html"

echo ""
echo "--- i18n Check ---"
cd "$SCRIPT_DIR/i18n-check"
./i18n-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/i18n-report.html"

echo ""
echo "--- Markup Check ---"
cd "$SCRIPT_DIR/markup-check"
./markup-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/markup-report.html"

echo ""
echo "--- NATEOAS Check ---"
cd "$SCRIPT_DIR/nateoas-check"
./nateoas-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/nateoas-report.html"

echo ""
echo "--- Security Check ---"
cd "$SCRIPT_DIR/security-check"
./security-check -path "$PROJECT_DIR" -output "$REPORTS_DIR/security-report.html"

echo ""
echo "========================================"
echo "All checks complete"
echo "========================================"
echo "Reports saved to: $REPORTS_DIR"
