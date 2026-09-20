package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"central-registry/shared"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WSHandler struct {
	hub           *Hub
	registry      *RegistryClient
	router        *Router
	nodeID        string
}

func NewWSHandler(hub *Hub, registry *RegistryClient, router *Router, nodeID string) *WSHandler {
	return &WSHandler{
		hub:      hub,
		registry: registry,
		router:   router,
		nodeID:   nodeID,
	}
}

func (h *WSHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user")
	if userID == "" {
		http.Error(w, "user query param required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WS upgrade error:", err)
		return
	}

	client := h.hub.Register(userID)

	// Tell registry this user is now on this node
	if err := h.registry.SetUserPresence(userID); err != nil {
		log.Println("Failed to register presence:", err)
	}

	// Start read and write pumps
	go h.readPump(conn, client, userID)
	go h.writePump(conn, client)
}

func (h *WSHandler) readPump(conn *websocket.Conn, client *Client, userID string) {
	defer func() {
		h.hub.Unregister(userID)
		h.registry.RemoveUserPresence(userID)
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
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "chat":
			msg.From = userID
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
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}