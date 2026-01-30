# LiveKit Egress Gateway - Simplified Implementation Plan

## Executive Summary

Simplified approach using **Redis-stored encrypted credentials** instead of client-side encryption. This eliminates complexity while maintaining security and flexibility.

---

## Key Design Decisions

### ✅ What Changed from Original Plan

**REMOVED:**
- ❌ Client-side AES-256-GCM encryption
- ❌ Shared encryption keys between clients and gateway
- ❌ Primary/fallback key rotation complexity
- ❌ Client encryption libraries in multiple languages

**ADDED:**
- ✅ Redis-based server registry
- ✅ Admin API for server management
- ✅ Server-side encryption of stored credentials
- ✅ Simple server_id references in egress requests

### 🎯 Benefits

- **Simpler**: ~5 components instead of 9+
- **Secure**: Credentials encrypted at rest, never in transit
- **Flexible**: Add/update servers without redeployment
- **Maintainable**: No client-side encryption code needed

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Admin (one-time setup)                                      │
│                                                              │
│  registerRemoteServer({                                     │
│    server_id: "prod-1",                                     │
│    host: "https://livekit.example.com",                     │
│    api_key: "APIxxxxxx",                                    │
│    api_secret: "secretxxxxxx"                               │
│  })                                                          │
│                                                              │
│         ↓                                                    │
│  Gateway encrypts & stores in Redis                         │
│  Key: gateway:remote_server:prod-1                          │
│  Value: {encrypted credentials}                             │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ Client (normal egress request)                              │
│                                                              │
│  startRoomCompositeEgress({                                 │
│    room_name: "my-room",                                    │
│    gateway_server_id: "prod-1",  ← Just the ID!            │
│    file: { ... }                                            │
│  })                                                          │
│                                                              │
│         ↓                                                    │
│  Gateway looks up "prod-1" in Redis                         │
│  Gateway decrypts credentials                               │
│  Gateway validates room on remote server                    │
│  Gateway starts egress                                      │
└─────────────────────────────────────────────────────────────┘
```

---

## Implementation Plan

### Phase 1: Configuration

**File:** `pkg/config/config.go`

```go
type EgressGatewayConfig struct {
    Enabled bool `yaml:"enabled,omitempty"`
    
    // Gateway's own Redis for egress state
    Redis redisLiveKit.RedisConfig `yaml:"redis,omitempty"`
    
    // Master encryption key for encrypting stored credentials (32 bytes)
    // Used to encrypt remote server credentials before storing in Redis
    MasterEncryptionKey string `yaml:"master_encryption_key,omitempty"`
    
    // Static remote server configurations (optional)
    // These are checked first before looking in Redis
    RemoteServers map[string]*RemoteServerConfig `yaml:"remote_servers,omitempty"`
    
    // Cache TTL for remote room validation (default: 60s)
    ValidationCacheTTL time.Duration `yaml:"validation_cache_ttl,omitempty"`
    
    // Remote validation timeout (default: 2s)
    ValidationTimeout time.Duration `yaml:"validation_timeout,omitempty"`
}

type RemoteServerConfig struct {
    Host      string `yaml:"host,omitempty"`
    APIKey    string `yaml:"api_key,omitempty"`
    APISecret string `yaml:"api_secret,omitempty"`
}
```

**Config Example (Static Servers):**
```yaml
egress_gateway:
  enabled: true
  redis:
    address: localhost:6380
    password: gateway_redis_password
    tls: true
  # 32-byte master key for encrypting stored credentials in Redis
  # Generate with: openssl rand -base64 32
  master_encryption_key: "your-32-byte-master-key-here=="
  
  # Static server configurations (checked first)
  remote_servers:
    prod-server-1:
      host: "https://livekit.example.com"
      api_key: "APIxxxxxx"
      api_secret: "secretxxxxxx"
    staging-server:
      host: "https://staging.livekit.example.com"
      api_key: "APIyyyyyy"
      api_secret: "secretyyyyyy"
  
  validation_cache_ttl: 60s
  validation_timeout: 2s
```

**Lookup Priority:**
1. Check config file `remote_servers` first
2. If not found, check Redis
3. If not found in either, return error

---

### Phase 2: Server Registry with Encrypted Storage

**File:** `pkg/gateway/server_registry.go`

```go
package gateway

