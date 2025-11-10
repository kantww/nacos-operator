package testutil

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// MockNacosServer8848 is a mock Nacos server that listens on port 8848
type MockNacosServer8848 struct {
	listener net.Listener
	servers  []NacosServerInfo
	running  bool
}

// NewMockNacosServer8848 creates a mock server on port 8848
func NewMockNacosServer8848(servers []NacosServerInfo) (*MockNacosServer8848, error) {
	// Try to listen on 127.0.0.1:8848
	listener, err := net.Listen("tcp", "127.0.0.1:8848")
	if err != nil {
		// Port might be in use, try a different approach
		return nil, fmt.Errorf("failed to listen on port 8848: %v", err)
	}

	mock := &MockNacosServer8848{
		listener: listener,
		servers:  servers,
		running:  true,
	}

	// Start HTTP server
	go func() {
		http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := NacosServersResponse{
				Code:    200,
				Message: "success",
				Data:    mock.servers,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
	}()

	return mock, nil
}

func (m *MockNacosServer8848) Close() {
	if m.running {
		m.listener.Close()
		m.running = false
	}
}

func (m *MockNacosServer8848) GetPodIP() string {
	return "127.0.0.1"
}

// UpdateServers updates the server list dynamically
func (m *MockNacosServer8848) UpdateServers(servers []NacosServerInfo) {
	m.servers = servers
}
