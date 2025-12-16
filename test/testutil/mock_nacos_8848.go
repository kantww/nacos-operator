package testutil

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// MockNacosServer8848 is a mock Nacos server that listens on port 8848
type MockNacosServer8848 struct {
	listener        net.Listener
	server          *http.Server
	servers         []NacosServerInfo
	running         bool
	RequireIdentity bool
	ExpectedKey     string
	ExpectedValue   string
	mu              sync.RWMutex
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.mu.RLock()
		requireIdentity := mock.RequireIdentity
		expectedKey := mock.ExpectedKey
		expectedValue := mock.ExpectedValue
		servers := mock.servers
		mock.mu.RUnlock()

		if requireIdentity {
			key := r.Header.Get(expectedKey)
			if key != expectedValue {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    403,
					"message": "Forbidden",
				})
				return
			}
		}

		response := NacosServersResponse{
			Code:    200,
			Message: "success",
			Data:    servers,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	httpServer := &http.Server{Handler: handler}
	mock.server = httpServer

	// Start HTTP server
	go func() {
		httpServer.Serve(listener)
	}()

	// Wait for server to be ready
	time.Sleep(10 * time.Millisecond)

	return mock, nil
}

func (m *MockNacosServer8848) Close() {
	if m.running {
		m.running = false
		if m.server != nil {
			m.server.Close()
		}
		if m.listener != nil {
			m.listener.Close()
		}
	}
}

func (m *MockNacosServer8848) GetPodIP() string {
	return "127.0.0.1"
}

// UpdateServers updates the server list dynamically
func (m *MockNacosServer8848) UpdateServers(servers []NacosServerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = servers
}

// WithIdentityCheck enables identity header checking
func (m *MockNacosServer8848) WithIdentityCheck(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequireIdentity = true
	m.ExpectedKey = key
	m.ExpectedValue = value
}
