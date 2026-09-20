package main

import (
	"central-registry/shared"
	"encoding/json"
	"log"
	"net/http"
	"sync"
)

// Store manages the in-memory mapping of user_id -> NodeInfo
type Store struct {
	nodes map[string]shared.NodeInfo
	users map[string]string
	mu    sync.RWMutex
}

func NewStore() *Store {
	return &Store{
		nodes: make(map[string]shared.NodeInfo),
		users: make(map[string]string),
	}
}

func (s *Store) RegisterNode(nodeID, address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[nodeID] = shared.NodeInfo{NodeID: nodeID, Address: address}
}

func (s *Store) SetUserPresence(userID, nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[nodeID]; !ok {
		return false
	}
	s.users[userID] = nodeID
	return true
}

func (s *Store) RemoveUserPresence(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, userID)
}

func (s *Store) LookupUser(userID string) (shared.NodeInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodeID, ok := s.users[userID]
	if !ok {
		return shared.NodeInfo{}, false
	}
	nodeInfo, ok := s.nodes[nodeID]
	return nodeInfo, ok
}

type Server struct {
	store *Store
}

func NewServer(store *Store) *Server {
	return &Server{store: store}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/nodes/register", s.handleRegisterNode)
	mux.HandleFunc("/presence", s.handlePresence)
	mux.HandleFunc("/lookup/", s.handleLookup)
	mux.HandleFunc("/health", s.handleHealth)
}

func (s *Server) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NodeID  string `json:"node_id"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s.store.RegisterNode(req.NodeID, req.Address)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		NodeID string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	ok := s.store.SetUserPresence(req.UserID, req.NodeID)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Path[len("/lookup/"):]
	nodeInfo, ok := s.store.LookupUser(userID)
	if !ok {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(nodeInfo)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	store := NewStore()
	server := NewServer(store)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	log.Println("Registry server starting on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
