package main

import (
	"bytes"
	"encoding/json"
	"net/http"

	"central-registry/shared"
)

type RegistryClient struct {
	registryURL string
	nodeID      string
	nodeAddr    string
	httpClient  *http.Client
}

func NewRegistryClient(registryURL, nodeID, nodeAddr string) *RegistryClient {
	return &RegistryClient{
		registryURL: registryURL,
		nodeID:      nodeID,
		nodeAddr:    nodeAddr,
		httpClient:  &http.Client{},
	}
}

// RegisterNode registers this node with the registry
func (c *RegistryClient) RegisterNode() error {
	reqBody := map[string]string{
		"node_id":  c.nodeID,
		"address":  c.nodeAddr,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := c.httpClient.Post(c.registryURL+"/nodes/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// SetUserPresence tells registry that a user is connected to this node
func (c *RegistryClient) SetUserPresence(userID string) error {
	reqBody := map[string]string{
		"user_id": userID,
		"node_id": c.nodeID,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := c.httpClient.Post(c.registryURL+"/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// RemoveUserPresence tells registry that a user disconnected
func (c *RegistryClient) RemoveUserPresence(userID string) error {
	reqBody := map[string]string{
		"user_id": userID,
		"node_id": c.nodeID,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest(http.MethodDelete, c.registryURL+"/presence/"+userID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// LookupUser asks registry which node a user is on
func (c *RegistryClient) LookupUser(userID string) (shared.NodeInfo, error) {
	resp, err := c.httpClient.Get(c.registryURL + "/lookup/" + userID)
	if err != nil {
		return shared.NodeInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return shared.NodeInfo{}, nil
	}

	var nodeInfo shared.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
		return shared.NodeInfo{}, err
	}
	return nodeInfo, nil
}