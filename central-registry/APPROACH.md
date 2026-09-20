# Central Registry Approach - Implementation Guide

## Overview
This document describes the **Central Registry Pattern** for routing messages between users connected to different nodes.

---

## Architecture Diagram

```
┌─────────────┐                         ┌─────────────┐
│  Client 1   │                         │  Client 2   │
│  (User 1)   │                         │  (User 2)   │
│             │                         │             │
│  WS ────────┼─────────────────────────┼──────── WS  │
└──────┬──────┘                         └──────┬──────┘
       │                                       │
       ▼                                       ▼
┌─────────────────────────┐         ┌─────────────────────────┐
│       NODE A            │         │       NODE B            │
│  (localhost:3001)       │         │  (localhost:3002)       │
│                         │         │                         │
│  ┌───────────────────┐  │         │  ┌───────────────────┐  │
│  │ WS Hub            │  │         │  │ WS Hub            │  │
│  │ - clients[user]   │  │         │  │ - clients[user]   │  │
│  │ - register/unreg  │  │         │  │ - register/unreg  │  │
│  └─────────┬─────────┘  │         │  └─────────┬─────────┘  │
│            │            │         │            │            │
│  ┌─────────▼─────────┐  │         │  ┌─────────▼─────────┐  │
│  │ Registry Client   │  │         │  │ Registry Client   │  │
│  │ - registerNode()  │  │         │  │ - registerNode()  │  │
│  │ - reportPresence()│  │         │  │ - reportPresence()│  │
│  │ - lookupUser()    │  │         │  │ - lookupUser()    │  │
│  └────────┬──────────┘  │         │  └────────┬──────────┘  │
│           │             │         │           │             │
│  ┌────────▼──────────┐  │         │  ┌────────▼──────────┐  │
│  │ Message Router    │  │         │  │ Message Handler   │  │
│  │ - routeMessage()  │  │         │  │ - handleForward() │  │
│  │ - forwardToNode() │──┼─────────┼─▶│ - deliverLocal()  │  │
│  └───────────────────┘  │  HTTP   │  └───────────────────┘  │
└─────────────────────────┘         └─────────────────────────┘
           │                               ▲
           │        ┌────────────────┐     │
           └───────▶│  REGISTRY      │─────┘
                    │  (localhost:8080)     │
                    │                     │
                    │  user_id → NodeInfo │
                    │  {node_id, addr}    │
                    └─────────────────────┘
```
---

## API Contracts

### Registry Server (Port 8080)

| Method | Endpoint | Request Body | Response |
|--------|----------|--------------|----------|
| POST | `/nodes/register` | `{node_id, address, client_address}` | `{success: true}` |
| POST | `/presence` | `{user_id, node_id}` | `{success: true}` |
| DELETE | `/presence/:user_id` | - | `{success: true}` |
| GET | `/lookup/:user_id` | - | `{node_id, address, client_address}` or 404 |
| GET | `/nodes/list` | - | `{nodes: [...]}` |
| GET | `/assign` | Query: `user_id` | `{address, node_id, internal_address}` |
| GET | `/users` | - | `{users: {user_id: {node_id, internal_address, client_address}, ...}}` |
| GET | `/health` | - | `{status: "ok"}` |
| OPTIONS | * (all endpoints) | - | `204 No Content` (CORS preflight) |

### CORS Support (Registry & Node)

All HTTP endpoints (both Registry and Node) include CORS headers via middleware (`corsMiddleware` in respective `main.go` files):

- **Access-Control-Allow-Origin**: `*` (all origins - adjust for production)
- **Access-Control-Allow-Methods**: `GET, POST, PUT, DELETE, OPTIONS`
- **Access-Control-Allow-Headers**: `Content-Type, Authorization` (Registry), plus WebSocket headers (Node)
- **Access-Control-Allow-Credentials**: `true`
- **Access-Control-Max-Age**: `86400` (24 hours)

Preflight OPTIONS requests are handled by returning `204 No Content` immediately.

> **Production Note**: When using credentials (`Access-Control-Allow-Credentials: true`), the origin cannot be `*`. Configure specific allowed origins in production.

---

## Request Flow Diagrams

### 1. User Login & Connection Flow

```
CLIENT (Browser)                    REGISTRY (8080)              NODE A (3001) / NODE B (3002)
     │                                  │                              │
     │── GET /assign?user_id=alice ────▶│                              │
     │◀── {address, node_id,            │                              │
     │     internal_address} ───────────│                              │
     │                                  │                              │
     │                                  │                              │
     │────── WS /ws?user=alice ────────▶│                              │
     │◀───── WS 101 Switching Protocols │                              │
     │                                  │                              │
     │                                  │── POST /presence             │
     │                                  │    {user_id: "alice",        │
     │                                  │     node_id: "node-a"}       │
     │                                  │◀── {success: true}           │
     │                                  │                              │
     │◀──────── "connected" ack ────────│                              │
     │                                  │                              │

Key Steps:
1. Client calls Registry /assign to get node assignment (round-robin)
2. Client opens WebSocket to assigned node's client_address
3. Node registers user presence with Registry via HTTP POST /presence
4. Registry stores user_id → NodeInfo mapping
5. Node sends "connected" confirmation to client
```

