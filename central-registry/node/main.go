package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"central-registry/shared"
)

// Client represents a connected WebSocket client.
// UserID is the unique identifier for the user.
// Send is a buffered channel for outgoing messages to this client.
type Client struct {
	UserID string
	Send   chan shared.WSMessage
}

// Hub manages all connected clients on this node.
// clients maps userID to their Client instance.
// mu protects concurrent access to the clients map.
type Hub struct {
	clients map[string]*Client
	mu      sync.Mutex
}

// NewHub creates and returns a new Hub with an empty clients map.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

// Register adds a new client to the hub for the given userID.
// Returns the created Client with a buffered send channel (256 messages).
// Thread-safe: uses mutex to protect the clients map.
func (h *Hub) Register(userID string) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &Client{UserID: userID, Send: make(chan shared.WSMessage, 256)}
	h.clients[userID] = c
	return c
}

// Unregister removes a client from the hub and closes their send channel.
// Thread-safe: uses mutex to protect the clients map.
// Safe to call multiple times for the same userID.
func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[userID]; ok {
		close(c.Send)
		delete(h.clients, userID)
	}
}

// SendToUser attempts to deliver a message to a specific user on this node.
// Returns true if the message was queued for delivery, false if user not found or channel full.
// Thread-safe: uses mutex to look up the client, then non-blocking send to avoid deadlocks.
func (h *Hub) SendToUser(userID string, msg shared.WSMessage) bool {
	h.mu.Lock()
	c, ok := h.clients[userID]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case c.Send <- msg:
		return true
	default:
		return false
	}
}

// RegistryClient handles HTTP communication with the central registry.
// registryURL: base URL of the registry service (e.g., http://registry:8080).
// nodeID: unique identifier for this chat node.
// nodeAddr: internal Docker address for node-to-node communication.
// clientAddr: external address for browser WebSocket connections.
// httpClient: HTTP client for making requests to the registry.
type RegistryClient struct {
	registryURL string
	nodeID      string
	nodeAddr    string
	clientAddr  string
	httpClient  *http.Client
}

// NewRegistryClient creates a new RegistryClient with the provided configuration.
// u: registry base URL, id: node ID, a: internal node address, ca: client-facing address.
func NewRegistryClient(u, id, a, ca string) *RegistryClient {
	return &RegistryClient{registryURL: u, nodeID: id, nodeAddr: a, clientAddr: ca, httpClient: &http.Client{}}
}

