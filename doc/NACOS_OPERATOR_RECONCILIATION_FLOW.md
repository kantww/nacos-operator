# Nacos Operator 调谐流程详解

## 概述

Nacos Operator 通过 Kubernetes Controller 模式管理 Nacos 集群的生命周期。每次调谐（Reconcile）会执行一系列步骤来确保实际状态与期望状态一致。

## 调谐入口

文件：[controllers/nacos_controller.go](controllers/nacos_controller.go#L52)

```
Reconcile() -> ReconcileWork() -> 执行6个步骤
```

## 调谐步骤详解

### 步骤1: PreCheck - 前置检查

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L70)

**功能**: 检查 Nacos CR 的当前状态并设置初始 Phase

**操作流程**:
1. 检查 `nacos.Status.Phase`
2. 如果 Phase 为空（PhaseNone），设置为 Creating
3. 如果 Phase 为 Failed，触发修复流程
4. 抛出 CODE_NORMAL 异常，触发状态更新并返回 RequeueAfter 5秒

**K8s 请求**: 无

**期望行为**:
- CR Status.Phase 从空变为 Creating
- Reconcile 返回 RequeueAfter: 5s

---

### 步骤2: PGEnsure - PostgreSQL 连接检查与初始化

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L86)

**功能**: 如果配置了 PostgreSQL，进行连通性检查和数据库初始化

**操作流程**:
1. 检查是否配置了 `nacos.Spec.Postgres.Host`
2. 如果未配置或 PGInit.Enabled 为 false，跳过
3. 读取 PostgreSQL 凭据 Secret
4. 连接 PostgreSQL 数据库
5. 根据 PGInit.Policy 决定是否执行初始化 SQL

**K8s 请求**:
- GET Secret (PostgreSQL 凭据)

**期望行为**:
- 成功连接 PostgreSQL
- 根据策略执行数据库初始化
- 更新 `nacos.Status.PG` 状态

---

### 步骤3: RotateAdmin - 管理员密码轮转

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L110)

**功能**: 如果配置了管理员凭据 Secret，通过直连数据库更新管理员密码

**操作流程**:
1. 检查是否配置了 PostgreSQL 和 AdminCredentialsSecretRef
2. 读取管理员凭据 Secret
3. 直连 PostgreSQL 更新 users 表中的密码哈希

**K8s 请求**:
- GET Secret (管理员凭据)

**期望行为**:
- 管理员密码在数据库中更新
- 更新 `nacos.Status.Admin` 状态

---

### 步骤4: MakeEnsure - 确保 K8s 资源创建

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L43)

**功能**: 根据 Nacos 类型（standalone/cluster）创建或更新所需的 K8s 资源

#### 4.1 Standalone 模式

**操作流程**:
1. ValidationField - 验证并设置默认值
2. EnsureConfigmap - 创建/更新 ConfigMap
3. EnsureStatefulset - 创建/更新 StatefulSet (replicas=1)
4. EnsureService - 创建/更新 Service
5. 如果使用 MySQL，创建 MySQL 初始化 ConfigMap 和 Job

**K8s 请求**:
- GET ConfigMap (检查是否存在)
- CREATE/UPDATE ConfigMap
- GET StatefulSet (检查是否存在)
- CREATE/UPDATE StatefulSet
- GET Service (检查是否存在)
- CREATE/UPDATE Service
- 如果使用 MySQL:
  - CREATE/UPDATE ConfigMap (MySQL 初始化脚本)
  - CREATE/UPDATE Job (MySQL 初始化任务)

**期望行为**:
- ConfigMap 创建成功
- StatefulSet 创建成功，replicas=1
- Service 创建成功
- K8s 开始调度 Pod

#### 4.2 Cluster 模式

**操作流程**:
1. ValidationField - 验证并设置默认值
2. EnsureConfigmap - 创建/更新 ConfigMap
3. EnsureStatefulsetCluster - 创建/更新 StatefulSet (replicas=N)
4. EnsureHeadlessServiceCluster - 创建/更新 Headless Service
5. EnsureClientService - 创建/更新 Client Service
6. 如果使用 MySQL，创建 MySQL 初始化 ConfigMap 和 Job

