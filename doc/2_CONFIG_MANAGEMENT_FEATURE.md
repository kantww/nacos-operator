# Nacos Operator 配置管理功能文档

## 目录
- [需求背景](#需求背景)
- [功能概述](#功能概述)
- [架构设计](#架构设计)
- [资源关系模型](#资源关系模型)
- [实现原理](#实现原理)
- [使用指南](#使用指南)
- [测试验证](#测试验证)

---

## 需求背景

### 项目背景
nacos-operator 是用于在 Kubernetes 集群中管理自定义的 Nacos CRD，通过 Operator Reconcile 方式来管理 Nacos 集群在 K8s 集群中的生命周期。

### 原有配置方式的问题
在原有实现中，Nacos 集群的配置文件只能通过一个 ConfigMap 全量指定，存在以下问题：
1. **配置管理不灵活**：用户需要管理完整的配置文件，包括很多不应该修改的核心参数
2. **配置变更风险高**：用户可能误修改核心参数，导致集群异常
3. **配置更新不便**：修改配置后需要手动重启 Pod 才能生效

### 需求说明

#### 需求1：用户自定义参数配置管理
只给用户暴露特定的 Nacos 参数，这些参数存在高频修改的可能，且不会显著影响集群性能。其它核心参数由运维团队内置，用户不需要关心。

**实现方式：**
- 用户需要关心的参数通过 `user-config` ConfigMap 指定
- 核心参数通过 `internal-config` ConfigMap 指定
- Operator 将 `user-config` 和 `internal-config` 合并为 `final-config`
- 最终通过 CR 中的 `k8sWrapper.podSpec` 将 `final-config` 挂载进 Pod

#### 需求2：参数修改自动滚动更新
用户可能会随时修改 `user-config` 来修改 Nacos 集群的参数，当用户修改 `user-config` 的 ConfigMap 后，需要滚动重启 Nacos 集群的所有节点来应用新的配置。

**实现方式：**
1. Operator 监听 `user-config`、`internal-config` 的资源变化
2. 当这些资源变化时触发 Operator 的 Reconcile 流程
3. Reconcile 流程首先进行 `user-config` 与 `internal-config` 合并出 `final-config`
4. 计算 `final-config` 内容的 SHA256 digest，写入 Nacos 集群的 StatefulSet template annotations
5. 当 `user-config` 发生变化时，Operator 的合并 config 流程会更新 `final-config`
6. 将新的 `final-config` 的 digest 写入 Nacos 集群的 StatefulSet
7. StatefulSet 中的 template annotations 发生变化，触发对应 Pod 滚动更新

---

## 功能概述

### 核心特性

#### 1. 配置分离
- **用户配置（user-config）**：用户可修改的高频参数
  - 示例：UI 开关、监控端点配置、日志级别等
- **内置配置（internal-config）**：核心参数，由运维团队管理
  - 示例：服务器端口、数据库连接、认证配置等
- **最终配置（final-config）**：由 Operator 自动合并生成
  - 合并顺序：internal-config + user-config
  - user-config 中的参数可以覆盖 internal-config 中的同名参数

#### 2. 自动合并
- Operator 自动读取 user-config 和 internal-config
- 按照 internal-config + user-config 的顺序合并
- 生成 final-config 并挂载到 Nacos Pod

#### 3. 滚动更新
- 计算配置内容的 SHA256 digest（取前16位）
- 将 digest 添加到 StatefulSet 的 template annotations
- 配置变化时，digest 变化触发 Pod 滚动更新

---

## 架构设计

### CRD 结构设计

#### NacosSpec 新增字段
```go
type NacosSpec struct {
    // ... 原有字段 ...

    // 配置管理：用户自定义配置 ConfigMap 引用
    UserConfigRef *ConfigMapRef `json:"userConfigRef,omitempty"`

    // 配置管理：内置配置 ConfigMap 引用
    InternalConfigRef *ConfigMapRef `json:"internalConfigRef,omitempty"`

    // 配置管理：最终合并后的 ConfigMap 名称（由 operator 创建和管理）
    FinalConfigName string `json:"finalConfigName,omitempty"`
}

// ConfigMapRef references a ConfigMap for configuration management
type ConfigMapRef struct {
    Name string `json:"name,omitempty"`
    Key  string `json:"key,omitempty"`
}
```

#### NacosStatus 新增字段
```go
type NacosStatus struct {
    // ... 原有字段 ...

    // Config digest tracks the hash of merged configuration for rolling updates
    ConfigDigest string `json:"configDigest,omitempty"`
}
```

---

## 资源关系模型

### Kubernetes 资源关系图

```
┌─────────────────────────────────────────────────────────────────┐
│                         Nacos CR                                 │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ Spec:                                                      │  │
│  │   userConfigRef:                                          │  │
│  │     name: nacos-user-config                               │  │
│  │     key: user.properties                                  │  │
│  │   internalConfigRef:                                      │  │
│  │     name: nacos-internal-config                           │  │
│  │     key: internal.properties                              │  │
│  │   finalConfigName: nacos-final-config                     │  │
│  └───────────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ Status:                                                    │  │
│  │   configDigest: "3f346e9c2f5179d9"                        │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Operator watches and reconciles
                              ▼
        ┌─────────────────────────────────────────────┐
        │                                             │
        ▼                                             ▼
┌──────────────────┐                        ┌──────────────────┐
│  user-config     │                        │ internal-config  │
│  ConfigMap       │                        │  ConfigMap       │
│                  │                        │                  │
│  Data:           │                        │  Data:           │
│    user.props    │                        │    internal.props│
└──────────────────┘                        └──────────────────┘
        │                                             │
        │                                             │
        └──────────────┬──────────────────────────────┘
                       │
                       │ Operator merges
                       ▼
              ┌──────────────────┐
              │  final-config    │
              │  ConfigMap       │
              │                  │
              │  Data:           │
              │    application.  │
              │    properties    │
              └──────────────────┘
                       │
                       │ Mounted into
                       ▼
              ┌──────────────────┐
              │   StatefulSet    │
              │                  │
              │  Template:       │
              │    Annotations:  │
              │      nacos.io/   │
              │      config-     │
              │      digest:     │
              │      "3f346e..."│
              │                  │
              │    Volumes:      │
              │      - name:     │
              │        config    │
              │        configMap:│
              │          name:   │
              │          final-  │
              │          config  │
              └──────────────────┘
                       │
                       │ Creates and manages
                       ▼
              ┌──────────────────┐
              │   Nacos Pods     │
              │                  │
              │  VolumeMounts:   │
              │    /home/nacos/  │
              │    conf/         │
              │    application.  │
              │    properties    │
              └──────────────────┘
```

### 配置合并流程

```
┌─────────────────┐
│ internal-config │
│                 │
│ # Core params   │
│ server.port=    │
│   8848          │
│ auth.enabled=   │
│   true          │
└────────┬────────┘
         │
         │ Merge (internal first)
         ▼
┌─────────────────────────────────┐
│ final-config                    │
│                                 │
│ # ===== Internal Config =====  │
│ server.port=8848                │
│ auth.enabled=true               │
│                                 │
│ # ===== User Config =====      │
│ console.ui.enabled=true         │
│ endpoints.include=health,info   │
└─────────────────────────────────┘
         ▲
         │
         │ Merge (user second, can override)
         │
┌────────┴────────┐
│  user-config    │
│                 │
│ # User params   │
│ console.ui.     │
│   enabled=true  │
│ endpoints.      │
│   include=...   │
└─────────────────┘
```

### 滚动更新触发流程

```
1. User updates user-config ConfigMap
   │
   ▼
2. Kubernetes triggers Reconcile (ConfigMap watch)
   │
   ▼
3. Operator.EnsureConfigmap()
   ├─ Read user-config
   ├─ Read internal-config
   ├─ Merge configs → final-config
   ├─ Compute SHA256 digest
   └─ Update nacos.Status.ConfigDigest
   │
   ▼
4. Operator.EnsureStatefulset()
   ├─ Build StatefulSet with new digest
   ├─ Set template.annotations["nacos.io/config-digest"] = new_digest
   └─ Call CreateOrUpdateStatefulSet()
   │
   ▼
5. StatefulSet.checkSts() detects annotation change
   │
   ▼
6. StatefulSet controller triggers rolling update
   │
   ▼
7. Pods restart one by one with new config
```

---

## 实现原理

### 核心代码实现

#### 1. 配置合并逻辑

**文件**: [pkg/service/operator/Kind.go](../pkg/service/operator/Kind.go)

```go
func (e *KindClient) EnsureConfigmap(nacos *nacosgroupv1alpha1.Nacos) {
    // 新的配置管理方式：合并 user-config 和 internal-config
    if nacos.Spec.UserConfigRef != nil || nacos.Spec.InternalConfigRef != nil {
        cm := e.buildMergedConfigMap(nacos)
        myErrors.EnsureNormal(e.k8sService.CreateOrUpdateConfigMap(nacos.Namespace, cm))

        // 计算配置的 digest 并保存到 Nacos status 中
        if content, ok := cm.Data["application.properties"]; ok {
            digest := e.computeConfigDigest(content)
            nacos.Status.ConfigDigest = digest
            e.logger.Info("Computed config digest", "digest", digest)
        }
        return
    }

    // 旧的配置方式：直接使用 Config 字段
    if nacos.Spec.Config != "" {
        cm := e.buildConfigMap(nacos)
        myErrors.EnsureNormal(e.k8sService.CreateIfNotExistsConfigMap(nacos.Namespace, cm))
    }
}
```

#### 2. 配置内容合并

```go
func (e *KindClient) buildMergedConfigMap(nacos *nacosgroupv1alpha1.Nacos) *v1.ConfigMap {
    // 读取 internal-config 内容
    internalContent := ""
    if nacos.Spec.InternalConfigRef != nil && nacos.Spec.InternalConfigRef.Name != "" {
        internalCM, err := e.k8sService.GetConfigMap(nacos.Namespace, nacos.Spec.InternalConfigRef.Name)
        // ... 错误处理 ...
        internalContent = internalCM.Data[key]
    }

    // 读取 user-config 内容
    userContent := ""
    if nacos.Spec.UserConfigRef != nil && nacos.Spec.UserConfigRef.Name != "" {
        userCM, err := e.k8sService.GetConfigMap(nacos.Namespace, nacos.Spec.UserConfigRef.Name)
        // ... 错误处理 ...
        userContent = userCM.Data[key]
    }

    // 合并配置内容：internal-config 在前，user-config 在后
    // 这样 user-config 中的参数可以覆盖 internal-config 中的同名参数
    mergedContent := ""
    if internalContent != "" {
        mergedContent += "# ===== Internal Configuration =====\n"
        mergedContent += internalContent
        mergedContent += "\n\n"
    }
    if userContent != "" {
        mergedContent += "# ===== User Configuration =====\n"
        mergedContent += userContent
        mergedContent += "\n"
    }

    // 创建 final-config ConfigMap
    data := make(map[string]string)
    data["application.properties"] = mergedContent

    cm := v1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:        finalConfigName,
            Namespace:   nacos.Namespace,
            Labels:      labels,
            Annotations: nacos.Annotations,
        },
        Data: data,
    }
    return &cm
}
```

#### 3. Digest 计算

```go
func (e *KindClient) computeConfigDigest(content string) string {
    // 使用 crypto/sha256 计算配置内容的哈希值
    hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
    // 取前16个字符作为简短的digest
    if len(hash) > 16 {
        return hash[:16]
    }
    return hash
}
```

#### 4. StatefulSet Digest 注入

```go
func (e *KindClient) buildStatefulset(nacos *nacosgroupv1alpha1.Nacos) *appv1.StatefulSet {
    // ... 构建 StatefulSet ...

    // 如果使用配置管理功能，将 config digest 添加到 StatefulSet template annotations
    // 这样当配置变化时，StatefulSet 会触发滚动更新
    if nacos.Spec.UserConfigRef != nil || nacos.Spec.InternalConfigRef != nil {
        if nacos.Status.ConfigDigest != "" {
            ss.Spec.Template.Annotations["nacos.io/config-digest"] = nacos.Status.ConfigDigest
            e.logger.Info("Added config digest to StatefulSet template", "digest", nacos.Status.ConfigDigest)
        }
    }

    return ss
}
```

#### 5. StatefulSet 更新检测

**文件**: [pkg/service/k8s/statefulset.go](../pkg/service/k8s/statefulset.go)

```go
func checkSts(old *appsv1.StatefulSet, new *appsv1.StatefulSet) operator {
    rsA, _ := json.Marshal(old.Spec.Template.Spec.Containers[0].Resources)
    rsB, _ := json.Marshal(new.Spec.Template.Spec.Containers[0].Resources)

    envA, _ := json.Marshal(old.Spec.Template.Spec.Containers[0].Env)
    envB, _ := json.Marshal(new.Spec.Template.Spec.Containers[0].Env)

    // Check template annotations (for config digest changes)
    annotationsA, _ := json.Marshal(old.Spec.Template.Annotations)
    annotationsB, _ := json.Marshal(new.Spec.Template.Annotations)

    if checkVolumeClaimTemplates(old, new) {
        return Delete
    }

    if !bytes.Equal(rsA, rsB) || *old.Spec.Replicas != *new.Spec.Replicas ||
       !bytes.Equal(envA, envB) || !bytes.Equal(annotationsA, annotationsB) {
        return Update
    }

    return None
}
```

### Reconcile 流程集成

配置管理功能集成到现有的 Reconcile 流程中：

```
Reconcile()
  ├─ PreCheck()
  ├─ PGEnsure()
  ├─ RotateAdmin()
  ├─ MakeEnsure()
  │   ├─ EnsureConfigmap()          ← 配置合并 + digest 计算
  │   ├─ EnsureStatefulsetCluster() ← digest 注入到 annotations
  │   ├─ EnsureHeadlessService()
  │   └─ EnsureClientService()
  ├─ CheckAndMakeHeal()
  └─ UpdateStatus()                 ← 持久化 Status.ConfigDigest
```

---

## 使用指南

### 基本使用

#### 1. 创建用户配置 ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nacos-user-config
  namespace: default
data:
  user.properties: |
    # 用户可修改的参数
    nacos.console.ui.enabled=true
    nacos.core.param.check.enabled=true
    management.endpoints.web.base-path=/actuator
    management.endpoints.web.exposure.include=health,info,prometheus
```

#### 2. 创建内置配置 ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nacos-internal-config
  namespace: default
data:
  internal.properties: |
    # 核心配置参数
    server.servlet.contextPath=/nacos
    server.port=8848
    server.tomcat.accesslog.enabled=false

    # Database configuration
    spring.sql.init.platform=postgresql
    db.url.0=jdbc:postgresql://postgres:5432/nacos
    db.user.0=nacos
    db.password.0=nacos123

    # Auth configuration
    nacos.core.auth.enabled=true
    nacos.core.auth.plugin.nacos.token.secret.key=SecretKey012345678901234567890123456789012345678901234567890123456789
```

#### 3. 创建 Nacos 集群

```yaml
apiVersion: nacos.io/v1alpha1
kind: Nacos
metadata:
  name: my-nacos
  namespace: default
spec:
  type: cluster
  image: nacos/nacos-server:v2.4.3
  replicas: 3
  resources:
    requests:
      cpu: 100m
      memory: 512Mi
    limits:
      cpu: 2
      memory: 2Gi

  # 配置管理：指定用户配置和内置配置的 ConfigMap 引用
  userConfigRef:
    name: nacos-user-config
    key: user.properties
  internalConfigRef:
    name: nacos-internal-config
    key: internal.properties

  # 最终合并后的 ConfigMap 名称（由 operator 自动创建和管理）
  finalConfigName: my-nacos-final-config
```

### 配置更新

#### 修改用户配置触发滚动更新

```bash
# 1. 编辑 user-config ConfigMap
kubectl edit configmap nacos-user-config -n default

# 2. 修改参数，例如：
#    nacos.console.ui.enabled=false

# 3. 保存后，Operator 会自动：
#    - 重新合并配置
#    - 更新 final-config
#    - 计算新的 digest
#    - 更新 StatefulSet annotations
#    - 触发 Pod 滚动更新
```

#### 查看配置更新状态

```bash
# 查看 Nacos CR 状态
kubectl get nacos my-nacos -n default -o yaml

# 查看 ConfigDigest
kubectl get nacos my-nacos -n default -o jsonpath='{.status.configDigest}'

# 查看 StatefulSet annotations
kubectl get statefulset my-nacos -n default -o jsonpath='{.spec.template.metadata.annotations}'

# 查看 Pod 滚动更新状态
kubectl rollout status statefulset my-nacos -n default
```

### 配置验证

```bash
# 查看 final-config 内容
kubectl get configmap my-nacos-final-config -n default -o yaml

# 进入 Pod 查看挂载的配置
kubectl exec -it my-nacos-0 -n default -- cat /home/nacos/conf/application.properties
```

---

## 测试验证

### 单元测试

#### 测试1：配置合并功能

**文件**: [test/testcase/nacos_config_merge_test.go](../test/testcase/nacos_config_merge_test.go)

**测试内容**:
- ✅ 创建 user-config 和 internal-config ConfigMap
- ✅ 创建 Nacos CR 并触发 Reconcile
- ✅ 验证 final-config ConfigMap 在 k8s 集群中正确创建
- ✅ 验证 final-config 内容包含 user-config 和 internal-config 的合并内容
- ✅ 验证 StatefulSet 挂载了 final-config
- ✅ 验证 Pod 正确挂载了 final-config
- ✅ 验证 ConfigDigest 被设置到 Status 中

**运行测试**:
```bash
go test -v ./test/testcase -run TestConfigMerge
```

#### 测试2：配置滚动更新功能

**文件**: [test/testcase/nacos_config_rolling_update_test.go](../test/testcase/nacos_config_rolling_update_test.go)

**测试内容**:
- ✅ 创建初始配置并部署 Nacos 集群
- ✅ 记录初始的 ConfigDigest
- ✅ 更新 user-config ConfigMap
- ✅ 触发 Reconcile
- ✅ 验证 final-config 内容被更新
- ✅ 验证 StatefulSet 中的 digest annotation 被更新
- ✅ 验证 Status 中的 ConfigDigest 被更新
- ✅ 验证 StatefulSet 和 Status 中的 digest 一致

**运行测试**:
```bash
go test -v ./test/testcase -run TestConfigRollingUpdate
```

#### 运行所有配置管理测试

```bash
go test -v ./test/testcase -run "TestConfig"
```

### 测试结果

```
=== RUN   TestConfigMerge
    ✓ Created user-config ConfigMap
    ✓ Created internal-config ConfigMap
    ✓ Synced config ConfigMaps to kubernetes clientset
    ✓ Created Nacos CR with config management fields
    ✓ Phase: Creating
    ✓ Final-config ConfigMap created in k8s cluster
    ✓ Final-config contains merged content (387 bytes)
    ✓ Merged config contains both user and internal parameters
    ✓ StatefulSet created
    ✓ StatefulSet has volume referencing final-config
    ✓ Container has volumeMount for config
    ✓ Created 3 pods, all mounting final-config ConfigMap
    ✓ Status reached Running
    ✓ ConfigDigest set in status
    ✓✓✓ Config merge test PASSED ✓✓✓
--- PASS: TestConfigMerge

=== RUN   TestConfigRollingUpdate
    ✓ Created user-config ConfigMap (Version 1)
    ✓ Created internal-config ConfigMap
    ✓ Initial config digest in StatefulSet
    ✓ Initial final-config content length: 341 bytes
    ✓ Status reached Running
    ✓ Initial ConfigDigest in status
    ✓ Updated user-config ConfigMap (Version 2)
    ✓ Final-config content was updated (372 bytes)
    ✓ Updated config contains new user parameter values
    ✓ StatefulSet digest was updated
    ✓ Status ConfigDigest was updated
    ✓ Digest in StatefulSet matches digest in Status
    ✓✓✓ Config rolling update test PASSED ✓✓✓
--- PASS: TestConfigRollingUpdate

PASS
ok      nacos.io/nacos-operator/test/testcase   0.791s
```

---

## 总结

### 实现完成度

| 功能模块 | 完成度 | 说明 |
|---------|--------|------|
| CRD 结构设计 | ✅ 100% | 添加了所有必要的字段 |
| 配置合并功能 | ✅ 100% | 正确合并 user-config 和 internal-config |
| Pod 挂载验证 | ✅ 100% | Pod 正确挂载 final-config |
| Digest 计算 | ✅ 100% | 使用 SHA256 计算配置哈希 |
| Status 更新 | ✅ 100% | Digest 正确保存到 Status |
| StatefulSet 注解 | ✅ 100% | Digest 添加到 template annotations |
| 滚动更新触发 | ✅ 100% | 配置变化触发 Pod 滚动更新 |
| 单元测试 | ✅ 100% | 所有测试通过 |

**总体完成度：100%**

### 核心优势

1. **配置分离**：用户配置和核心配置分离，降低误操作风险
2. **自动合并**：Operator 自动合并配置，用户无需关心合并逻辑
3. **自动更新**：配置变化自动触发滚动更新，无需手动重启
4. **向后兼容**：保留原有的 Config 字段，不影响现有用户
5. **测试完善**：完整的单元测试覆盖，保证功能稳定性

### 修改的文件清单

#### API 定义
- `api/v1alpha1/nacos_types.go` - 添加配置管理相关字段

#### CRD 定义
- `config/crd/bases/nacos.io_nacos.yaml` - 更新 CRD schema
- `chart/nacos-operator/crds/crd.yaml` - 更新 CRD schema

#### 核心实现
- `pkg/service/operator/Kind.go` - 实现配置合并和 digest 计算逻辑
- `pkg/service/k8s/statefulset.go` - 添加 annotations 变化检测
- `pkg/util/hash/hash.go` - 创建 SHA256 哈希工具函数

#### 测试代码
- `test/testcase/nacos_config_merge_test.go` - 配置合并测试
- `test/testcase/nacos_config_rolling_update_test.go` - 滚动更新测试
- `test/testutil/client_bridge.go` - 添加双向同步方法

#### 示例文件
- `deploy/nacos_cluster_with_config_management.yaml` - 配置管理示例

---

## 附录

### 相关文档
- [Nacos Operator Reconciliation Flow](NACOS_OPERATOR_RECONCILIATION_FLOW.md)
- [Unit Test Implementation](UNIT_TEST_IMPLEMENTATION.md)

### 参考资料
- [Kubernetes StatefulSet Rolling Updates](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#rolling-updates)
- [Kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)
- [Nacos Configuration Management](https://nacos.io/zh-cn/docs/quick-start.html)
