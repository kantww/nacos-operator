package nacosClient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"nacos.io/nacos-operator/test/testutil"
)

func TestNacosClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Nacos Client Suite")
}

var _ = Describe("NacosClient", func() {
	var (
		client *NacosClient
	)

	BeforeEach(func() {
		client = &NacosClient{}
	})

	extractIPPort := func(serverURL string) string {
		return strings.TrimPrefix(serverURL, "http://")
	}

	Describe("GetClusterNodes", func() {
		Context("with a healthy cluster", func() {
			It("should return cluster nodes successfully", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Code).To(Equal(200))
				Expect(len(servers.Data)).To(Equal(3))
				Expect(servers.Data[0].State).To(Equal("UP"))
				Expect(servers.Data[0].ExtendInfo.Version).To(Equal("2.1.0"))
			})
		})

		Context("with identity headers", func() {
			It("should send identity headers when provided", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				mockServer.WithIdentityCheck("X-Identity-Key", "secret-value")
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort, "X-Identity-Key", "secret-value")
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Code).To(Equal(200))
			})

			It("should fail when identity headers are incorrect", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				mockServer.WithIdentityCheck("X-Identity-Key", "secret-value")
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort, "X-Identity-Key", "wrong-value")
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Code).To(Equal(403))
			})

			It("should work without identity headers when not required", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Code).To(Equal(200))
			})
		})

		Context("with cluster having down nodes", func() {
			It("should return nodes with DOWN state", func() {
				mockServers := testutil.CreateMockClusterServersWithDownNode(3, 1, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Code).To(Equal(200))
				Expect(len(servers.Data)).To(Equal(3))

				downCount := 0
				for _, server := range servers.Data {
					if server.State == "DOWN" {
						downCount++
					}
				}
				Expect(downCount).To(Equal(1))
			})
		})

		Context("with network errors", func() {
			It("should return error when server is unreachable", func() {
				_, err := client.GetClusterNodes("192.0.2.1")
				Expect(err).To(HaveOccurred())
			})

			It("should return error on invalid response", func() {
				mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("invalid json"))
				}))
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.URL)

				_, err := client.GetClusterNodes(ipPort)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with different Nacos versions", func() {
			It("should work with Nacos 2.1.0", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Data[0].ExtendInfo.Version).To(Equal("2.1.0"))
			})

			It("should work with Nacos 2.2.0", func() {
				mockServers := testutil.CreateMockClusterServers(3, 0, "2.2.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())
				Expect(servers.Data[0].ExtendInfo.Version).To(Equal("2.2.0"))
			})
		})

		Context("with Raft leader information", func() {
			It("should parse Raft leader correctly", func() {
				mockServers := testutil.CreateMockClusterServers(3, 1, "2.1.0")
				mockServer := testutil.NewMockNacosServer(mockServers)
				defer mockServer.Close()

				ipPort := extractIPPort(mockServer.Server.URL)

				servers, err := client.GetClusterNodes(ipPort)
				Expect(err).NotTo(HaveOccurred())

				leader := servers.Data[0].ExtendInfo.RaftMetaData.MetaDataMap.NamingPersistentServiceV2.Leader
				for _, server := range servers.Data {
					Expect(server.ExtendInfo.RaftMetaData.MetaDataMap.NamingPersistentServiceV2.Leader).To(Equal(leader))
				}
			})
		})
	})
})