import (
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "time"
    
    "github.com/redis/go-redis/v9"
)

type RemoteServerInfo struct {
    ServerID  string    `json:"server_id"`
    Host      string    `json:"host"`
    APIKey    string    `json:"api_key"`    // Stored encrypted
    APISecret string    `json:"api_secret"` // Stored encrypted
    CreatedAt time.Time `json:"created_at"`
    CreatedBy string    `json:"created_by"`
}

type ServerRegistry struct {
    redis          redis.UniversalClient
    encryptionKey  []byte // 32-byte master key
    staticServers  map[string]*RemoteServerConfig // From config file
}

func NewServerRegistry(rc redis.UniversalClient, masterKey []byte, staticServers map[string]*RemoteServerConfig) (*ServerRegistry, error) {
    if len(masterKey) != 32 {
        return nil, fmt.Errorf("master encryption key must be 32 bytes")
    }
    
    return &ServerRegistry{
        redis:         rc,
        encryptionKey: masterKey,
        staticServers: staticServers,
    }, nil
}

// RegisterServer stores remote server credentials (encrypted)
func (r *ServerRegistry) RegisterServer(ctx context.Context, info *RemoteServerInfo) error {
    // Encrypt credentials before storing
    encryptedKey, err := r.encrypt(info.APIKey)
    if err != nil {
        return fmt.Errorf("failed to encrypt API key: %w", err)
    }
    
    encryptedSecret, err := r.encrypt(info.APISecret)
    if err != nil {
        return fmt.Errorf("failed to encrypt API secret: %w", err)
    }
    
    // Store encrypted version
    storedInfo := &RemoteServerInfo{
        ServerID:  info.ServerID,
        Host:      info.Host,
        APIKey:    encryptedKey,
        APISecret: encryptedSecret,
        CreatedAt: info.CreatedAt,
        CreatedBy: info.CreatedBy,
    }
    
    data, err := json.Marshal(storedInfo)
    if err != nil {
        return err
    }
    
    key := fmt.Sprintf("gateway:remote_server:%s", info.ServerID)
    return r.redis.Set(ctx, key, data, 0).Err()
}

// GetServer retrieves remote server credentials
// Priority: 1) Config file, 2) Redis
func (r *ServerRegistry) GetServer(ctx context.Context, serverID string) (*RemoteServerInfo, error) {
    // Check static config first
    if staticServer, ok := r.staticServers[serverID]; ok {
        return &RemoteServerInfo{
            ServerID:  serverID,
            Host:      staticServer.Host,
            APIKey:    staticServer.APIKey,
            APISecret: staticServer.APISecret,
            CreatedAt: time.Time{}, // Static servers don't have creation time
            CreatedBy: "config",
        }, nil
    }
    
    // Not in config, check Redis
    key := fmt.Sprintf("gateway:remote_server:%s", serverID)
    
    data, err := r.redis.Get(ctx, key).Bytes()
    if err != nil {
        return nil, fmt.Errorf("server not found: %s", serverID)
    }
    
    var storedInfo RemoteServerInfo
    if err := json.Unmarshal(data, &storedInfo); err != nil {
        return nil, err
    }
    
    // Decrypt credentials
    apiKey, err := r.decrypt(storedInfo.APIKey)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt API key: %w", err)
    }
    
    apiSecret, err := r.decrypt(storedInfo.APISecret)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt API secret: %w", err)
    }
    
    return &RemoteServerInfo{
        ServerID:  storedInfo.ServerID,
        Host:      storedInfo.Host,
        APIKey:    apiKey,
        APISecret: apiSecret,
        CreatedAt: storedInfo.CreatedAt,
        CreatedBy: storedInfo.CreatedBy,
    }, nil
}

// ListServers returns all registered server IDs (not credentials)
// Includes both static config servers and Redis servers
func (r *ServerRegistry) ListServers(ctx context.Context) ([]string, error) {
    serverIDs := make([]string, 0)
    
    // Add static servers from config
    for serverID := range r.staticServers {
        serverIDs = append(serverIDs, serverID)
    }
    
    // Add dynamic servers from Redis
    keys, err := r.redis.Keys(ctx, "gateway:remote_server:*").Result()
    if err != nil {
        return serverIDs, nil // Return static servers even if Redis fails
    }
    
    for _, key := range keys {
        serverID := key[len("gateway:remote_server:"):]
        serverIDs = append(serverIDs, serverID)
    }
    
    return serverIDs, nil
}

