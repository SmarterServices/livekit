# Egress Gateway Setup Guide

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
