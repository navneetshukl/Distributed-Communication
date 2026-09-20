package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
)

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins (adjust as needed for production)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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

// Simple in-memory store
var (
	// user_id -> node_id (which node the user is connected to)
	userNodes = make(map[string]string)
	// node_id -> internal address (for node-to-node communication)
	nodes = make(map[string]string)
	// node_id -> client address (for browser WebSocket connections)
	clientAddrs = make(map[string]string)
	nodeList  = make([]string, 0)       // node IDs in round-robin order
	rrIndex   = 0                       // round-robin cursor
	mu        sync.RWMutex
)

func main() {
	// Register node
	http.HandleFunc("/nodes/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			NodeID      string `json:"node_id"`
			Address     string `json:"address"`       // internal Docker address for node-to-node
			ClientAddr  string `json:"client_address"` // external address for browser clients
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		if _, exists := nodes[req.NodeID]; !exists {
			nodeList = append(nodeList, req.NodeID)
		}
		nodes[req.NodeID] = req.Address
		clientAddrs[req.NodeID] = req.ClientAddr
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	// User connects to a node (called by node when WebSocket connects)
	http.HandleFunc("/presence", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID string `json:"user_id"`
			NodeID string `json:"node_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		if _, ok := nodes[req.NodeID]; ok {
			userNodes[req.UserID] = req.NodeID
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else {
			http.Error(w, "node not found", 404)
		}
		mu.Unlock()
	})

	// Lookup which node a user is on (returns full NodeInfo for node-to-node forwarding)
	http.HandleFunc("/lookup/", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Path[len("/lookup/"):]
		mu.RLock()
		nodeID, ok := userNodes[userID]
		mu.RUnlock()
		if ok {
			mu.RLock()
			internalAddr := nodes[nodeID]
			clientAddr := clientAddrs[nodeID]
			mu.RUnlock()
			json.NewEncoder(w).Encode(map[string]string{
				"node_id":        nodeID,
				"address":        internalAddr,
				"client_address": clientAddr,
			})
		} else {
			http.Error(w, "user not found", 404)
		}
	})

	// List all registered nodes
	http.HandleFunc("/nodes/list", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		list := make([]string, len(nodeList))
		copy(list, nodeList)
		mu.RUnlock()
		json.NewEncoder(w).Encode(map[string]interface{}{"nodes": list})
	})

	// List all online users (for client user discovery)
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		users := make(map[string]string, len(userNodes))
		for uid, nid := range userNodes {
			users[uid] = nid
		}
		mu.RUnlock()
		json.NewEncoder(w).Encode(map[string]interface{}{"users": users})
	})

	// Remove user presence (called by node when WebSocket disconnects)
	http.HandleFunc("/presence/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", 405)
			return
		}
		userID := r.URL.Path[len("/presence/"):]
		mu.Lock()
		delete(userNodes, userID)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	// Round-robin node assignment for a new user (called by client before WebSocket connect)
	http.HandleFunc("/assign", func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user_id")
		mu.Lock()
		defer mu.Unlock()
		if len(nodeList) == 0 {
			http.Error(w, "no nodes available", 503)
			return
		}
		// If user already assigned, return same node
		if nodeID, ok := userNodes[userID]; ok {
			internalAddr := nodes[nodeID]
			clientAddr := clientAddrs[nodeID]
			json.NewEncoder(w).Encode(map[string]string{"address": clientAddr, "node_id": nodeID, "internal_address": internalAddr})
			return
		}
		nodeID := nodeList[rrIndex%len(nodeList)]
		rrIndex++
		userNodes[userID] = nodeID
		clientAddr := clientAddrs[nodeID]
		internalAddr := nodes[nodeID]
		json.NewEncoder(w).Encode(map[string]string{"address": clientAddr, "node_id": nodeID, "internal_address": internalAddr})
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Wrap with CORS middleware
	handler := corsMiddleware(http.DefaultServeMux)

	log.Println("Registry starting on :8080")
	http.ListenAndServe(":8080", handler)
}