### 2. Message Sending Flow (Same Node - Local Delivery)

```
ALICE (Node A)                      NODE A (3001)                    BOB (Node A)
     │                                  │                              │
     │─── WS: {type:"chat",             │                              │
     │      to:"bob",                   │                              │
     │      content:"Hi!",              │                              │
     │      msg_id:"msg_1"} ───────────▶│                              │
     │                                  │                              │
     │                                  │── deliverLocal("bob", msg)   │
     │                                  │                              │
     │◀─── WS: {type:"ack",             │                              │
     │      msg_id:"msg_1"} ────────────│                              │
     │                                  │                              │
     │                                  │                              │── WS: {type:"chat",
     │                                  │                              │      from:"alice",
     │                                  │                              │      content:"Hi!",
     │                                  │                              │      msg_id:"msg_1"}▶
     │                                  │                              │

Key Steps:
1. Alice sends chat message via WebSocket to Node A
2. Node A checks if recipient (Bob) is in local hub.clients map
3. If local: deliverLocal() sends message directly to Bob's WebSocket
4. Node A sends ACK to Alice
```

### 3. Message Sending Flow (Cross-Node - Remote Delivery)

```
ALICE (Node A)                      NODE A (3001)              REGISTRY (8080)        NODE B (3002)        BOB (Node B)
     │                                  │                              │                    │                   │
     │─── WS: {type:"chat",             │                              │                    │                   │
     │      to:"bob",                   │                              │                    │                   │
     │      content:"Hi!",              │                              │                    │                   │
     │      msg_id:"msg_1"} ───────────▶│                              │                    │                   │
     │                                  │                              │                    │                   │
     │                                  │── GET /lookup/bob ─────────▶│                    │                   │
     │                                  │◀── {node_id:"node-b",        │                    │                   │
     │                                  │     address:"http://node-b:3000",                 │                   │
     │                                  │     client_address:"http://localhost:3002"}       │                   │
     │                                  │                              │                    │                   │
     │                                  │                              │                    │                   │
     │                                  │── POST /internal/forward     │                    │                   │
     │                                  │    {to_user:"bob",           │                    │                   │
     │                                  │     from_user:"alice",       │                    │                   │
     │                                  │     content:"Hi!",           │                    │                   │
     │                                  │     msg_id:"msg_1",          │                    │                   │
     │                                  │     timestamp:1234567890}───▶│                    │                   │
     │                                  │                              │                    │                   │
     │◀─── WS: {type:"ack",             │◀── {success: true} ────────│                    │                   │
     │      msg_id:"msg_1"} ────────────│                              │                    │                   │
     │                                  │                              │                    │                   │
     │                                  │                              │                    │── deliverLocal(
     │                                  │                              │                    │     "bob", msg)   │
     │                                  │                              │                    │                   │
     │                                  │                              │                    │                   │── WS: {type:"chat",
     │                                  │                              │                    │                   │      from:"alice",
     │                                  │                              │                    │                   │      content:"Hi!",
     │                                  │                              │                    │                   │      msg_id:"msg_1"}▶
     │                                  │                              │                    │                   │

Key Steps:
1. Alice sends chat message via WebSocket to Node A
2. Node A checks local hub - Bob NOT found
3. Node A calls Registry GET /lookup/bob to find Bob's node
4. Registry returns Node B's internal_address (http://node-b:3000)
5. Node A forwards message via HTTP POST /internal/forward to Node B's internal address
6. Node B receives forward, delivers to Bob via local WebSocket
7. Node A sends ACK to Alice (after successful forward)
```

### 4. User Discovery Flow (Periodic / Manual Refresh)

```
CLIENT (Browser)                    REGISTRY (8080)
     │                                  │
     │── GET /users ───────────────────▶│
     │                                  │
     │◀── {users: {                      │
     │       "alice": {                 │
     │         node_id: "node-a",       │
     │         internal_address:        │
     │           "http://node-a:3000",  │
     │         client_address:          │
     │           "http://localhost:3001"│
     │       },                         │
     │       "bob": {                   │
     │         node_id: "node-b",       │
     │         internal_address:        │
     │           "http://node-b:3000",  │
     │         client_address:          │
     │           "http://localhost:3002"│
     │       }                          │
     │     }} ──────────────────────────│
     │                                  │

Key Steps:
1. Client calls GET /users (on connect + every 30s + manual refresh)
2. Registry returns all online users with full node info
3. Client renders user list showing node_id and client_address for each user
```

