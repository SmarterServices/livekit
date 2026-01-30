# Egress Gateway Setup Guide

## Architecture & Data Flow

The Egress Gateway acts as a centralized egress orchestrator that can manage recording/streaming for multiple remote LiveKit media servers.

### Visual Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CLIENT APPLICATION                                │
│                                                                             │
│  const egressInfo = await egressClient.startRoomCompositeEgress({          │
│    room_name: 'my-room',                                                   │
│    // ... egress config                                                    │
│  }, {                                                                      │
│    headers: { 'media-server-id': 'production' }  ◄─── Specifies target    │
│  });                                                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ ① Egress API Request
                                    │    (with media-server-id header)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          EGRESS GATEWAY                                     │
│  ┌───────────────────────────────────────────────────────────────────┐    │
│  │  ② Lookup Server Credentials                                      │    │
│  │     - Check config file first (static servers)                    │    │
│  │     - Fallback to Redis (dynamic servers)                         │    │
│  │     - Decrypt credentials (AES-256-GCM)                           │    │
│  └───────────────────────────────────────────────────────────────────┘    │
│                                    │                                        │
│  ┌───────────────────────────────────────────────────────────────────┐    │
│  │  ③ Validate Room on Remote Server                                │    │
│  │     - Call remote server's ListRooms API                          │    │
│  │     - Check cache first (60s TTL)                                 │    │
│  │     - Get room SID                                                │    │
│  └───────────────────────────────────────────────────────────────────┘    │
│                                    │                                        │
│  ┌───────────────────────────────────────────────────────────────────┐    │
│  │  ④ Generate Access Token                                          │    │
│  │     - Use remote server's API key/secret                          │    │
│  │     - Grant: RoomJoin, Hidden, Recorder                           │    │
│  │     - Identity: egress_id                                         │    │
│  └───────────────────────────────────────────────────────────────────┘    │
│                                    │                                        │
│  ┌───────────────────────────────────────────────────────────────────┐    │
│  │  ⑤ Start Egress Worker                                            │    │
│  │     - Pass token + remote server URL                              │    │
│  │     - Worker connects to remote media server                      │    │
│  └───────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Gateway Redis: Stores encrypted credentials + egress state                │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ ⑥ Egress Worker Connects
                                    │    (using generated token)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      REMOTE LIVEKIT MEDIA SERVER                            │
│                         (Production/Staging/etc)                            │
│                                                                             │
│  ┌─────────────────┐         ┌──────────────────┐                         │
│  │  Active Room    │◄────────│  Egress Worker   │                         │
│  │  "my-room"      │         │  (Hidden)        │                         │
│  │                 │         │                  │                         │
│  │  Participants:  │  Media  │  Recording/      │                         │
│  │  - User A       │─────────►  Streaming       │                         │
│  │  - User B       │  Stream │                  │                         │
│  └─────────────────┘         └──────────────────┘                         │
│                                       │                                     │
│  Media Server Redis: Room state only  │                                    │
└───────────────────────────────────────┼─────────────────────────────────────┘
                                        │
                                        │ ⑦ Output Stream/File
                                        ▼
                              ┌──────────────────┐
                              │   S3 / Storage   │
                              │   or RTMP URL    │
                              └──────────────────┘