// DeleteServer removes a registered server
func (r *ServerRegistry) DeleteServer(ctx context.Context, serverID string) error {
    key := fmt.Sprintf("gateway:remote_server:%s", serverID)
    return r.redis.Del(ctx, key).Err()
}

// encrypt encrypts a string using AES-256-GCM
func (r *ServerRegistry) encrypt(plaintext string) (string, error) {
    block, err := aes.NewCipher(r.encryptionKey)
    if err != nil {
        return "", err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }
    
    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts a string using AES-256-GCM
func (r *ServerRegistry) decrypt(encrypted string) (string, error) {
    ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
    if err != nil {
        return "", err
    }
    
    block, err := aes.NewCipher(r.encryptionKey)
    if err != nil {
        return "", err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return "", fmt.Errorf("ciphertext too short")
    }
    
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }
    
    return string(plaintext), nil
}
```

---

### Phase 3: Admin API for Server Management

**File:** `pkg/gateway/admin_service.go`

```go
package gateway

import (
    "context"
    "time"
    
    "github.com/livekit/protocol/livekit"
)

type AdminService struct {
    registry *ServerRegistry
}

func NewAdminService(registry *ServerRegistry) *AdminService {
    return &AdminService{
        registry: registry,
    }
}

// RegisterRemoteServer registers a new remote LiveKit server
func (s *AdminService) RegisterRemoteServer(
    ctx context.Context,
    req *livekit.RegisterRemoteServerRequest,
) (*livekit.RegisterRemoteServerResponse, error) {
    // Require admin grant
    if !hasAdminGrant(ctx) {
        return nil, ErrUnauthorized
    }
    
    info := &RemoteServerInfo{
        ServerID:  req.ServerId,
        Host:      req.Host,
        APIKey:    req.ApiKey,
        APISecret: req.ApiSecret,
        CreatedAt: time.Now(),
        CreatedBy: getIdentity(ctx),
    }
    
    if err := s.registry.RegisterServer(ctx, info); err != nil {
        return nil, err
    }
    
    return &livekit.RegisterRemoteServerResponse{
        ServerId: req.ServerId,
    }, nil
}

// ListRemoteServers lists all registered servers (IDs only, no credentials)
func (s *AdminService) ListRemoteServers(
    ctx context.Context,
    req *livekit.ListRemoteServersRequest,
) (*livekit.ListRemoteServersResponse, error) {
    if !hasAdminGrant(ctx) {
        return nil, ErrUnauthorized
    }
    
    serverIDs, err := s.registry.ListServers(ctx)
    if err != nil {
        return nil, err
    }
    
    return &livekit.ListRemoteServersResponse{
        ServerIds: serverIDs,
    }, nil
}

// DeleteRemoteServer removes a registered server
func (s *AdminService) DeleteRemoteServer(
    ctx context.Context,
    req *livekit.DeleteRemoteServerRequest,
) (*livekit.DeleteRemoteServerResponse, error) {
    if !hasAdminGrant(ctx) {
        return nil, ErrUnauthorized
    }
    
    if err := s.registry.DeleteServer(ctx, req.ServerId); err != nil {
        return nil, err
    }
    
    return &livekit.DeleteRemoteServerResponse{}, nil
}
```

---

### Phase 4: Remote Validation Using Stored Credentials

**File:** `pkg/gateway/remote_validator.go`

```go
package gateway

import (
    "context"
    "fmt"
    "sync"
    "time"
    
    "github.com/livekit/protocol/livekit"
)

type RemoteValidator struct {
    registry    *ServerRegistry
    cache       *sync.Map // map[string]*CachedRoomInfo
    cacheTTL    time.Duration
    httpClient  *http.Client
}

type CachedRoomInfo struct {
    RoomSID   string
    RoomName  string
    ExpiresAt time.Time
}

func NewRemoteValidator(registry *ServerRegistry, cacheTTL time.Duration) *RemoteValidator {
    return &RemoteValidator{
        registry: registry,
        cache:    &sync.Map{},
        cacheTTL: cacheTTL,
        httpClient: &http.Client{
            Timeout: 2 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
                ForceAttemptHTTP2:   true,
            },
        },
    }
}

