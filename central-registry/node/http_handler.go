package main

import (
	"central-registry/shared"
	"encoding/json"
	"net/http"
)

type HTTPHandler struct {
	router *Router
}

func NewHTTPHandler(router *Router) *HTTPHandler {
	return &HTTPHandler{router: router}
}

func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/internal/forward", h.handleForward)
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *HTTPHandler) handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg shared.ForwardMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Convert to WSMessage and deliver locally
	wsMsg := shared.WSMessage{
		Type:      "chat",
		From:      msg.FromUser,
		To:        msg.ToUser,
		Content:   msg.Content,
		MsgID:     msg.MsgID,
		Timestamp: msg.Timestamp,
	}

	h.router.DeliverLocal(wsMsg)
	json.NewEncoder(w).Encode(map[string]bool{"delivered": true})
}

func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
