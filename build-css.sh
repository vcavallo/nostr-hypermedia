#!/bin/bash

# Build CSS from modular files for development
# Output: static/style.css
#
# Usage: ./build-css.sh [--watch]
#   --watch   Rebuild automatically when CSS files change (requires inotifywait)

set -e

CSS_DIR="static/css"
OUTPUT="static/style.css"

build_css() {
    echo "Building $OUTPUT..."
    cat "$CSS_DIR/base.css" \
        "$CSS_DIR/layout.css" \
        "$CSS_DIR/notes.css" \
        "$CSS_DIR/kinds/"*.css \
        "$CSS_DIR/components.css" \
        "$CSS_DIR/pages.css" > "$OUTPUT"
    echo "Done. $(wc -l < "$OUTPUT") lines"
}

# Initial build
build_css

# Watch mode
if [[ "$1" == "--watch" ]]; then
    if ! command -v inotifywait &> /dev/null; then
        echo "Error: inotifywait not found. Install inotify-tools for watch mode."
        exit 1
    fi

    echo ""
    echo "Watching $CSS_DIR for changes..."
    echo "Press Ctrl+C to stop"
    echo ""

    while inotifywait -q -r -e modify,create,delete "$CSS_DIR"; do
        build_css
    done
fi
