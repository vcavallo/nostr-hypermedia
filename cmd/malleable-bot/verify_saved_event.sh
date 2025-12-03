#!/bin/bash
# Run this after the bot saves a failed event to /tmp/last_published_event.json

if [ ! -f /tmp/last_published_event.json ]; then
    echo "No saved event found at /tmp/last_published_event.json"
    exit 1
fi

echo "=== Saved event ==="
cat /tmp/last_published_event.json | head -c 500
echo ""
echo "..."
echo ""

echo "=== Verifying with nak ==="
cat /tmp/last_published_event.json | nak verify 2>&1

echo ""
echo "=== Checking ID computation ==="
# Extract fields and compute ID
ID=$(cat /tmp/last_published_event.json | jq -r '.id')
PUBKEY=$(cat /tmp/last_published_event.json | jq -r '.pubkey')
CREATED_AT=$(cat /tmp/last_published_event.json | jq -r '.created_at')
KIND=$(cat /tmp/last_published_event.json | jq -r '.kind')

echo "Event ID: $ID"
echo "Pubkey: $PUBKEY"
echo "Created at: $CREATED_AT"
echo "Kind: $KIND"

echo ""
echo "=== Trying to re-publish with nak ==="
cat /tmp/last_published_event.json | nak event --sec nsec1ahys6ph7u9mp2g5us5ndcqzajk0y4uaacz6gc4mke9gmetldajzsy4y8e6 wss://relay.damus.io 2>&1
