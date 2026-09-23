package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"central-registry/shared"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var clients = make(map[string]*Client)
var clientsMu sync.Mutex

type Client struct {
	UserID string
	Conn   *websocket.Conn
	Send   chan shared.WSMessage
}

func main() {
	nodeID := getEnv("NODE_ID", "node-a")
	nodeAddr := getEnv("NODE_ADDR", "http://"+nodeID+":3000")
	clientAddr := getEnv("CLIENT_ADDR", "http://localhost:3001")
	regURL := getEnv("REGISTRY_URL", "http://registry:8080")
	port := getEnv("PORT", "3000")

	registerNode(regURL, nodeID, nodeAddr, clientAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWS)
	mux.HandleFunc("/internal/forward", handleForward)

	handler := corsMiddleware(mux)

	log.Printf("Node %s on :%s", nodeID, port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func registerNode(regURL, nodeID, nodeAddr, clientAddr string) {
	req := map[string]string{
		"node_id":        nodeID,
		"address":        nodeAddr,
		"client_address": clientAddr,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(regURL+"/nodes/register", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to register node: %v", err)
		return
	}
	resp.Body.Close()
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user")
	if userID == "" {
		http.Error(w, "user parameter required", 400)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{UserID: userID, Conn: ws, Send: make(chan shared.WSMessage, 256)}
	clientsMu.Lock()
	clients[userID] = client
	clientsMu.Unlock()

	reportPresence(userID)

	client.Send <- shared.WSMessage{Type: "connected", From: userID}

	go writePump(client)

	for {
		var msg shared.WSMessage
		if err := ws.ReadJSON(&msg); err != nil {
			log.Printf("WebSocket read error for %s: %v", userID, err)
			break
		}

		j, _ := json.Marshal(msg)
		log.Println("Received Message is ", string(j))

		handleMessage(userID, msg)
	}

	clientsMu.Lock()
	delete(clients, userID)
	clientsMu.Unlock()
	reportPresenceDelete(userID)
	ws.Close()
}

func writePump(client *Client) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case msg := <-client.Send:
			if err := client.Conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := client.Conn.WriteJSON(shared.WSMessage{Type: "ping"}); err != nil {
				return
			}
		}
	}
}

func handleMessage(fromUser string, msg shared.WSMessage) {
	switch msg.Type {
	case "chat":
		clientsMu.Lock()
		toClient, local := clients[msg.To]
		clientsMu.Unlock()

		if local {
			toClient.Send <- shared.WSMessage{
				Type:      "chat",
				From:      fromUser,
				Content:   msg.Content,
				MsgID:     msg.MsgID,
				Timestamp: time.Now().Unix(),
			}
			clientsMu.Lock()
			if sender, ok := clients[fromUser]; ok {
				sender.Send <- shared.WSMessage{Type: "ack", MsgID: msg.MsgID}
			}
			clientsMu.Unlock()
			return
		}

		targetAddr, err := lookupUser(msg.To)
		if err != nil {
			clientsMu.Lock()
			if sender, ok := clients[fromUser]; ok {
				sender.Send <- shared.WSMessage{Type: "error", Error: "User not found"}
			}
			clientsMu.Unlock()
			return
		}

		forwardMsg := shared.ForwardMessage{
			ToUser:    msg.To,
			FromUser:  fromUser,
			Content:   msg.Content,
			MsgID:     msg.MsgID,
			Timestamp: time.Now().Unix(),
		}
		body, _ := json.Marshal(forwardMsg)
		resp, err := http.Post(targetAddr+"/internal/forward", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("Forward failed: %v", err)
			return
		}
		resp.Body.Close()

		clientsMu.Lock()
		if sender, ok := clients[fromUser]; ok {
			sender.Send <- shared.WSMessage{Type: "ack", MsgID: msg.MsgID}
		}
		clientsMu.Unlock()
	}
}

func lookupUser(userID string) (string, error) {
	resp, err := http.Get("http://registry:8080/lookup/" + userID)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", err
	}

	var result struct {
		Address string `json:"address"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Address, nil
}

func handleForward(w http.ResponseWriter, r *http.Request) {
	var fm shared.ForwardMessage
	if err := json.NewDecoder(r.Body).Decode(&fm); err != nil {
		http.Error(w, "bad json", 400)
		return
	}

	clientsMu.Lock()
	toClient, ok := clients[fm.ToUser]
	clientsMu.Unlock()

	if ok {
		toClient.Send <- shared.WSMessage{
			Type:      "chat",
			From:      fm.FromUser,
			Content:   fm.Content,
			MsgID:     fm.MsgID,
			Timestamp: fm.Timestamp,
		}
	}
	json.NewEncoder(w).Encode(map[string]bool{"delivered": ok})
}

func reportPresence(userID string) {
	nodeID := getEnv("NODE_ID", "node-a")
	req := map[string]string{"user_id": userID, "node_id": nodeID}
	body, _ := json.Marshal(req)
	resp, err := http.Post("http://registry:8080/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to report presence: %v", err)
		return
	}
	resp.Body.Close()
}

func reportPresenceDelete(userID string) {
	req, _ := http.NewRequest(http.MethodDelete, "http://registry:8080/presence/"+userID, nil)
	http.DefaultClient.Do(req)
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