// ValidateRoom validates a room exists on the remote server
func (v *RemoteValidator) ValidateRoom(ctx context.Context, serverID, roomName string) (*RemoteRoomInfo, error) {
    // Check cache first
    cacheKey := fmt.Sprintf("%s:%s", serverID, roomName)
    if cached, ok := v.cache.Load(cacheKey); ok {
        info := cached.(*CachedRoomInfo)
        if time.Now().Before(info.ExpiresAt) {
            return &RemoteRoomInfo{
                RoomSID:  info.RoomSID,
                RoomName: info.RoomName,
            }, nil
        }
        v.cache.Delete(cacheKey)
    }
    
    // Get server credentials from registry
    server, err := v.registry.GetServer(ctx, serverID)
    if err != nil {
        return nil, fmt.Errorf("server not found: %s", serverID)
    }
    
    // Validate room on remote server
    roomInfo, err := v.validateRemote(ctx, server, roomName)
    if err != nil {
        return nil, err
    }
    
    // Cache the result
    v.cache.Store(cacheKey, &CachedRoomInfo{
        RoomSID:   roomInfo.RoomSID,
        RoomName:  roomInfo.RoomName,
        ExpiresAt: time.Now().Add(v.cacheTTL),
    })
    
    return roomInfo, nil
}

func (v *RemoteValidator) validateRemote(ctx context.Context, server *RemoteServerInfo, roomName string) (*RemoteRoomInfo, error) {
    // Create LiveKit client for remote server
    client := livekit.NewRoomServiceClient(server.Host, server.APIKey, server.APISecret)
    
    // List rooms to check if it exists
    rooms, err := client.ListRooms(ctx, &livekit.ListRoomsRequest{
        Names: []string{roomName},
    })
    if err != nil {
        return nil, fmt.Errorf("failed to validate room on remote server: %w", err)
    }
    
    if len(rooms.Rooms) == 0 {
        return nil, fmt.Errorf("room not found: %s", roomName)
    }
    
    return &RemoteRoomInfo{
        RoomSID:  rooms.Rooms[0].Sid,
        RoomName: rooms.Rooms[0].Name,
    }, nil
}
```

---

### Phase 5: Integrate into Egress Service

**File:** `pkg/service/egress.go` (modifications)

```go
type EgressService struct {
    launcher        rtc.EgressLauncher
    client          rpc.EgressClient
    io              IOClient
    roomService     livekit.RoomService
    gatewayMode     bool                    // NEW
    remoteValidator *gateway.RemoteValidator // NEW
    serverRegistry  *gateway.ServerRegistry  // NEW
}

func (s *EgressService) StartRoomCompositeEgress(ctx context.Context, req *livekit.RoomCompositeEgressRequest) (*livekit.EgressInfo, error) {
    // Check if gateway mode and media_server_id provided
    if s.gatewayMode && req.MediaServerId != "" {
        return s.startGatewayEgress(ctx, req)
    }
    
    // Normal mode - existing logic
    return s.startEgress(ctx, &rpc.StartEgressRequest{
        Request: &rpc.StartEgressRequest_RoomComposite{
            RoomComposite: req,
        },
    })
}

func (s *EgressService) startGatewayEgress(ctx context.Context, req *livekit.RoomCompositeEgressRequest) (*livekit.EgressInfo, error) {
    // Validate room on remote media server
    roomInfo, err := s.remoteValidator.ValidateRoom(ctx, req.MediaServerId, req.RoomName)
    if err != nil {
        return nil, err
    }
    
    // Get media server credentials for token generation
    server, err := s.serverRegistry.GetServer(ctx, req.MediaServerId)
    if err != nil {
        return nil, err
    }
    
    // Generate token for egress to connect to remote server
    token := generateRemoteToken(server, req.RoomName, roomInfo.RoomSID)
    
    // Start egress with remote server details
    return s.launcher.StartEgress(ctx, &rpc.StartEgressRequest{
        Request: &rpc.StartEgressRequest_RoomComposite{
            RoomComposite: req,
        },
        RoomId:      roomInfo.RoomSID,
        RemoteToken: token, // Token for remote server
    })
}
```

---

### Phase 6: Testing

**Test Scenarios:**

1. **Server Registration:**
   - Register new server
   - List servers
   - Delete server
   - Verify credentials encrypted in Redis

2. **Egress with Gateway:**
   - Start egress with valid server_id
   - Start egress with invalid server_id
   - Verify room validation on remote server
   - Verify cache hit/miss behavior

3. **Security:**
   - Verify non-admin cannot register servers
   - Verify credentials encrypted in Redis
   - Verify credentials never exposed in API responses

4. **Performance:**
   - Cache hit latency < 1ms
   - Cache miss latency < 200ms
   - Concurrent requests

---

### Phase 7: Documentation

**Client Usage Examples:**

**Option 1: Using Config File Servers (Simplest)**

```yaml
# Gateway config file
egress_gateway:
  enabled: true
  remote_servers:
    prod-server-1:
      host: "https://livekit.example.com"
      api_key: "APIxxxxxx"
      api_secret: "secretxxxxxx"
