# Malleable Bot

A Nostr bot that generates malleable UI specifications on demand. Mention the bot with a description of the UI you want, and it will reply with a fully functional malleable app.

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  User posts:                                                     │
│  "Hey nostr:npub1bot... make me a poll about pizza toppings"    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ kind:1 with p-tag mentioning bot
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Malleable Bot                               │
│                                                                  │
│  1. Listens for p-tag mentions via WebSocket                    │
│  2. Extracts the UI request from note content                   │
│  3. Calls Claude API with malleable spec documentation          │
│  4. Validates the generated JSON                                │
│  5. Publishes reply with UI spec as content                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ kind:1 reply containing UI spec JSON
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Malleable Client                              │
│  User views reply in malleable.html → sees rendered poll        │
│  → can vote → votes published as reactions                      │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Start

### 1. Generate a keypair for the bot

```bash
# Using nak
BOT_NSEC=$(nak key generate)
BOT_NPUB=$(echo $BOT_NSEC | nak key public | nak encode npub)

echo "Bot private key: $BOT_NSEC"
echo "Bot npub: $BOT_NPUB"
```

### 2. Set environment variables

```bash
export BOT_NSEC="your-bot-private-key-hex"
export ANTHROPIC_API_KEY="your-anthropic-api-key"
```

### 3. Run the bot

```bash
# From the project root
go run ./cmd/malleable-bot/

# Or build and run
go build -o malleable-bot ./cmd/malleable-bot/
./malleable-bot
```

### 4. Mention the bot

From any Nostr client, create a note that mentions the bot's npub:

```
Hey nostr:npub1... make me a poll asking what people's favorite programming language is with options for Go, Rust, Python, and JavaScript
```

The bot will reply with a malleable UI spec that, when viewed in a malleable client, renders as an interactive poll.

## Command Line Options

```bash
./malleable-bot -nsec <private-key-hex> -anthropic-key <api-key>
```

| Flag | Environment Variable | Description |
|------|---------------------|-------------|
| `-nsec` | `BOT_NSEC` | Bot's private key (64 char hex) |
| `-anthropic-key` | `ANTHROPIC_API_KEY` | Anthropic API key |

## Example Requests

### Polls

```
@bot make a poll about favorite pizza toppings
@bot create a vote for best sci-fi movie
@bot poll: tabs vs spaces?
```

### Forms

```
@bot create a feedback form
@bot make a newsletter signup form
@bot build a contact form with name and message fields
```

### Interactive Elements

```
@bot make a like button
@bot create an RSVP for a party on Saturday
@bot build a simple tip jar
```

### Complex UIs

```
@bot create a quiz about geography with 3 questions
@bot make a meeting scheduler with time slots
@bot build a simple todo list where I can add items
```

## Viewing Generated UIs

The bot's reply contains JSON that looks like raw data in normal Nostr clients. To see it rendered:

1. Copy the event ID of the bot's reply
2. Open `malleable.html` in your browser
3. Paste the event ID and click "Load Event"
4. The UI renders and becomes interactive

Or use the server-side renderer:
```
http://localhost:8080/html/malleable?event=<event-id>
```

## Architecture

```
cmd/malleable-bot/
└── main.go
    ├── Bot struct           - Manages relay connections and state
    ├── RelayPool            - WebSocket connection management
    ├── handleMention()      - Processes incoming mentions
    ├── generateUISpec()     - Calls Claude API with spec docs
    ├── createReply()        - Builds signed reply event
    └── publish()            - Broadcasts to all relays
```

### Relay Connections

The bot connects to multiple relays simultaneously:
- `wss://relay.damus.io`
- `wss://nos.lol`
- `wss://relay.primal.net`

It subscribes to `kind:1` events that have a `p` tag matching its public key.

### Claude Integration

The bot uses Claude to generate UI specs. The system prompt includes:
- Complete malleable UI specification format
- All available element types and properties
- Data binding syntax
- Action definitions (publish, link)
- Multiple examples (polls, forms, RSVP, etc.)
- Design guidelines

This allows Claude to generate valid, well-structured UI specs for a wide variety of requests.

## Security Considerations

- The bot's private key signs all replies—keep it secure
- Rate limiting is not implemented—consider adding it for production
- The bot processes all mentions—consider adding allowlists/blocklists
- Generated UIs can publish events—users should review before interacting

## Extending the Bot

### Adding New UI Patterns

Edit the system prompt in `generateUISpec()` to add new examples:

```go
QUIZ:
{"layout":"card","elements":[...]}
```

### Custom Relays

Modify the `relays` slice at the top of main.go:

```go
relays = []string{
    "wss://your-relay.example",
    "wss://another-relay.example",
}
```

### Different Claude Models

Change the `claudeModel` variable:

```go
claudeModel = "claude-sonnet-4-20250514"  // Current
claudeModel = "claude-3-5-sonnet-20241022"  // Alternative
```

## Troubleshooting

### Bot not receiving mentions

1. Verify the bot's public key matches what you're mentioning
2. Check relay connections in logs
3. Ensure the mention uses a `p` tag (some clients use different formats)

### Invalid UI specs generated

1. Check Claude API response in logs
2. The bot attempts to clean markdown fences from responses
3. If JSON is invalid, an error reply is sent instead

### Connection issues

1. Relays may rate-limit or block unknown pubkeys
2. Try different relays
3. Check firewall/proxy settings

## Dependencies

- `github.com/btcsuite/btcd/btcec/v2` - Secp256k1 signing
- `github.com/gorilla/websocket` - WebSocket client
- Anthropic API - Claude for UI generation
