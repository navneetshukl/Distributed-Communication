package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	nodeID := getEnv("NODE_ID", "node-a")
	nodeAddr := getEnv("NODE_ADDR", "http://"+nodeID+":3000")
	registryURL := getEnv("REGISTRY_URL", "http://registry:8080")
	port := getEnv("PORT", "3000")

	hub := NewHub()
	registryClient := NewRegistryClient(registryURL, nodeID, nodeAddr)
	router := NewRouter(registryClient, nodeID, nodeAddr)
	router.SetHub(hub)

	wsHandler := NewWSHandler(hub, registryClient, router, nodeID)
	httpHandler := NewHTTPHandler(router)

	// Register this node with registry
	if err := registryClient.RegisterNode(); err != nil {
		log.Println("Warning: failed to register with registry:", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler.HandleWS)
	httpHandler.RegisterRoutes(mux)

	log.Printf("Node %s starting on :%s", nodeID, port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}