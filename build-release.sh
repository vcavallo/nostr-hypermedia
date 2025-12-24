#!/bin/bash

# Build a self-contained release package
# Output: release.tar.gz (or release/ folder with --no-gz)
#
# Usage: ./build-release.sh [--no-gz]
#   --no-gz   Keep release/ folder instead of creating tarball

set -e

NO_GZ=0
if [[ "$1" == "--no-gz" ]]; then
    NO_GZ=1
fi

echo "Building release package..."

# Create release directory structure
mkdir -p release/config/i18n release/static

# Build binary
echo "  Compiling binary..."
go build -o release/nostr-server

# Copy config files
echo "  Copying config files..."
cp config/*.json release/config/
cp -r config/i18n/* release/config/i18n/

# Copy and compress static files
echo "  Copying static files..."
cp static/avatar.jpg static/favicon.ico static/og-image.png release/static/
echo "  Building CSS from modules..."
cat static/css/base.css \
    static/css/layout.css \
    static/css/notes.css \
    static/css/kinds/*.css \
    static/css/components.css \
    static/css/pages.css > release/static/style.css
echo "  Minifying CSS..."
# Remove comments, collapse whitespace, remove space around punctuation
sed -i 's|/\*[^*]*\*\+\([^/][^*]*\*\+\)*/||g' release/static/style.css
tr -s ' \t\n' ' ' < release/static/style.css | \
    sed 's/ *{ */{/g; s/ *} */}\n/g; s/ *: */:/g; s/ *; */;/g; s/ *, */,/g; s/;}/}/g' > release/static/style.min.css
mv release/static/style.min.css release/static/style.css
echo "  Compressing JS/CSS..."
gzip -c -9 static/helm.js > release/static/helm.js.gz
gzip -c -9 release/static/style.css > release/static/style.css.gz
rm release/static/style.css

# Copy .env.example
cp .env.example release/

# Generate start.sh
cat > release/start.sh << 'EOF'
#!/bin/bash

# Dynamic startup script for nostr-server
# Usage: ./start.sh [flags]
#
# Flags:
#   --dev       Enable DEV_MODE (persistent keypair)
#   --redis     Enable Redis caching (uses REDIS_URL from .env or default)
#   --no-gzip   Disable gzip compression
#   --debug     Set LOG_LEVEL=debug
#   --port N    Set server port (default: 3000)
#   --help      Show this help message

set -e

# Show help
if [[ "$1" == "--help" || "$1" == "-h" ]]; then
    sed -n '3,12p' "$0"
    exit 0
fi

# Load .env if exists (for secrets like GIPHY_API_KEY, CSRF_SECRET)
if [ -f .env ]; then
    export $(grep -v '^#' .env | grep -v '^$' | xargs)
fi

# Defaults
export PORT="${PORT:-3000}"
export GZIP_ENABLED="${GZIP_ENABLED:-1}"

# Parse flags
while [[ $# -gt 0 ]]; do
    case $1 in
        --dev)
            export DEV_MODE=1
            ;;
        --redis)
            export REDIS_URL="${REDIS_URL:-redis://localhost:6379/0}"
            ;;
        --no-gzip)
            unset GZIP_ENABLED
            ;;
        --debug)
            export LOG_LEVEL=debug
            ;;
        --port)
            export PORT="$2"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage"
            exit 1
            ;;
    esac
    shift
done

# Show config
echo "Starting server with:"
echo "  PORT=$PORT"
[ -n "$DEV_MODE" ] && echo "  DEV_MODE=1"
[ -n "$REDIS_URL" ] && echo "  REDIS_URL=$REDIS_URL"
[ -n "$GZIP_ENABLED" ] && echo "  GZIP_ENABLED=1"
[ -n "$LOG_LEVEL" ] && echo "  LOG_LEVEL=$LOG_LEVEL"
[ -n "$GIPHY_API_KEY" ] && echo "  GIPHY_API_KEY=***"
echo ""

# Run
exec ./nostr-server
EOF

# Generate reload.sh
cat > release/reload.sh << 'EOF'
#!/bin/bash

# Reload configuration files without restarting the server
# Sends SIGHUP to nostr-server, which reloads all JSON configs
# and broadcasts a refresh to connected browsers via SSE

# Check if server is running
PID=$(pgrep -f "^./nostr-server$")

if [ -z "$PID" ]; then
    echo "Error: nostr-server is not running"
    exit 1
fi

# Handle multiple instances
if [ $(echo "$PID" | wc -l) -gt 1 ]; then
    echo "Warning: Multiple instances found:"
    echo "$PID"
    echo "Reloading all..."
fi

echo "Sending SIGHUP to nostr-server (PID: $PID)..."
kill -HUP $PID

echo ""
echo "Config reloaded:"
echo "  - config/actions.json"
echo "  - config/navigation.json"
echo "  - config/relays.json"
echo "  - config/i18n/*.json"
echo ""
echo "Connected browsers will auto-refresh via SSE."
EOF

# Make scripts executable
chmod +x release/start.sh release/reload.sh

echo ""
if [[ "$NO_GZ" == "1" ]]; then
    echo "Release package created in release/"
    echo ""
    echo "To run: cd release && ./start.sh"
else
    echo "  Creating tarball..."
    tar -czf release.tar.gz release
    rm -rf release
    echo "Release package created: release.tar.gz"
    echo ""
    echo "To deploy:"
    echo "  tar -xzf release.tar.gz"
    echo "  cd release && ./start.sh"
fi
