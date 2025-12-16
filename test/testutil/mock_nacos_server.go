package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

type NacosServerInfo struct {
	IP         string                 `json:"ip"`
	Port       int                    `json:"port"`
	State      string                 `json:"state"`
	ExtendInfo NacosServerExtendInfo  `json:"extendInfo"`
	Address    string                 `json:"address"`
	FailAccessCnt int                 `json:"failAccessCnt"`
	Abilities  NacosServerAbilities   `json:"abilities"`
}

type NacosServerExtendInfo struct {
	LastRefreshTime int64               `json:"lastRefreshTime"`
	RaftMetaData    NacosRaftMetaData   `json:"raftMetaData"`
	RaftPort        string              `json:"raftPort"`
	Version         string              `json:"version"`
}

type NacosRaftMetaData struct {
	MetaDataMap NacosRaftMetaDataMap `json:"metaDataMap"`
}

type NacosRaftMetaDataMap struct {
	NamingPersistentServiceV2 NacosRaftGroup `json:"naming_persistent_service_v2"`
}

type NacosRaftGroup struct {
	Leader string   `json:"leader"`
	Raftgroup []string `json:"raftGroupMember"`
}

type NacosServerAbilities struct {
	RemoteAbility  NacosAbility `json:"remoteAbility"`
	ConfigAbility  NacosAbility `json:"configAbility"`
	NamingAbility  NacosAbility `json:"namingAbility"`
}

type NacosAbility struct {
	SupportRemoteConnection bool `json:"supportRemoteConnection"`
}

type NacosServersResponse struct {
	Code int                `json:"code"`
	Message string           `json:"message"`
	Data []NacosServerInfo  `json:"data"`
}

type MockNacosServer struct {
	Server         *httptest.Server
	Servers        []NacosServerInfo
	RequireIdentity bool
	ExpectedKey    string
	ExpectedValue  string
}

func NewMockNacosServer(servers []NacosServerInfo) *MockNacosServer {
	mock := &MockNacosServer{
		Servers: servers,
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mock.RequireIdentity {
			key := r.Header.Get(mock.ExpectedKey)
			if key != mock.ExpectedValue {
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
			Data:    mock.Servers,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	return mock
}

func (m *MockNacosServer) Close() {
	if m.Server != nil {
		m.Server.Close()
	}
}

func (m *MockNacosServer) WithIdentityCheck(key, value string) *MockNacosServer {
	m.RequireIdentity = true
	m.ExpectedKey = key
	m.ExpectedValue = value
	return m
}

// UpdateServers updates the server list dynamically
func (m *MockNacosServer) UpdateServers(servers []NacosServerInfo) {
	m.Servers = servers
}

func CreateMockClusterServers(replicas int, leaderIndex int, version string) []NacosServerInfo {
	return CreateMockClusterServersWithName(replicas, leaderIndex, version, "nacos")
}

func CreateMockClusterServersWithName(replicas int, leaderIndex int, version string, stsName string) []NacosServerInfo {
	servers := make([]NacosServerInfo, replicas)

	for i := 0; i < replicas; i++ {
		leader := fmt.Sprintf("%s-%d:7848", stsName, leaderIndex)

		servers[i] = NacosServerInfo{
			IP:    fmt.Sprintf("10.244.0.%d", i+1),
			Port:  8848,
			State: "UP",
			ExtendInfo: NacosServerExtendInfo{
				LastRefreshTime: 1234567890,
				RaftPort:        "7848",
				Version:         version,
				RaftMetaData: NacosRaftMetaData{
					MetaDataMap: NacosRaftMetaDataMap{
						NamingPersistentServiceV2: NacosRaftGroup{
							Leader:    leader,
							Raftgroup: []string{"nacos"},
						},
					},
				},
			},
			Address:       fmt.Sprintf("10.244.0.%d:8848", i+1),
			FailAccessCnt: 0,
			Abilities: NacosServerAbilities{
				RemoteAbility: NacosAbility{
					SupportRemoteConnection: true,
				},
				ConfigAbility: NacosAbility{
					SupportRemoteConnection: true,
				},
				NamingAbility: NacosAbility{
					SupportRemoteConnection: true,
				},
			},
		}
	}

	return servers
}

func CreateMockClusterServersWithDownNode(replicas int, downIndex int, leaderIndex int, version string) []NacosServerInfo {
	servers := CreateMockClusterServers(replicas, leaderIndex, version)
	servers[downIndex].State = "DOWN"
	return servers
}
