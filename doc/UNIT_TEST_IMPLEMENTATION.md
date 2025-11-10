# Nacos Operator 单元测试实现分析

## 概述

本文档详细分析 nacos-operator 的全流程单元测试实现方式，包括 Mock 机制、测试工具类的作用，以及为什么需要双客户端架构。

## 测试目录结构

```
test/
├── testcase/           # 测试用例
│   ├── nacos_create_test.go    # 创建测试（standalone/cluster）
│   ├── nacos_scale_test.go     # 扩缩容测试
│   ├── nacos_delete_test.go    # 删除测试
│   └── util.go                 # 测试环境初始化
└── testutil/           # 测试工具和 Mock 实现
    ├── fixtures.go             # 测试数据构造器
    ├── k8s_helpers.go          # K8s 资源辅助函数
    ├── k8s_simulator.go        # K8s 行为模拟器
    ├── mock_nacos_server.go    # Mock Nacos Server（随机端口）
    ├── mock_nacos_8848.go      # Mock Nacos Server（固定8848端口）
    └── client_bridge.go        # 双客户端数据同步桥接
```

## 一、Mock 实现分析

### 1.1 Mock Kubernetes 集群

单元测试 Mock 了两个 Kubernetes 客户端：

#### 1.1.1 Controller-Runtime Fake Client

