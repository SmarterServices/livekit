# LiveKit Egress Gateway - Overview

## What is the Egress Gateway?

The **LiveKit Egress Gateway** is a specialized deployment mode that allows you to run LiveKit's egress services (recording, streaming, and track export) as a standalone service, completely independent from your main LiveKit media servers.

Think of it as a dedicated egress processing hub that can handle recording and streaming for multiple LiveKit servers without being tied to any single one.

---

## Why Use an Egress Gateway?

### **Problem: Tight Coupling**

In a standard LiveKit deployment:
- Egress services run on the same infrastructure as your media servers
- Each LiveKit server manages its own egress operations
- Scaling egress requires scaling entire server instances
- Egress state is tied to the server's Redis instance

### **Solution: Dedicated Gateway**

With the Egress Gateway:
- **Decouple egress processing** from media servers
- **Centralize egress operations** across multiple LiveKit servers
- **Scale independently** - add egress capacity without adding media servers
- **Simplify operations** - manage egress in one place

---

## Key Benefits

### 🎯 **1. Independent Scaling**
Scale your egress capacity independently from your media servers. Need more recording capacity? Just add more egress workers to the gateway - no need to provision additional media servers.

### 🔄 **2. Multi-Server Support**
One gateway can handle egress for multiple LiveKit servers:
- Production and staging environments
- Multiple regional deployments
- Different customer instances

### 💰 **3. Cost Optimization**
- Run egress-heavy workloads on cost-optimized instances
- Keep media servers lean and focused on real-time processing
- Reduce overall infrastructure costs

### 🛠️ **4. Operational Simplicity**
- Single point of management for all egress operations
- Centralized monitoring and logging
- Easier troubleshooting and debugging

### 🔒 **5. Security & Isolation**
- Isolate egress processing from media servers
- Separate Redis instances for better security
- Control which servers can use the gateway

---

## How It Works

### **Architecture Overview**

```
┌─────────────────────────────────────────────────────────────┐
│                    Your Application                         │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ Egress API Call
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Egress Gateway                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ 1. Receives egress request with server_id           │  │
│  │ 2. Looks up remote server credentials               │  │
│  │ 3. Validates room exists on remote server           │  │
│  │ 4. Generates access token for remote server         │  │
│  │ 5. Starts egress worker                             │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ Connects to room
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Remote LiveKit Media Server                    │
│                  (Production/Staging/etc)                   │
└─────────────────────────────────────────────────────────────┘
```

### **Step-by-Step Flow**

1. **Your application** calls the gateway's egress API
2. **Gateway** receives request with `media_server_id` parameter
3. **Gateway** looks up the media server's credentials (from config or Redis)
4. **Gateway** validates the room exists on the remote server
5. **Gateway** generates an access token using the remote server's credentials
6. **Egress worker** connects to the remote server and starts recording/streaming
7. **Output** is saved to your configured destination (S3, etc.)

---

## Use Cases

### **1. Multi-Environment Setup**
Run a single gateway for production, staging, and development environments:
```
Gateway → Production Server (recordings)
       → Staging Server (testing)
       → Dev Server (development)
```

### **2. Regional Deployments**
Centralize egress for multiple regional media servers:
```
Gateway → US-East Server
       → US-West Server
       → EU Server
       → APAC Server
```

### **3. Multi-Tenant SaaS**
One gateway serving multiple customer instances:
```
Gateway → Customer A's LiveKit Server
       → Customer B's LiveKit Server
       → Customer C's LiveKit Server
```

### **4. Cost-Optimized Recording**
Run media servers on high-performance instances, egress on cheaper compute:
```
Media Servers: c5.2xlarge (optimized for real-time)
Egress Gateway: c5.large (optimized for cost)
```

---

## Server Registration Options

The gateway supports two ways to configure remote LiveKit servers:

### **Option 1: Config File (Static)**

Best for stable, long-term servers that rarely change.

**Pros:**
- ✅ Simple configuration
- ✅ No API calls needed
- ✅ Credentials never leave the gateway
- ✅ Survives gateway restarts

**Cons:**
- ❌ Requires gateway restart to add/remove servers
- ❌ Not suitable for dynamic environments

**When to use:**
- Production and staging environments
- Fixed infrastructure
- Security-sensitive deployments

### **Option 2: Redis (Dynamic)**

Best for frequently changing servers or multi-tenant scenarios.

**Pros:**
- ✅ Add/update/delete servers without restart
- ✅ Perfect for multi-tenant SaaS
- ✅ API-driven management
- ✅ Credentials encrypted at rest

**Cons:**
- ❌ Requires admin API access
- ❌ Depends on Redis availability

**When to use:**
- Multi-tenant SaaS platforms
- Dynamic customer onboarding
- Temporary/test environments
- Frequently changing infrastructure

### **Option 3: Hybrid (Best of Both)**

