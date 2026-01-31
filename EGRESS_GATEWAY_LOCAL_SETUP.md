# Running Egress Gateway Locally on Mac

This guide walks you through running the LiveKit Egress Gateway on your Mac for local development and testing.

## Prerequisites

- Go 1.21+ installed
- Redis installed (via Homebrew)
- LiveKit server (can use LiveKit Cloud or local instance)

## Quick Start

### 1. Install Redis

```bash
brew install redis
brew services start redis
```

Verify Redis is running:
```bash
redis-cli ping
# Should return: PONG
```

### 2. Generate Encryption Key

```bash
openssl rand -base64 32
```

Save this key - you'll need it for the config.

### 3. Create Gateway Config

Create `config-gateway.yaml`:

```yaml
port: 7880
bind_addresses:
  - "127.0.0.1"

# Gateway Redis (separate from media server)
redis:
  address: localhost:6379
  db: 1  # Use different DB than media server

# API keys for gateway authentication
keys:
  your_api_key: your_api_secret

# Egress Gateway Configuration
egress_gateway:
  enabled: true
  encryption_key: "YOUR_BASE64_KEY_FROM_STEP_2"
  
  # Static remote servers (optional)
  remote_servers:
    - id: "local-media"
      host: "http://localhost:7880"  # Your local LiveKit server
      api_key: "media_api_key"
      api_secret: "media_api_secret"
    
    # Or use LiveKit Cloud:
    # - id: "livekit-cloud"
    #   host: "https://your-project.livekit.cloud"
    #   api_key: "your_cloud_api_key"
    #   api_secret: "your_cloud_api_secret"

# Egress configuration
egress:
  redis:
    address: localhost:6379
    db: 1  # Same as gateway Redis
```

### 4. Build and Run

```bash
# From the livekit repo root
cd /Volumes/Data/Engineering/livekit

# Build the server
go build -o livekit-server ./cmd/server

# Run in gateway mode
./livekit-server --config config-gateway.yaml --dev
```

You should see:
```
INFO    starting in gateway-only mode    nodeID=...
INFO    gateway server initialized       services=[egress] port=7880
```

## Testing the Gateway

### Option A: Test with LiveKit Cloud

1. **Get your LiveKit Cloud credentials** from https://cloud.livekit.io
2. **Update config** with your cloud server details
3. **Make an egress request** with the `media-server-id` header:

```bash
# Using LiveKit CLI
livekit-cli start-room-composite-egress \
  --url http://localhost:7880 \
  --api-key your_api_key \
  --api-secret your_api_secret \
  --room my-room \
  --file output.mp4 \
  --header "media-server-id=livekit-cloud"
```

### Option B: Test with Local Media Server

1. **Run a local LiveKit media server** (in another terminal):

```bash
# Create media server config
cat > config-media.yaml <<EOF
port: 7881
redis:
  address: localhost:6379
  db: 0  # Different DB from gateway
keys:
  media_api_key: media_api_secret
EOF

# Run media server
./livekit-server --config config-media.yaml --dev
```

2. **Create a test room** on the media server:

```bash
livekit-cli create-room \
  --url http://localhost:7881 \
  --api-key media_api_key \
  --api-secret media_api_secret \
  --room test-room
```

3. **Start egress via gateway**:

```bash
livekit-cli start-room-composite-egress \
  --url http://localhost:7880 \
  --api-key your_api_key \
  --api-secret your_api_secret \
  --room test-room \
  --file output.mp4 \
  --header "media-server-id=local-media"
```

## Verify Gateway Mode

Check that only egress services are running:

```bash
# Gateway should only have egress endpoints
curl http://localhost:7880/

# These should work:
# - POST /twirp/livekit.Egress/StartRoomCompositeEgress
# - POST /twirp/livekit.Egress/StartParticipantEgress
# - etc.

# These should NOT exist (404):
# - /rtc (no RTC service)
# - /whip (no WHIP service)
```

## Troubleshooting

### "Redis connection refused"
```bash
# Check Redis is running
redis-cli ping

# Start Redis if needed
brew services start redis
```

### "Server not found: local-media"
- Check the `id` in your config matches the `media-server-id` header
- Verify the remote server config is correct

### "Room not found"
- Ensure the room exists on the remote media server
- Check the media server URL is accessible from the gateway

### "Invalid encryption key"
- Must be 32 bytes base64-encoded
- Generate new key: `openssl rand -base64 32`

## Development Tips

### Hot Reload
Use `--dev` flag for development mode with debug logging:
```bash
./livekit-server --config config-gateway.yaml --dev
```

### Check Logs
Gateway logs show:
- Server credential lookups
- Room validation (with cache hits/misses)
- Token generation
- Egress worker starts

### Multiple Gateways
Run multiple gateways on different ports for load testing:
```bash
# Gateway 1
./livekit-server --config config-gateway.yaml --dev

# Gateway 2 (different port)
./livekit-server --config config-gateway-2.yaml --dev
```

## Next Steps

- See [EGRESS_GATEWAY_SETUP.md](./EGRESS_GATEWAY_SETUP.md) for production deployment
- See [EGRESS_GATEWAY_OVERVIEW.md](./EGRESS_GATEWAY_OVERVIEW.md) for architecture details
- Add dynamic servers via Admin API (see setup guide)