### 5. Node Registration & Heartbeat Flow

```
NODE A (3001)                       REGISTRY (8080)
     │                                  │
     │── POST /nodes/register ─────────▶│
     │    {node_id:"node-a",            │
     │     address:"http://node-a:3000",│
     │     client_address:"http://localhost:3001"}
     │                                  │
     │◀── {success: true} ─────────────│
     │                                  │
     │     (runs in background)         │
     │                                  │
     │──── POST /nodes/register ───────▶│  (every 30s - re-register)
     │◀── {success: true} ─────────────│
     │                                  │

Key Steps:
1. Node registers itself on startup with internal + client addresses
2. Node re-registers every 30s (heartbeat) to maintain presence
3. Registry uses this for /nodes/list and health checks
```

### 6. User Disconnect / Presence Cleanup Flow

```
CLIENT                          NODE A (3001)              REGISTRY (8080)
     │                              │                          │
     │── WS Close ────────────────▶│                          │
     │                              │                          │
     │                              │── DELETE /presence/alice▶│
     │                              │◀── {success: true}       │
     │                              │                          │

Key Steps:
1. Client WebSocket closes (browser close, network issue, etc.)
2. Node detects disconnect in readPump (ReadJSON error)
3. Node calls Registry DELETE /presence/:user_id to remove mapping
4. User no longer appears in /users or /lookup
```

---

### Node HTTP Endpoints (Port 3001, 3002)

| Method | Endpoint | Request Body | Response |
|--------|----------|--------------|----------|
| POST | `/internal/forward` | `{to_user, from_user, content, msg_id, timestamp}` | `{delivered: bool}` |
| GET | `/health` | - | `{status: "ok"}` |
| OPTIONS | * (all endpoints) | - | `204 No Content` (CORS preflight) |

### WebSocket Protocol (Client ↔ Node)

**Client → Server:**
```json
{ "type": "chat", "to": "user_2", "content": "Hello!", "msg_id": "..." }
{ "type": "ping" }
```

**Server → Client:**
```json
{ "type": "chat", "from": "user_1", "content": "Hello!", "msg_id": "...", "timestamp": 1234567890 }
{ "type": "ack", "msg_id": "..." }
{ "type": "error", "error": "User not found" }
{ "type": "pong" }
```

---

## Component Responsibilities

### Registry Server
- **Single instance** (can be made HA later with Raft/etcd)
- In-memory map: `map[string]NodeInfo` (user_id → NodeInfo)
- Thread-safe with `sync.RWMutex`
- RESTful HTTP API only (no WebSocket)

### Chat Node (runs multiple instances)
- **WebSocket Hub**: Manages connected clients per node
- **Registry Client**: Communicates with registry via HTTP
- **Message Router**: Forwards messages to other nodes
- **HTTP Server**: Receives forwarded messages from other nodes
- **CORS Middleware**: Adds cross-origin headers to all HTTP responses (in `main.go`)

### Client (HTML/JS)
- Single WebSocket connection to assigned node
- Simple UI: user list with node address display, chat area, message input
- Auto-reconnect on disconnect
- User discovery via registry `/users` endpoint (periodic refresh + manual refresh button)
- Shows node ID and client address for each connected user

---

## File Structure

```
central-registry/
├── shared/
│   └── types.go           # Shared structs & constants
├── registry/
│   ├── main.go            # Registry server entry + CORS middleware
│   ├── server.go          # HTTP handlers
│   └── store.go           # In-memory user→node map
├── node/
│   ├── main.go            # Node entry point + CORS middleware
│   ├── hub.go             # WebSocket hub (clients)
│   ├── registry_client.go # Talks to registry
│   ├── router.go          # Message routing logic
│   ├── ws_handler.go      # WebSocket upgrader + pumps
│   └── http_handler.go    # Internal HTTP (receive forwards)
├── client/
│   ├── index.html         # Simple chat UI
│   └── app.js             # WebSocket client logic
├── docker-compose.yml     # Multi-container orchestration
└── README.md
```

---

## Implementation Order (Piece by Piece)

1. **shared/types.go** - Common data structures
2. **registry/store.go** - In-memory user→node registry
3. **registry/server.go** - HTTP handlers for registry API
4. **registry/main.go** - Registry server startup
5. **node/registry_client.go** - HTTP client for registry
6. **node/hub.go** - WebSocket client management
7. **node/ws_handler.go** - WebSocket connection handling
8. **node/router.go** - Message routing to other nodes
9. **node/http_handler.go** - Receive forwarded messages
10. **node/main.go** - Wire everything together
11. **client/index.html** - Simple chat UI
12. **client/app.js** - Client WebSocket logic
13. **docker-compose.yml** - Run all containers locally