```

### Key Components

1. **Client Application** - Initiates egress with `media-server-id` header
2. **Egress Gateway** - Orchestrates egress across multiple media servers
3. **Gateway Redis** - Stores encrypted credentials and egress state
4. **Remote Media Server** - Hosts the actual room with participants
5. **Media Server Redis** - Stores room state (separate from gateway)
6. **Egress Worker** - Joins room as hidden participant, records/streams
7. **Storage/Output** - Final destination (S3, RTMP, etc.)

### Data Flow Steps

| Step | Component | Action | Data |
|------|-----------|--------|------|
| ① | Client → Gateway | Start egress request | Room name, layout, output config, `media-server-id` header |
| ② | Gateway | Lookup credentials | Server ID → Host, API Key, API Secret (decrypted) |
| ③ | Gateway → Media Server | Validate room exists | Room name → Room SID (cached 60s) |
| ④ | Gateway | Generate token | Remote credentials → JWT with RoomJoin grant |
| ⑤ | Gateway | Start worker | Token + URL → Egress worker process |
| ⑥ | Worker → Media Server | Join room | WebSocket connection with JWT auth |
| ⑦ | Worker → Storage | Save output | Media stream → MP4/HLS/RTMP |

## Gateway-Only Mode

When `egress_gateway.enabled: true`, the LiveKit server runs in **gateway-only mode**, which disables all non-essential services to create a lightweight egress orchestrator.

### Services Running in Gateway Mode ✅

- **EgressService** - Egress orchestration and API
- **IOInfoService** - Egress state management
- **Gateway Redis** - Encrypted credential storage (separate from media servers)
- **ServerRegistry** - Remote server credential management
- **RemoteValidator** - Room validation with caching
- **API Authentication** - Standard LiveKit auth
- **Prometheus Metrics** - Optional monitoring

### Services Disabled in Gateway Mode ❌

- **RoomService** - No local room management
- **RTCService** - No media processing
- **SignalServer** - No WebRTC signaling
- **RoomManager** - No room lifecycle management
- **Router** - No node routing/discovery
- **IngressService** - No ingress
- **SIPService** - No SIP trunking
- **WHIPService** - No WHIP
- **AgentService** - No agent dispatch
- **TURN Server** - No TURN relay

**Result:** Gateway server uses ~90% less resources than a full LiveKit server.

## Quick Start

The Egress Gateway allows you to run LiveKit egress services independently from your main media servers, enabling centralized recording/streaming across multiple LiveKit deployments.

### 1. Generate Encryption Key

```bash
openssl rand -base64 32
```

### 2. Configure Gateway

Add to your `config.yaml`:

```yaml
egress_gateway:
  enabled: true
  redis:
    address: gateway-redis.host:6379
    password: your_redis_password
  master_encryption_key: "your-32-byte-key-from-step-1"
  
  # Optional: Static servers (checked first)
  remote_servers:
    production:
      host: "https://livekit.example.com"
      api_key: "APIxxxxxx"
      api_secret: "secretxxxxxx"
```

### 3. Start Gateway

```bash
livekit-server --config config.yaml
```

### 4. Use Gateway from Client

**Option A: Static Server (from config)**

```javascript
const egressInfo = await egressClient.startRoomCompositeEgress({
  room_name: 'my-room',
  attributes: {
    media_server_id: 'production'  // References config
  },
  layout: 'grid',
  file: {
    filepath: 'recording-{time}.mp4',
    output: { /* S3 config */ }
  }
});
```

**Option B: Dynamic Server (via API)**

```javascript
// 1. Register server (admin only, one-time)
await gatewayAdmin.registerRemoteServer({
  server_id: 'new-server',
  host: 'https://new.livekit.example.com',
  api_key: 'APIxxxxxx',
  api_secret: 'secretxxxxxx'
});

// 2. Use it
const egressInfo = await egressClient.startRoomCompositeEgress({
  room_name: 'my-room',
  attributes: {
    media_server_id: 'new-server'
  },
  // ... rest of config
});
```

## Architecture

```
Client → Gateway API → Gateway validates room on remote server
                    → Gateway starts egress worker
                    → Egress worker connects to remote server
                    → Recording saved to S3/storage
```

## Configuration Reference

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | false | Enable gateway mode |
| `redis` | RedisConfig | - | Gateway's own Redis instance |
| `master_encryption_key` | string | - | 32-byte key for encrypting credentials |
| `remote_servers` | map | - | Static server configurations |
| `validation_cache_ttl` | duration | 60s | Cache TTL for room validation |
| `validation_timeout` | duration | 2s | Timeout for remote validation |

## Security

- **Credentials**: Encrypted at rest in Redis using AES-256-GCM
- **Authentication**: Standard LiveKit API authentication required
- **Admin API**: Requires `roomAdmin` grant for server registration
- **TLS**: Recommended for all connections (gateway ↔ Redis, gateway ↔ remote servers)

## Performance

- **Cache hits**: < 1ms
- **Cache misses**: < 200ms (including remote validation)
- **Connection pooling**: HTTP/2 with persistent connections
- **Caching**: 60s TTL reduces remote calls by 80%+

## Troubleshooting

### Gateway not starting
- Check `master_encryption_key` is exactly 32 bytes
- Verify Redis connection
- Check logs for initialization errors

### Room validation failing
- Verify remote server is accessible
- Check API credentials are correct
- Ensure room exists on remote server
- Check `validation_timeout` setting

### Performance issues
- Increase `validation_cache_ttl` for longer caching
- Check network latency to remote servers
- Monitor cache hit rates in logs

## See Also

- [Implementation Plan](EGRESS_GATEWAY_IMPLEMENTATION_PLAN.md)
- [Overview](EGRESS_GATEWAY_OVERVIEW.md)
- [Requirements](EGRESS_GATEWAY_REQ.md)
