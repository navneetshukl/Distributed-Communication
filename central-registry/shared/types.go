package shared

// NodeInfo represents a chat node in the registry
type NodeInfo struct {
	NodeID  string `json:"node_id"`
	Address string `json:"address"` // e.g., "http://node-a:3000"
}

// ForwardMessage - Node-to-node message delivery
type ForwardMessage struct {
	MsgID     string `json:"msg_id"`
	FromUser  string `json:"from_user"`
	ToUser    string `json:"to_user"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// WSMessage - Client ↔ Node WebSocket message
type WSMessage struct {
	Type      string `json:"type"`        // "chat", "ack", "error", "ping", "pong"
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Content   string `json:"content,omitempty"`
	MsgID     string `json:"msg_id,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Error     string `json:"error,omitempty"`
}