**K8s 请求**:
- GET ConfigMap (检查是否存在)
- CREATE/UPDATE ConfigMap
- GET StatefulSet (检查是否存在)
- CREATE/UPDATE StatefulSet
- GET Service (检查 Headless Service 是否存在)
- CREATE/UPDATE Service (Headless)
- GET Service (检查 Client Service 是否存在)
- CREATE/UPDATE Service (Client)
- 如果使用 MySQL:
  - CREATE/UPDATE ConfigMap (MySQL 初始化脚本)
  - CREATE/UPDATE Job (MySQL 初始化任务)

**期望行为**:
- ConfigMap 创建成功
- StatefulSet 创建成功，replicas=N
- Headless Service 创建成功 (用于 Pod 间通信)
- Client Service 创建成功 (用于外部访问)
- K8s 开始调度 Pod

---

### 步骤5: CheckAndMakeHeal - 检查并修复

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L98)

**功能**: 检查 K8s 资源和 Nacos 集群健康状态

#### 5.1 CheckKind - 检查 K8s 资源

**文件**: [pkg/service/operator/Check.go](pkg/service/operator/Check.go#L37)

**操作流程**:
1. 获取 StatefulSet，检查副本数是否与 CR 一致
2. 获取 Ready 状态的 Pod 列表
3. 检查 Ready Pod 数量是否满足要求:
   - Standalone: 至少 1 个
   - Cluster: 至少 (replicas+1)/2 个（过半）

**K8s 请求**:
- GET StatefulSet
- LIST Pods (带 label selector)

**期望行为**:
- StatefulSet 存在且副本数正确
- Pod 数量满足最低要求
- 返回 Ready Pod 列表

#### 5.2 CheckNacos - 检查 Nacos 集群

**文件**: [pkg/service/operator/Check.go](pkg/service/operator/Check.go#L57)

**操作流程**:
1. 遍历每个 Ready Pod
2. 调用 Nacos API: `GET http://{pod-ip}:8848/nacos/v1/core/cluster/nodes`
3. 如果配置了 IdentitySecretRef，添加身份验证 Header
4. 验证返回的集群节点信息:
   - 节点数量等于 CR 中的 replicas
   - 所有节点状态为 UP
   - 所有节点的 Leader 一致
5. 更新 `nacos.Status.Conditions` 记录每个节点状态
6. 更新 `nacos.Status.Version` 记录 Nacos 版本

**Nacos Server 请求**:
- GET http://{pod-ip}:8848/nacos/v1/core/cluster/nodes (对每个 Pod)
- 可选: 添加身份验证 Header

**期望行为**:
- 所有 Pod 的 Nacos 服务可访问
- 集群节点数量正确
- 所有节点状态为 UP
- Leader 选举完成且一致
- Status.Conditions 更新为每个节点的详细信息

---

### 步骤6: UpdateStatus - 更新状态为 Running

**文件**: [pkg/service/operator/operaror.go](pkg/service/operator/operaror.go#L105)

**功能**: 将 Nacos CR 状态更新为 Running

**操作流程**:
1. 设置 `nacos.Status.Phase = PhaseRunning`
2. 更新 Status.Event 记录成功事件
3. 调用 K8s API 更新 CR Status

**K8s 请求**:
- UPDATE Nacos CR Status

**期望行为**:
- CR Status.Phase 变为 Running
- Status.Event 记录成功事件
- Reconcile 返回成功，不再 Requeue

---

## 异常处理机制

### 全局异常处理

**文件**: [controllers/nacos_controller.go](controllers/nacos_controller.go#L126)

**机制**:
1. 使用 defer + recover 捕获 panic
2. 根据错误码分类处理:
   - CODE_NORMAL: 正常流程中断，更新状态后返回
   - 其他错误: 如果超过3分钟仍未成功，设置 Phase 为 Failed

**返回值**:
- 返回 false 时，Reconcile 返回 RequeueAfter: 5s
- 返回 true 时，Reconcile 返回成功

---

## 完整调谐流程示例

### Standalone Nacos 创建流程

| Round | 步骤 | K8s 操作 | Nacos Server 操作 | CR Phase | 返回 |
|-------|------|----------|-------------------|----------|------|
| 1 | PreCheck | 无 | 无 | None -> Creating | Requeue 5s |
| 2 | MakeEnsure | CREATE ConfigMap, StatefulSet, Service | 无 | Creating | Requeue 5s |
| 3 | CheckKind | GET StatefulSet, LIST Pods | 无 | Creating | Requeue 5s |
| 4 | CheckKind | GET StatefulSet, LIST Pods (Pod Ready) | 无 | Creating | Requeue 5s |
| 5 | CheckNacos | GET StatefulSet, LIST Pods | GET /nacos/v1/core/cluster/nodes | Creating | Requeue 5s |
| 6 | UpdateStatus | UPDATE Nacos Status | 无 | Creating -> Running | Success |

### Cluster Nacos 创建流程

| Round | 步骤 | K8s 操作 | Nacos Server 操作 | CR Phase | 返回 |
|-------|------|----------|-------------------|----------|------|
| 1 | PreCheck | 无 | 无 | None -> Creating | Requeue 5s |
| 2 | MakeEnsure | CREATE ConfigMap, StatefulSet, Headless Service, Client Service | 无 | Creating | Requeue 5s |
| 3 | CheckKind | GET StatefulSet, LIST Pods | 无 | Creating | Requeue 5s |
| 4 | CheckKind | GET StatefulSet, LIST Pods (部分 Pod Ready) | 无 | Creating | Requeue 5s |
| 5 | CheckKind | GET StatefulSet, LIST Pods (过半 Pod Ready) | 无 | Creating | Requeue 5s |
| 6 | CheckNacos | GET StatefulSet, LIST Pods | GET /nacos/v1/core/cluster/nodes (对每个 Pod) | Creating | Requeue 5s |
| 7 | UpdateStatus | UPDATE Nacos Status | 无 | Creating -> Running | Success |

### Cluster Nacos 扩容流程

| Round | 步骤 | K8s 操作 | Nacos Server 操作 | CR Phase | 返回 |
|-------|------|----------|-------------------|----------|------|
| 1 | MakeEnsure | UPDATE StatefulSet (replicas 3->5) | 无 | Running | Requeue 5s |
| 2 | CheckKind | GET StatefulSet, LIST Pods (等待新 Pod) | 无 | Running | Requeue 5s |
| 3 | CheckKind | GET StatefulSet, LIST Pods (新 Pod Ready) | 无 | Running | Requeue 5s |
| 4 | CheckNacos | GET StatefulSet, LIST Pods | GET /nacos/v1/core/cluster/nodes (验证5个节点) | Running | Requeue 5s |
| 5 | UpdateStatus | UPDATE Nacos Status | 无 | Running | Success |

---

## 测试用例编写要点

### 1. K8s 模拟器需要模拟的行为

- **StatefulSet 创建后**: 不会立即有 Pod
- **Pod 创建**: 需要手动调用 `simulator.SimulateStatefulSetPods()`
- **Pod Ready**: 需要设置 Pod Status.Phase = Running 和 Conditions
- **Pod IP**: 测试中设置为 127.0.0.1 以访问 Mock Nacos Server

### 2. Mock Nacos Server 需要返回的数据

- **API**: `/nacos/v1/core/cluster/nodes`
- **返回**: 包含所有节点信息的 JSON
- **关键字段**:
  - `data[].state`: "UP"
  - `data[].extendInfo.raftMetaData.metaDataMap.naming_persistent_service_v2.leader`: 一致的 Leader
  - `data[]` 数组长度等于 replicas

### 3. 两个 K8s 客户端的问题

当前测试使用两个客户端:
- `controller-runtime/pkg/client` (fakeClient): Controller 使用
- `k8s.io/client-go/kubernetes` (kubeClient): OperatorClient 内部使用

**原因**: OperatorClient 内部的 k8sService 使用 kubernetes.Interface，需要 clientset

**解决方案**: 需要保持两个客户端数据同步，使用 ClientBridge

### 4. 关键的同步点

- 每次 Reconcile 后，调用 `bridge.SyncFromKubeToCtrl()` 同步资源
- 模拟 Pod 创建后，需要同时更新两个客户端
- StatefulSet 状态更新需要同步

---

## 总结

Nacos Operator 的调谐流程分为6个步骤，每个步骤都有明确的职责。测试用例需要模拟 K8s 的异步行为（资源创建、Pod 调度）和 Nacos Server 的 API 响应，通过多轮 Reconcile 来验证最终状态达到 Running。
