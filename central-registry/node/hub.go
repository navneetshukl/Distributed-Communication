package main

import (
	"sync"

	"central-registry/shared"
)

type Client struct {
	UserID string
	Send   chan shared.WSMessage
}

type Hub struct {
	clients map[string]*Client // userID -> Client
	mu      sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*Client),
	}
}

func (h *Hub) Register(userID string) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := &Client{
		UserID: userID,
		Send:   make(chan shared.WSMessage, 256),
	}
	h.clients[userID] = client
	return client
}

func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[userID]; ok {
		close(client.Send)
		delete(h.clients, userID)
	}
}

func (h *Hub) SendToUser(userID string, msg shared.WSMessage) bool {
	h.mu.Lock()
	client, ok := h.clients[userID]
	h.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case client.Send <- msg:
		return true
	default:
		return false
	}
}

func (h *Hub) GetConnectedUsers() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	users := make([]string, 0, len(h.clients))
	for userID := range h.clients {
		users = append(users, userID)
	}
	return users
}