Use config file for stable servers, Redis for dynamic ones.

**Example:**
- Config: Production and staging servers
- Redis: Customer servers, temporary environments

---

## Setup Overview

### **1. Deploy Gateway**

Deploy the LiveKit server in gateway mode:
- Enable gateway mode in configuration
- Configure separate Redis instance
- Set master encryption key
- Optionally define static servers

### **2. Register Remote Servers**

**Static (Config File):**
Add servers to your gateway configuration file and restart.

**Dynamic (API):**
Use the admin API to register servers on-the-fly.

### **3. Start Using**

Your application calls the gateway's egress API with:
- Standard egress parameters (room name, layout, output, etc.)
- `media_server_id` parameter to specify which media server

The gateway handles the rest!

---

## Security Model

### **Credential Storage**

**Config File Servers:**
- Credentials stored in gateway config file
- Protected by file system permissions
- Never transmitted over network

**Redis Servers:**
- Credentials encrypted with AES-256-GCM before storage
- Master encryption key stored on gateway only
- Encrypted at rest in Redis

### **Access Control**

**Egress API:**
- Standard LiveKit authentication (API key/secret)
- Requires `roomRecord` grant
- Same security model as normal egress

**Admin API:**
- Requires `roomAdmin` grant
- Only admins can register/delete servers
- Audit trail of who registered what

### **Network Security**

- Use TLS for all connections (gateway ↔ Redis, gateway ↔ remote servers)
- Credentials never transmitted in plain text
- Server IDs are non-sensitive (safe to log)

---

## Monitoring & Operations

### **Metrics to Track**

- **Egress success/failure rates** per media_server_id
- **Remote validation latency** (should be <200ms)
- **Cache hit rates** (should be >80%)
- **Active egress sessions** per remote server
- **Server registry size** (config + Redis)

### **Logging**

The gateway logs:
- Server registration/deletion events
- Remote validation attempts
- Cache hits/misses
- Egress start/stop events
- Errors and failures

### **Health Checks**

Monitor:
- Gateway API availability
- Redis connectivity
- Remote server reachability
- Egress worker capacity

---

## Comparison: Standard vs Gateway Mode

| Aspect | Standard LiveKit | Egress Gateway |
|--------|------------------|----------------|
| **Deployment** | Egress runs on media server | Egress runs separately |
| **Scaling** | Scale entire server | Scale egress independently |
| **Multi-Server** | One server at a time | Multiple servers supported |
| **Redis** | Shared with media server | Dedicated Redis instance |
| **Cost** | Higher (bundled resources) | Lower (optimized instances) |
| **Complexity** | Simpler (single service) | Moderate (separate service) |
| **Use Case** | Single server deployments | Multi-server, multi-tenant |

---

## Migration Path

### **From Standard to Gateway**

1. **Deploy gateway** alongside existing LiveKit server
2. **Register your server** in the gateway (config or API)
3. **Update application** to call gateway instead of server
4. **Test thoroughly** with non-production traffic
5. **Gradually migrate** production traffic
6. **Monitor and optimize** performance

### **Backward Compatibility**

The gateway maintains full API compatibility with standard LiveKit egress:
- Same API endpoints
- Same request/response formats
- Only addition: `media_server_id` parameter

Existing egress code works with minimal changes!

---

## Limitations & Considerations

### **Current Limitations**

- One remote server per egress request (no multi-server composites)
- Remote validation adds latency (~100-200ms on cache miss)
- Requires network connectivity to remote servers
- Admin API needed for dynamic server management

### **Best Practices**

- Use config file for stable, long-term servers
- Use Redis for dynamic, frequently changing servers
- Enable caching for optimal performance
- Monitor remote server health
- Set appropriate timeouts
- Use TLS for all connections

### **When NOT to Use**

- Single LiveKit server deployment (use standard mode)
- Extremely latency-sensitive scenarios
- No need for independent scaling
- Simple, small-scale deployments

---

## Getting Started

Ready to deploy an Egress Gateway? See the implementation plan for technical details:
- `EGRESS_GATEWAY_IMPLEMENTATION_PLAN.md` - Technical implementation guide
- `EGRESS_GATEWAY.md` - Original requirements document

### **Quick Start Checklist**

- [ ] Review architecture and benefits
- [ ] Determine server registration strategy (config vs Redis)
- [ ] Plan deployment infrastructure
- [ ] Configure gateway settings
- [ ] Register remote servers
- [ ] Update application to use gateway
- [ ] Test with non-production traffic
- [ ] Monitor and optimize

---

## Support & Resources

For questions, issues, or contributions:
- Technical implementation: See `EGRESS_GATEWAY_IMPLEMENTATION_PLAN.md`
- LiveKit documentation: https://docs.livekit.io
- Community support: https://livekit.io/community

---

**Document Version**: 1.0  
**Last Updated**: January 30, 2026  
**Status**: Ready for Review