**文件**: [test/testcase/util.go:41](test/testcase/util.go#L41)

```go
fakeClient := fake.NewClientBuilder().
    WithScheme(testScheme).
    Build()
```

**作用**:
- 供 Controller 的 Reconcile 逻辑使用
- 实现 `sigs.k8s.io/controller-runtime/pkg/client.Client` 接口
- 用于 CR 的 Get/Update/Status 操作

#### 1.1.2 Kubernetes Clientset Fake

**文件**: [test/testcase/util.go:56](test/testcase/util.go#L56)

```go
fakeKubeClient := kubefake.NewSimpleClientset()
```

**作用**:
- 供 OperatorClient 内部的 k8sService 使用
- 实现 `k8s.io/client-go/kubernetes.Interface` 接口
- 用于创建 StatefulSet、Service、ConfigMap 等资源

**为什么需要两个 K8s 客户端？**

原因在于 nacos-operator 的架构设计：

1. **Controller 层**使用 controller-runtime client 进行 CR 操作
2. **OperatorClient 层**内部的 k8sService 使用 kubernetes.Interface 创建资源

**文件**: [pkg/service/operator/operaror.go:28-29](pkg/service/operator/operaror.go#L28-L29)

```go
func NewOperatorClient(logger log.Logger, clientset kubernetes.Interface, ...) {
    service := k8s.NewK8sService(clientset, logger)
    ...
}
```

这导致测试中需要维护两个独立的 fake client，并通过 **ClientBridge** 同步数据。

### 1.2 Mock Nacos Server

单元测试 Mock 了两个 Nacos Server：

#### 1.2.1 MockNacosServer（随机端口）

**文件**: [test/testutil/mock_nacos_server.go:56-93](test/testutil/mock_nacos_server.go#L56-L93)

```go
type MockNacosServer struct {
    Server         *httptest.Server
    Servers        []NacosServerInfo
    RequireIdentity bool
    ExpectedKey    string
    ExpectedValue  string
}
```

**特点**:
- 使用 `httptest.NewServer()` 创建，监听随机端口
- 返回 Mock 的集群节点信息
- 支持身份验证（IdentityKey/IdentityValue）
- 可动态更新节点列表（`UpdateServers()`）

**使用场景**:
- 测试中不使用，仅作为备用
- 可用于需要不同端口的测试场景

#### 1.2.2 MockNacosServer8848（固定端口）

**文件**: [test/testutil/mock_nacos_8848.go:10-46](test/testutil/mock_nacos_8848.go#L10-L46)

```go
type MockNacosServer8848 struct {
    listener net.Listener
    servers  []NacosServerInfo
    running  bool
}
```

**特点**:
- 监听固定端口 `127.0.0.1:8848`
- 使用 `net.Listen()` 直接监听
- 返回 Mock 的集群节点信息
- 可动态更新节点列表

**使用场景**:
- 主要用于测试中的健康检查
- 因为测试中 Pod IP 设置为 `127.0.0.1`，Operator 会访问 `http://127.0.0.1:8848/nacos/v1/core/cluster/nodes`

**为什么需要两个 Nacos Server？**

1. **MockNacosServer8848（固定8848端口）**:
   - 测试中模拟的 Pod IP 都是 `127.0.0.1`
   - Operator 在 CheckNacos 阶段会访问 `http://{pod-ip}:8848/nacos/v1/core/cluster/nodes`
   - 因此需要在本地 8848 端口启动 Mock Server 响应健康检查

2. **MockNacosServer（随机端口）**:
   - 作为备用，当 8848 端口被占用时使用
   - 可用于需要多个不同端口的测试场景
   - 当前测试中主要使用 MockNacosServer8848

**实际测试流程**:

```
测试设置 Pod IP = 127.0.0.1
    ↓
Operator 调用 CheckNacos
    ↓
访问 http://127.0.0.1:8848/nacos/v1/core/cluster/nodes
    ↓
MockNacosServer8848 响应集群节点信息
    ↓
Operator 验证节点数量、状态、Leader
```

### 1.3 Mock 数据构造

**文件**: [test/testutil/mock_nacos_server.go:113-157](test/testutil/mock_nacos_server.go#L113-L157)

`CreateMockClusterServersWithName()` 函数构造符合 Nacos API 规范的集群节点数据：

```go
type NacosServerInfo struct {
    IP         string                 // Pod IP
    Port       int                    // 8848
    State      string                 // "UP"
    ExtendInfo NacosServerExtendInfo  // 包含 Raft 元数据
    Address    string                 // IP:Port
    Abilities  NacosServerAbilities   // 能力信息
}
```

**关键字段**:
- `State`: 必须为 "UP" 才能通过健康检查
- `ExtendInfo.RaftMetaData.MetaDataMap.naming_persistent_service_v2.Leader`: 所有节点必须返回相同的 Leader
- 数组长度必须等于 CR 中的 `replicas`

## 二、testutil 目录文件作用

### 2.1 fixtures.go

**作用**: 提供测试数据构造器，快速创建各种类型的 Nacos CR 和 K8s 资源

**主要函数**:

| 函数 | 作用 |
|------|------|
| `NewNacosStandalone()` | 创建 standalone 模式的 Nacos CR |
| `NewNacosCluster()` | 创建 cluster 模式的 Nacos CR |
| `NewNacosWithMySQL()` | 创建使用 MySQL 数据库的 Nacos CR |
| `NewNacosWithPostgreSQL()` | 创建使用 PostgreSQL 数据库的 Nacos CR |
| `NewNacosWithAdminSecret()` | 创建配置管理员凭据的 Nacos CR |
| `NewSecret()` | 创建 Secret 资源 |
| `NewPGCredentialsSecret()` | 创建 PostgreSQL 凭据 Secret |
| `NewAdminCredentialsSecret()` | 创建管理员凭据 Secret |
| `NewReadyPod()` | 创建 Ready 状态的 Pod |

**示例**:

```go
// 创建一个 standalone Nacos CR
nacos := testutil.NewNacosStandalone("test-standalone", "default")
nacos.UID = "test-uid-standalone"
fakeClient.Create(ctx, nacos)
```

### 2.2 k8s_helpers.go

**作用**: 提供 K8s 资源的辅助构造函数

**主要函数**:

| 函数 | 作用 |
|------|------|
| `NewStatefulSet()` | 创建 StatefulSet 资源 |
| `NewConfigMap()` | 创建 ConfigMap 资源 |
| `NewService()` | 创建 Service 资源 |
| `NewPodList()` | 批量创建 Pod 列表 |

**特点**:
- 创建的资源已设置好 Status（如 StatefulSet.Status.ReadyReplicas）
- 简化测试中的资源准备工作

### 2.3 k8s_simulator.go

**作用**: 模拟 Kubernetes 的异步行为，是测试的核心组件

**主要功能**:

#### 2.3.1 模拟 Pod 创建

**文件**: [test/testutil/k8s_simulator.go:28-100](test/testutil/k8s_simulator.go#L28-L100)

```go
func (s *K8sSimulator) SimulateStatefulSetPodsWithIP(ctx, namespace, stsName, customIP)
```

**模拟行为**:
1. 读取 StatefulSet 的 replicas
2. 为每个副本创建对应的 Pod（如 `nacos-0`, `nacos-1`, `nacos-2`）
3. 设置 Pod 状态为 Running
4. 设置 Pod IP（测试中通常为 `127.0.0.1`）
5. 设置 Pod Conditions（PodScheduled, PodReady, PodInitialized, ContainersReady）
6. 同时更新 controller-runtime client 和 kubernetes clientset

**为什么需要这个模拟器？**

真实 Kubernetes 中，创建 StatefulSet 后：
1. K8s Scheduler 会异步调度 Pod
2. Kubelet 会拉取镜像、启动容器
3. Pod 逐渐变为 Running 状态

但在 fake client 中，这些都不会自动发生，需要手动模拟。

#### 2.3.2 更新 StatefulSet 状态

**文件**: [test/testutil/k8s_simulator.go:102-114](test/testutil/k8s_simulator.go#L102-L114)

```go
func (s *K8sSimulator) UpdateStatefulSetStatus(ctx, namespace, stsName, readyReplicas)
```

**作用**: 更新 StatefulSet 的 Status 字段，模拟 K8s 控制器的行为

### 2.4 mock_nacos_server.go

**作用**: 提供 Mock Nacos Server 实现（随机端口）

**主要组件**:

| 组件 | 作用 |
|------|------|
| `MockNacosServer` | HTTP 测试服务器，响应 Nacos API |
| `NacosServerInfo` | Nacos 节点信息结构体 |
| `CreateMockClusterServers()` | 创建 Mock 集群节点数据 |
| `CreateMockClusterServersWithDownNode()` | 创建包含宕机节点的数据 |

**API 实现**:
- 路径: 所有请求
- 返回: `NacosServersResponse` JSON
- 支持身份验证（可选）

### 2.5 mock_nacos_8848.go

**作用**: 提供固定 8848 端口的 Mock Nacos Server

**特点**:
- 监听 `127.0.0.1:8848`
- 与 mock_nacos_server.go 功能类似，但端口固定
- 测试中主要使用这个 Mock Server

### 2.6 client_bridge.go

**作用**: 同步两个 K8s 客户端之间的数据，解决双客户端架构问题

**文件**: [test/testutil/client_bridge.go:12-74](test/testutil/client_bridge.go#L12-L74)

```go
type ClientBridge struct {
    kubeClient kubernetes.Interface      // kubernetes clientset
    ctrlClient client.Client              // controller-runtime client
}
```

**核心方法**:

```go
func (b *ClientBridge) SyncFromKubeToCtrl(ctx, namespace) error
```

**同步的资源**:
1. ConfigMap
2. StatefulSet（包括 Spec 和 Status）
3. Service

**为什么需要 ClientBridge？**

测试流程中的数据流向：

```
Reconcile() 调用 OperatorClient.MakeEnsure()
    ↓
OperatorClient 使用 kubernetes.Interface 创建资源
    ↓
资源只存在于 fakeKubeClient 中
    ↓
Reconcile() 后续使用 controller-runtime client 查询资源
    ↓
找不到资源！❌
    ↓
需要 ClientBridge.SyncFromKubeToCtrl() 同步数据
    ↓
两个客户端数据一致 ✓
```

**测试中的使用**:

```go
// 每次 Reconcile 后同步数据
reconciler.Reconcile(ctx, req)
bridge.SyncFromKubeToCtrl(ctx, "default")
```

## 三、为什么需要双客户端和双 Mock Server

### 3.1 为什么需要两个 K8s Fake Client？

**根本原因**: nacos-operator 的分层架构

```
Controller (使用 controller-runtime client)
    ↓
OperatorClient
    ↓
k8sService (使用 kubernetes.Interface)
    ↓
K8s API
```

**生产环境**:
- 两个客户端都连接真实的 K8s API Server
- 数据自然一致

**测试环境**:
- 两个 fake client 是独立的内存存储
- 需要 ClientBridge 手动同步

**解决方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| 重构代码统一使用一个客户端 | 测试简单 | 需要大量重构，风险高 |
| 使用 ClientBridge 同步 | 无需修改生产代码 | 测试代码稍复杂 |

当前采用 **ClientBridge 方案**，保持生产代码不变。

### 3.2 为什么需要两个 Nacos Mock Server？

**根本原因**: 测试中 Pod IP 设置为 `127.0.0.1`

**测试流程**:

```
1. 创建 StatefulSet
2. K8sSimulator 创建 Pod，设置 IP = 127.0.0.1
3. Operator CheckNacos 阶段访问 http://127.0.0.1:8848/nacos/v1/core/cluster/nodes
4. 需要本地 8848 端口有 Mock Server 响应
```

**两个 Mock Server 的分工**:

| Mock Server | 端口 | 使用场景 |
|-------------|------|----------|
| MockNacosServer8848 | 127.0.0.1:8848 | 主要使用，响应健康检查 |
| MockNacosServer | 随机端口 | 备用，8848 被占用时使用 |

**为什么不能只用随机端口？**

因为测试中 Pod IP 固定为 `127.0.0.1`，Operator 会访问：
```
http://127.0.0.1:8848/nacos/v1/core/cluster/nodes
```

如果 Mock Server 在随机端口（如 `127.0.0.1:54321`），Operator 无法访问到。

**动态更新支持**:

两个 Mock Server 都支持 `UpdateServers()` 方法，用于扩缩容测试：

```go
// 扩容时更新 Mock Server 返回的节点数
newMockServers := testutil.CreateMockClusterServersWithName(5, 0, "2.1.0", "test-scale")
mockServer.UpdateServers(newMockServers)
mockServer8848.UpdateServers(newMockServers)
```

## 四、测试用例执行流程

### 4.1 Standalone Nacos 创建测试

**文件**: [test/testcase/nacos_create_test.go:19-141](test/testcase/nacos_create_test.go#L19-L141)

**流程**:

```
1. setupTest() - 初始化测试环境
   ├─ 创建 controller-runtime fake client
   ├─ 创建 kubernetes fake clientset
   ├─ 启动 MockNacosServer8848 (127.0.0.1:8848)
   ├─ 创建 Reconciler
   ├─ 创建 K8sSimulator
   └─ 创建 ClientBridge

2. 创建 Nacos CR (Phase=None)
   └─ fakeClient.Create(ctx, nacos)

3. Round 1: Reconcile - PreCheck
   ├─ Phase: None -> Creating
   └─ 返回 RequeueAfter 5s

4. Round 2: Reconcile - MakeEnsure
   ├─ 创建 ConfigMap (在 fakeKubeClient 中)
   ├─ 创建 StatefulSet (在 fakeKubeClient 中)
   ├─ 创建 Service (在 fakeKubeClient 中)
   └─ bridge.SyncFromKubeToCtrl() - 同步到 fakeClient

5. 模拟 K8s 创建 Pod
   └─ simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-standalone", "127.0.0.1")
      ├─ 创建 Pod: test-standalone-0
      ├─ 设置 Pod IP = 127.0.0.1
      ├─ 设置 Pod Status = Running
      └─ 设置 Pod Conditions = Ready

6. Round 3-10: Reconcile - CheckAndMakeHeal
   ├─ CheckKind: 检查 StatefulSet 和 Pod
   ├─ CheckNacos: 访问 http://127.0.0.1:8848/nacos/v1/core/cluster/nodes
   ├─ MockNacosServer8848 返回节点信息
   ├─ 验证节点数量、状态、Leader
   └─ UpdateStatus: Phase -> Running

7. 验证最终状态
   ├─ Phase = Running
   └─ 测试通过 ✓
```

### 4.2 Cluster Nacos 扩容测试

**文件**: [test/testcase/nacos_scale_test.go:17-173](test/testcase/nacos_scale_test.go#L17-L173)

**流程**:

```
1. 创建初始集群 (3 replicas)
   └─ 执行完整创建流程，达到 Running 状态

2. 更新 CR replicas: 3 -> 5
   └─ fakeClient.Update(ctx, updatedNacos)

3. 更新 Mock Server 返回 5 个节点
   ├─ mockServer.UpdateServers(newMockServers)
   └─ mockServer8848.UpdateServers(newMockServers)

4. Reconcile - MakeEnsure
   ├─ 更新 StatefulSet replicas: 3 -> 5
   └─ bridge.SyncFromKubeToCtrl()

5. 模拟新 Pod 创建
   └─ simulator.SimulateStatefulSetPodsWithIP() - 创建 5 个 Pod

6. Reconcile - CheckAndMakeHeal
   ├─ CheckKind: 验证 5 个 Pod Ready
   ├─ CheckNacos: 访问所有 Pod 的健康检查接口
   ├─ 验证返回 5 个节点，状态都是 UP
   └─ UpdateStatus: 保持 Running

7. 验证最终状态
   ├─ Phase = Running
   ├─ Replicas = 5
   ├─ Conditions 包含 5 个节点
   ├─ 1 个 Leader + 4 个 Follower
   └─ 测试通过 ✓
```

## 五、关键测试技巧

### 5.1 同步时机

**规则**: 每次 Reconcile 后立即同步

```go
reconciler.Reconcile(ctx, req)
bridge.SyncFromKubeToCtrl(ctx, "default")
```

### 5.2 Pod IP 设置

**规则**: 统一设置为 `127.0.0.1`，匹配 MockNacosServer8848 的监听地址

```go
simulator.SimulateStatefulSetPodsWithIP(ctx, "default", "test-cluster", "127.0.0.1")
```

### 5.3 Mock 数据更新

**扩缩容测试**: 动态更新 Mock Server 返回的节点数

```go
newMockServers := testutil.CreateMockClusterServersWithName(newReplicas, 0, "2.1.0", stsName)
mockServer.UpdateServers(newMockServers)
if mockServer8848 != nil {
    mockServer8848.UpdateServers(newMockServers)
}
```

### 5.4 多轮 Reconcile

**规则**: 使用循环多次调用 Reconcile，直到达到期望状态

```go
for i := 0; i < 10; i++ {
    reconciler.Reconcile(ctx, req)
    fakeClient.Get(ctx, req.NamespacedName, updatedNacos)
    if updatedNacos.Status.Phase == nacosgroupv1alpha1.PhaseRunning {
        break
    }
}
```

## 六、测试覆盖场景

| 测试文件 | 测试场景 | 验证点 |
|---------|---------|--------|
| nacos_create_test.go | Standalone 创建 | Phase: None -> Creating -> Running |
| nacos_create_test.go | Cluster 创建 | 3 副本集群，Leader 选举 |
| nacos_scale_test.go | Cluster 扩容 | 3 -> 5 副本，节点数量正确 |
| nacos_delete_test.go | 资源删除 | Finalizer 处理，资源清理 |

## 七、总结

### 7.1 Mock 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                      测试环境                            │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────┐         ┌──────────────┐             │
│  │ Reconciler   │────────>│ fakeClient   │             │
│  │              │         │ (ctrl-runtime)│             │
│  └──────┬───────┘         └──────────────┘             │
│         │                         ▲                      │
│         │                         │                      │
│         v                         │ sync                 │
│  ┌──────────────┐         ┌──────┴───────┐             │
│  │OperatorClient│────────>│ClientBridge  │             │
│  │              │         └──────────────┘             │
│  └──────┬───────┘                 │                      │
│         │                         │                      │
│         v                         v                      │
│  ┌──────────────┐         ┌──────────────┐             │
│  │ k8sService   │────────>│fakeKubeClient│             │
│  │              │         │ (clientset)  │             │
│  └──────────────┘         └──────────────┘             │
│                                                          │
│  ┌──────────────┐         ┌──────────────┐             │
│  │K8sSimulator  │────────>│ 模拟 Pod 创建 │             │
│  └──────────────┘         └──────────────┘             │
│                                                          │
│  ┌──────────────┐         ┌──────────────┐             │
│  │CheckNacos    │────────>│MockNacos8848 │             │
│  │http://127... │         │127.0.0.1:8848│             │
│  └──────────────┘         └──────────────┘             │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### 7.2 核心设计原则

1. **最小化生产代码修改**: 通过 ClientBridge 和 K8sSimulator 适配测试，不修改 Operator 代码
2. **真实模拟异步行为**: K8sSimulator 模拟 K8s 的异步资源创建
3. **完整的健康检查**: MockNacosServer8848 响应真实的 Nacos API
4. **支持动态场景**: Mock Server 支持动态更新，测试扩缩容场景

### 7.3 双客户端架构的必要性

| 组件 | 使用的客户端 | 原因 |
|------|-------------|------|
| Controller | controller-runtime client | Controller 框架要求 |
| OperatorClient | kubernetes.Interface | 历史代码，k8sService 依赖 |
| 测试 | 两个 fake client + ClientBridge | 适配双客户端架构 |

### 7.4 双 Mock Server 的必要性

| Mock Server | 端口 | 使用原因 |
|-------------|------|---------|
| MockNacosServer8848 | 127.0.0.1:8848 | 测试中 Pod IP = 127.0.0.1，必须监听 8848 |
| MockNacosServer | 随机端口 | 备用，8848 被占用时使用 |

### 7.5 改进建议

1. **统一客户端**: 重构 OperatorClient 使用 controller-runtime client，消除双客户端
2. **使用真实 IP**: 为每个 Pod 分配不同的 IP，更接近真实环境
3. **增加异常场景**: 测试网络故障、节点宕机、数据库连接失败等场景
4. **性能测试**: 测试大规模集群（如 100 副本）的性能表现