// RegisterNode registers this chat node with the central registry.
// Sends node_id, internal address, and client address to the registry.
// Called once at node startup.
func (c *RegistryClient) RegisterNode() error {
	body, _ := json.Marshal(map[string]string{"node_id": c.nodeID, "address": c.nodeAddr, "client_address": c.clientAddr})
	resp, err := c.httpClient.Post(c.registryURL+"/nodes/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// SetUserPresence informs the registry that a user is now connected to this node.
// Called when a user's WebSocket connection is established.
// Sends user_id and this node's node_id to the registry's /presence endpoint.
func (c *RegistryClient) SetUserPresence(uid string) error {
	body, _ := json.Marshal(map[string]string{"user_id": uid, "node_id": c.nodeID})
	resp, err := c.httpClient.Post(c.registryURL+"/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// RemoveUserPresence informs the registry that a user has disconnected from this node.
// Called when a user's WebSocket connection closes.
// Sends DELETE request to the registry's /presence/:user_id endpoint.
func (c *RegistryClient) RemoveUserPresence(uid string) error {
	body, _ := json.Marshal(map[string]string{"user_id": uid, "node_id": c.nodeID})
	req, _ := http.NewRequest(http.MethodDelete, c.registryURL+"/presence/"+uid, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// LookupUser queries the registry to find which node a user is connected to.
// Returns NodeInfo containing the target node's ID, internal address, and client address.
// Returns (empty NodeInfo, nil) if user not found (404).
// Returns error if the registry request fails.
func (c *RegistryClient) LookupUser(uid string) (shared.NodeInfo, error) {
	resp, err := c.httpClient.Get(c.registryURL + "/lookup/" + uid)
	if err != nil {
		return shared.NodeInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return shared.NodeInfo{}, nil
	}
	var ni shared.NodeInfo
	json.NewDecoder(resp.Body).Decode(&ni)
	return ni, nil
}

// Router handles message routing between users.
// It routes messages locally if the recipient is on this node,
// or forwards them to another node via the registry.
// registry: client for querying the central registry.
// httpClient: HTTP client for node-to-node forwarding (with timeout).
// hub: reference to the local Hub for delivering messages to connected clients.
type Router struct {
	registry   *RegistryClient
	httpClient *http.Client
	hub        *Hub
}

// NewRouter creates a new Router with the given RegistryClient.
// Initializes an HTTP client with a 5-second timeout for forwarding requests.
func NewRouter(r *RegistryClient) *Router {
	return &Router{registry: r, httpClient: &http.Client{Timeout: 5 * time.Second}}
}

// SetHub sets the Hub reference for local message delivery.
// Called after Hub creation to wire up the router.
func (r *Router) SetHub(h *Hub) { r.hub = h }

// RouteMessage routes a chat message to its recipient.
// If recipient is on this node (hub.SendToUser succeeds), delivers locally and sends ack to sender.
// Otherwise, looks up the recipient's node via the registry and forwards the message there.
// If user not found in registry, sends error to sender.
func (r *Router) RouteMessage(msg shared.WSMessage) {
	if msg.To == "" {
		log.Println("missing to")
		return
	}
	if r.hub != nil && r.hub.SendToUser(msg.To, msg) {
		r.hub.SendToUser(msg.From, shared.WSMessage{Type: "ack", MsgID: msg.MsgID, To: msg.From})
		return
	}
	ni, err := r.registry.LookupUser(msg.To)
	if err != nil || ni.Address == "" {
		r.sendError(msg.From, "User not found: "+msg.To)
		return
	}
	r.forward(ni.Address, msg)
}

// forward sends a message to another node via HTTP POST to /internal/forward.
// Converts WSMessage to ForwardMessage format for node-to-node transport.
// On success, sends ack to the original sender.
// On failure, logs error and sends error to sender.
func (r *Router) forward(addr string, msg shared.WSMessage) {
	fwd := shared.ForwardMessage{MsgID: msg.MsgID, FromUser: msg.From, ToUser: msg.To, Content: msg.Content, Timestamp: msg.Timestamp}
	body, _ := json.Marshal(fwd)
	resp, err := r.httpClient.Post(addr+"/internal/forward", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Println("fwd fail:", err)
		r.sendError(msg.From, "fail")
		return
	}
	resp.Body.Close()
	r.sendAck(msg.From, msg.MsgID)
}

// sendError sends an error message to a user via the local hub.
// Used when routing fails (e.g., user not found, forward failed).
func (r *Router) sendError(uid, err string) {
	if r.hub != nil {
		r.hub.SendToUser(uid, shared.WSMessage{Type: "error", Error: err})
	}
}

// sendAck sends an acknowledgment message to a user via the local hub.
// Confirms that a message was successfully forwarded to the target node.
func (r *Router) sendAck(uid, mid string) {
	if r.hub != nil {
		r.hub.SendToUser(uid, shared.WSMessage{Type: "ack", MsgID: mid, To: uid})
	}
}

// DeliverLocal delivers a message to a local user.
// Called by the HTTP handler when another node forwards a message to this node.
// Simply passes the message to the hub for delivery to the recipient's WebSocket.
func (r *Router) DeliverLocal(msg shared.WSMessage) {
	if r.hub != nil {
		r.hub.SendToUser(msg.To, msg)
	}
}

// upgrader configures WebSocket connection upgrade.
// CheckOrigin allows all origins (adjust for production).
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// WSHandler manages WebSocket connections and message handling for clients.
// hub: manages connected clients on this node.
// registry: client for registering user presence with the central registry.
// router: handles message routing (local or cross-node).
type WSHandler struct {
	hub      *Hub
	registry *RegistryClient
	router   *Router
}

// NewWSHandler creates a new WSHandler with the given dependencies.
func NewWSHandler(h *Hub, reg *RegistryClient, rt *Router) *WSHandler {
	return &WSHandler{hub: h, registry: reg, router: rt}
}

// HandleWS upgrades an HTTP request to a WebSocket connection.
// Expects "user" query parameter for user identification.
// Registers the user with the hub and registry, then starts read/write pumps.
func (h *WSHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("user")
	if uid == "" {
		http.Error(w, "user required", 400)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("ws:", err)
		return
	}
	client := h.hub.Register(uid)
	h.registry.SetUserPresence(uid)
	go h.readPump(conn, client, uid)
	go h.writePump(conn, client)
}

// readPump reads messages from the WebSocket connection and processes them.
// Runs in a separate goroutine per connection.
// Handles "chat" messages (routes via router) and "ping" messages (responds with pong).
// On disconnect or error, unregisters user from hub and registry.
func (h *WSHandler) readPump(conn *websocket.Conn, client *Client, uid string) {
	defer func() {
		h.hub.Unregister(uid)
		h.registry.RemoveUserPresence(uid)
		conn.Close()
	}()
	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg shared.WSMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "chat":
			msg.From = uid
			msg.Timestamp = time.Now().UnixMilli()
			h.router.RouteMessage(msg)
		case "ping":
			client.Send <- shared.WSMessage{Type: "pong"}
		}
	}
}

// writePump writes messages from the client's send channel to the WebSocket connection.
// Runs in a separate goroutine per connection.
// Sends periodic ping messages (every 30s) to keep connection alive.
// Exits when send channel is closed or write fails.
func (h *WSHandler) writePump(conn *websocket.Conn, client *Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()
	for {
		select {
		case msg, ok := <-client.Send:
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			data, _ := json.Marshal(msg)
			if conn.WriteMessage(websocket.TextMessage, data) != nil {
				return
			}
		case <-ticker.C:
			if conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		}
	}
}

// HTTPHandler handles internal HTTP endpoints for node-to-node communication.
// router: reference to the router for delivering forwarded messages locally.
type HTTPHandler struct {
	router *Router
}

// NewHTTPHandler creates a new HTTPHandler with the given router.
func NewHTTPHandler(r *Router) *HTTPHandler {
	return &HTTPHandler{router: r}
}

// RegisterRoutes registers the HTTP handler's routes on the given ServeMux.
// /internal/forward: receives forwarded messages from other nodes.
// /health: health check endpoint for container orchestration.
func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/internal/forward", h.handleForward)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// handleForward handles POST /internal/forward requests from other nodes.
// Decodes a ForwardMessage, converts it to a WSMessage, and delivers it locally via the router.
// Returns JSON response indicating delivery status.
func (h *HTTPHandler) handleForward(w http.ResponseWriter, r *http.Request) {
	var msg shared.ForwardMessage
	if json.NewDecoder(r.Body).Decode(&msg) != nil {
		http.Error(w, "bad json", 400)
		return
	}
	h.router.DeliverLocal(shared.WSMessage{
		Type:      "chat",
		From:      msg.FromUser,
		To:        msg.ToUser,
		Content:   msg.Content,
		MsgID:     msg.MsgID,
		Timestamp: msg.Timestamp,
	})
	json.NewEncoder(w).Encode(map[string]bool{"delivered": true})
}

// getEnv retrieves an environment variable or returns a default value.
// k: environment variable name.
// d: default value if variable is not set or empty.
func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// corsMiddleware adds CORS headers to all responses.
// Allows all origins, methods, and common headers (adjust for production).
// Handles OPTIONS preflight requests by returning 204 No Content.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins (adjust as needed for production)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Upgrade, Connection, Sec-WebSocket-Key, Sec-WebSocket-Version, Sec-WebSocket-Extensions")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// main is the entry point for the chat node.
// Reads configuration from environment variables, initializes components, and starts the HTTP server.
// Environment variables:
//   NODE_ID: unique identifier for this node (default: "node-a")
//   NODE_ADDR: internal address for node-to-node communication (default: http://<nodeID>:3000)
//   CLIENT_ADDR: external address for client WebSocket connections (default: http://<nodeID>:3000)
//   REGISTRY_URL: central registry URL (default: http://registry:8080)
//   PORT: HTTP server port (default: "3000")
func main() {
	nodeID := getEnv("NODE_ID", "node-a")
	nodeAddr := getEnv("NODE_ADDR", "http://"+nodeID+":3000")
	clientAddr := getEnv("CLIENT_ADDR", "http://"+nodeID+":3000")
	regURL := getEnv("REGISTRY_URL", "http://registry:8080")
	port := getEnv("PORT", "3000")

	hub := NewHub()
	reg := NewRegistryClient(regURL, nodeID, nodeAddr, clientAddr)
	router := NewRouter(reg)
	router.SetHub(hub)

	ws := NewWSHandler(hub, reg, router)
	httpH := NewHTTPHandler(router)

	reg.RegisterNode()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.HandleWS)
	httpH.RegisterRoutes(mux)

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("Node %s on :%s", nodeID, port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
