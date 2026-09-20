package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"central-registry/shared"
)

// HubInterface defines what the router needs from hub
type HubInterface interface {
	SendToUser(userID string, msg shared.WSMessage) bool
}

type Router struct {
	registry   *RegistryClient
	httpClient *http.Client
	nodeID     string
	nodeAddr   string
	hub        HubInterface
}

func NewRouter(registry *RegistryClient, nodeID, nodeAddr string) *Router {
	return &Router{
		registry:   registry,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		nodeID:     nodeID,
		nodeAddr:   nodeAddr,
	}
}

func (r *Router) RouteMessage(msg shared.WSMessage) {
	if msg.To == "" {
		log.Println("Message missing 'to' field")
		return
	}

	// Try local delivery first
	if r.hub != nil && r.hub.SendToUser(msg.To, msg) {
		// Delivered locally - send ack
		r.hub.SendToUser(msg.From, shared.WSMessage{
			Type:  "ack",
			MsgID: msg.MsgID,
			To:    msg.From,
		})
		return
	}

	// Otherwise, find which node they're on and forward
	nodeInfo, err := r.registry.LookupUser(msg.To)
	if err != nil || nodeInfo.NodeID == "" {
		r.sendError(msg.From, "User not found: "+msg.To)
		return
	}

	r.forwardToNode(nodeInfo.Address, msg)
}

func (r *Router) forwardToNode(targetNodeAddr string, msg shared.WSMessage) {
	forwardMsg := shared.ForwardMessage{
		MsgID:     msg.MsgID,
		FromUser:  msg.From,
		ToUser:    msg.To,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
	}

	body, _ := json.Marshal(forwardMsg)
	url := targetNodeAddr + "/internal/forward"

	resp, err := r.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Println("Forward failed:", err)
		r.sendError(msg.From, "Failed to deliver message")
		return
	}
	defer resp.Body.Close()

	// Send ack to sender
	r.sendAck(msg.From, msg.MsgID)
}

func (r *Router) SetHub(hub HubInterface) {
	r.hub = hub
}

func (r *Router) sendError(userID, errorMsg string) {
	if r.hub != nil {
		r.hub.SendToUser(userID, shared.WSMessage{
			Type:  "error",
			Error: errorMsg,
		})
	}
}

func (r *Router) sendAck(userID, msgID string) {
	if r.hub != nil {
		r.hub.SendToUser(userID, shared.WSMessage{
			Type:  "ack",
			MsgID: msgID,
			To:    userID,
		})
	}
}

// DeliverLocal delivers a message to a local user (called from HTTP handler)
func (r *Router) DeliverLocal(msg shared.WSMessage) {
	if r.hub != nil {
		r.hub.SendToUser(msg.To, msg)
	}
}