```

```javascript
// Client just uses the media_server_id from config
const client = new RoomServiceClient(gatewayUrl, gatewayApiKey, gatewayApiSecret);
const egressInfo = await client.startRoomCompositeEgress({
  room_name: 'my-room',
  media_server_id: 'prod-server-1', // Defined in config
  layout: 'grid',
  file: { /* ... */ },
});
```

**Option 2: Using Dynamic Redis Registration (Most Flexible)**

```javascript
const { RoomServiceClient, AccessToken } = require('livekit-server-sdk');

// 1. Admin registers remote server dynamically (no config change needed)
const adminToken = new AccessToken(gatewayApiKey, gatewayApiSecret, {
  identity: 'admin',
});
adminToken.addGrant({ roomAdmin: true });

const adminClient = new RoomServiceClient(gatewayUrl, gatewayApiKey, gatewayApiSecret);
await adminClient.registerRemoteServer({
  server_id: 'new-server',
  host: 'https://new.livekit.example.com',
  api_key: 'APIxxxxxx',
  api_secret: 'secretxxxxxx',
});

// 2. Client uses it immediately (no gateway restart needed)
const client = new RoomServiceClient(gatewayUrl, gatewayApiKey, gatewayApiSecret);
const egressInfo = await client.startRoomCompositeEgress({
  room_name: 'my-room',
  media_server_id: 'new-server', // Dynamically registered
  layout: 'grid',
  file: {
    filepath: 'recording-{time}.mp4',
    output: {
      s3: {
        access_key: 's3-key',
        secret: 's3-secret',
        region: 'us-west-2',
        bucket: 'recordings',
      },
    },
  },
});
```

**Lookup Priority:**
1. Config file `remote_servers` (checked first)
2. Redis dynamic registrations (checked if not in config)
3. Error if not found in either

**Best Practices:**
- Use **config file** for stable, long-term servers (production, staging)
- Use **Redis registration** for temporary or frequently changing servers
- Config servers can't be deleted via API (immutable)
- Redis servers can be added/updated/deleted dynamically

---

## Comparison: Old vs New Plan

| Aspect | Original Plan (Encryption) | New Plan (Config + Redis) |
|--------|---------------------------|---------------------------|
| **Client Complexity** | Must encrypt credentials | Just pass server_id |
| **Security** | Encrypted in transit | Encrypted at rest in Redis |
| **Flexibility** | Dynamic servers | Static (config) + Dynamic (Redis) |
| **Redeployment** | Not needed | Not needed for Redis servers |
| **Components** | 9+ components | 5 components |
| **Key Management** | Primary + fallback keys | Single master key |
| **Client Libraries** | Need encryption code | No special code needed |
| **Maintainability** | Complex | Simple |
| **Configuration** | N/A | Config file OR Redis OR both |

---

## Success Criteria

1. ✅ Gateway mode can be enabled via configuration
2. ✅ Egress service runs independently with separate Redis
3. ✅ Remote servers can be registered without redeployment
4. ✅ Credentials encrypted at rest in Redis
5. ✅ All existing egress types work in gateway mode
6. ✅ Zero regression in normal mode
7. ✅ Performance meets requirements (< 200ms validation)
8. ✅ Simple client integration (just pass server_id)

---

**Document Version**: 2.0 (Simplified)  
**Last Updated**: January 30, 2026  
**Status**: Ready for Implementation
