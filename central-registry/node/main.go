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

type Client struct {
	UserID string
	Send   chan shared.WSMessage
}

type Hub struct {
	clients map[string]*Client
	mu      sync.Mutex
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

func (h *Hub) Register(userID string) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &Client{UserID: userID, Send: make(chan shared.WSMessage, 256)}
	h.clients[userID] = c
	return c
}

func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[userID]; ok {
		close(c.Send)
		delete(h.clients, userID)
	}
}

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

type RegistryClient struct {
	registryURL string
	nodeID      string
	nodeAddr    string
	clientAddr  string
	httpClient  *http.Client
}

func NewRegistryClient(u, id, a, ca string) *RegistryClient {
	return &RegistryClient{registryURL: u, nodeID: id, nodeAddr: a, clientAddr: ca, httpClient: &http.Client{}}
}

func (c *RegistryClient) RegisterNode() error {
	body, _ := json.Marshal(map[string]string{"node_id": c.nodeID, "address": c.nodeAddr, "client_address": c.clientAddr})
	resp, err := c.httpClient.Post(c.registryURL+"/nodes/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *RegistryClient) SetUserPresence(uid string) error {
	body, _ := json.Marshal(map[string]string{"user_id": uid, "node_id": c.nodeID})
	resp, err := c.httpClient.Post(c.registryURL+"/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

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

type Router struct {
	registry   *RegistryClient
	httpClient *http.Client
	hub        *Hub
}

func NewRouter(r *RegistryClient) *Router {
	return &Router{registry: r, httpClient: &http.Client{Timeout: 5 * time.Second}}
}

func (r *Router) SetHub(h *Hub) { r.hub = h }

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

func (r *Router) sendError(uid, err string) {
	if r.hub != nil {
		r.hub.SendToUser(uid, shared.WSMessage{Type: "error", Error: err})
	}
}

func (r *Router) sendAck(uid, mid string) {
	if r.hub != nil {
		r.hub.SendToUser(uid, shared.WSMessage{Type: "ack", MsgID: mid, To: uid})
	}
}

func (r *Router) DeliverLocal(msg shared.WSMessage) {
	if r.hub != nil {
		r.hub.SendToUser(msg.To, msg)
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type WSHandler struct {
	hub      *Hub
	registry *RegistryClient
	router   *Router
}

func NewWSHandler(h *Hub, reg *RegistryClient, rt *Router) *WSHandler {
	return &WSHandler{hub: h, registry: reg, router: rt}
}

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

type HTTPHandler struct {
	router *Router
}

func NewHTTPHandler(r *Router) *HTTPHandler {
	return &HTTPHandler{router: r}
}

func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/internal/forward", h.handleForward)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

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

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// corsMiddleware adds CORS headers to all responses
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
