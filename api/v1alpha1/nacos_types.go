/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"encoding/json"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NacosSpec defines the desired state of Nacos
type NacosSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// 通用配置
	Image            string                    `json:"image,omitempty"`
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,15,rep,name=imagePullSecrets"`
	Replicas         *int32                    `json:"replicas,omitempty"`
	//VolumeClaimTemplates []v1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty" protobuf:"bytes,4,rep,name=volumeClaimTemplates"`
	Resources      v1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,8,opt,name=resources"`
	Affinity       *v1.Affinity            `json:"affinity,omitempty" protobuf:"bytes,18,opt,name=affinity"`
	Tolerations    []v1.Toleration         `json:"tolerations,omitempty" protobuf:"bytes,22,opt,name=tolerations"`
	NodeSelector   map[string]string       `json:"nodeSelector,omitempty" protobuf:"bytes,7,rep,name=nodeSelector"`
	LivenessProbe  *v1.Probe               `json:"livenessProbe,omitempty" protobuf:"bytes,10,opt,name=livenessProbe"`
	ReadinessProbe *v1.Probe               `json:"readinessProbe,omitempty" protobuf:"bytes,11,opt,name=readinessProbe"`
	Env            []v1.EnvVar             `json:"env,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,7,rep,name=env"`
	MysqlInitImage string                  `json:"mysqlInitImage,omitempty"`

	// 自定义配置
	// 部署模式
	Type         string   `json:"type,omitempty"`
	FunctionMode string   `json:"function_mode,omitempty"`
	Database     Database `json:"database,omitempty"`
	Volume       Storage  `json:"volume,omitempty"`
	// 配置文件
	Config string `json:"config,omitempty"`
	// 配置管理：用户自定义配置 ConfigMap 引用
	UserConfigRef *ConfigMapRef `json:"userConfigRef,omitempty"`
	// 配置管理：内置配置 ConfigMap 引用
	InternalConfigRef *ConfigMapRef `json:"internalConfigRef,omitempty"`
	// 配置管理：最终合并后的 ConfigMap 名称（由 operator 创建和管理）
	FinalConfigName string `json:"finalConfigName,omitempty"`
	// 开启认证
	Certification Certification `json:"certification,omitempty"`
	// 通用k8s配置包装器
	K8sWrapper K8sWrapper `json:"k8sWrapper,omitempty"`
    // Operator 专用：Postgres 直连配置与初始化控制（不影响 Nacos 运行时配置）
    Postgres NacosPostgresSpec `json:"postgres,omitempty"`
    PGInit   PGInitSpec   `json:"pgInit,omitempty"`
    // Admin credentials secret (username + bcrypt hash), used for direct-DB rotation
    AdminCredentialsSecretRef AdminCredentialsSecretRef `json:"adminCredentialsSecretRef,omitempty"`
    // Optional external checksum used to explicitly trigger rotation when changed
    AdminSecretChecksum       string                    `json:"adminSecretChecksum,omitempty"`
    // Identity header source from Secret (preferred)
    IdentitySecretRef         *IdentitySecretRef        `json:"identitySecretRef,omitempty"`
}

type Certification struct {
	Enabled            bool   `json:"enabled,omitempty"`
	Token              string `json:"token,omitempty"`
	TokenExpireSeconds string `json:"token_expire_seconds,omitempty"`
	CacheEnabled       bool   `json:"cache_enabled,omitempty"`
}

// ConfigMapRef references a ConfigMap for configuration management
type ConfigMapRef struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

type K8sWrapper struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	PodSpec PodSpecWrapper `json:"PodSpec,omitempty"`
}

// +kubebuilder:pruning:PreserveUnknownFields
type PodSpecWrapper struct {
	Spec v1.PodSpec `json:"-"`
}

// MarshalJSON defers JSON encoding to the wrapped map
func (m *PodSpecWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Spec)
}

// UnmarshalJSON will decode the data into the wrapped map
func (m *PodSpecWrapper) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &m.Spec)
}

type Storage struct {
    VolumeClaimTemplate  *v1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
    EmptyDir             *v1.EmptyDirVolumeSource  `json:"emptyDir,omitempty"`
    HostPath             *v1.HostPathVolumeSource  `json:"hostPath,omitempty"`
    KeepAfterDeletion    bool                      `json:"keepAfterDeletion,omitempty"`
    PersistentVolumeSize string                    `json:"persistentVolumeSize,omitempty"`
}

type Database struct {
	TypeDatabase  string `json:"type,omitempty" patchStrategy:"merge" patchMergeKey:"name" protobuf:"bytes,7,rep,name=type"`
	MysqlHost     string `json:"mysqlHost,omitempty"`
	MysqlPort     string `json:"mysqlPort,omitempty"`
	MysqlDb       string `json:"mysqlDb,omitempty"`
	MysqlUser     string `json:"mysqlUser,omitempty"`
	MysqlPassword string `json:"mysqlPassword,omitempty"`
}

// Operator 直连 PG 的凭据引用
type PGCredentialsSecretRef struct {
    Name        string `json:"name,omitempty"`
    UsernameKey string `json:"usernameKey,omitempty"`
    PasswordKey string `json:"passwordKey,omitempty"`
}

// Operator 直连 PG 的配置（不影响 Nacos 自身的连接方式）
type NacosPostgresSpec struct {
    Host                 string                 `json:"host,omitempty"`
    Port                 string                 `json:"port,omitempty"`
    Database             string                 `json:"database,omitempty"`
    CredentialsSecretRef PGCredentialsSecretRef `json:"credentialsSecretRef,omitempty"`
}

// 初始化控制（Operator 侧）
type PGInitSpec struct {
    Enabled        bool                     `json:"enabled,omitempty"`
    TimeoutSeconds int32                    `json:"timeoutSeconds,omitempty"`
    // Desired schema version; used with policy to decide re-init or migrations
    SchemaVersion  int32                    `json:"schemaVersion,omitempty"`
    // Init policy: IfNotPresent|Always|Never|BumpVersion (default IfNotPresent)
    Policy         string                   `json:"policy,omitempty"`
}

// IdentitySecretRef references a Secret that holds Nacos server identity header
// key and value. Keys default to identity_key / identity_value when omitted.
type IdentitySecretRef struct {
    Name     string `json:"name,omitempty"`
    KeyKey   string `json:"keyKey,omitempty"`
    ValueKey string `json:"valueKey,omitempty"`
}

// AdminCredentialsSecretRef references the Secret that holds admin username and password hash
type AdminCredentialsSecretRef struct {
    Name            string `json:"name,omitempty"`
    UsernameKey     string `json:"usernameKey,omitempty"`
    PasswordHashKey string `json:"passwordHashKey,omitempty"`
}

// NacosStatus defines the observed state of Nacos
type NacosStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// 记录实例状态
    Conditions []NacosCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`
	// 记录事件
	Event []Event `json:"event,omitempty" protobuf:"bytes,4,opt,name=event"`
	// 运行状态，主要根据这个字段用来判断是否正常
	Phase Phase `json:"phase,omitempty"`

    Version string `json:"version,omitempty"`

    // PG reflects Postgres initialization status (operator-managed)
    PG PGStatus `json:"pg,omitempty"`
    // Admin password rotation status
    Admin AdminStatus `json:"admin,omitempty"`
    // Config digest tracks the hash of merged configuration for rolling updates
    ConfigDigest string `json:"configDigest,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Nacos is the Schema for the nacos API
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="dbType",type=string,JSONPath=`.spec.database.type`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.version`
// +kubebuilder:printcolumn:name="CreateTime",type=string,JSONPath=`.metadata.creationTimestamp`
type Nacos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NacosSpec   `json:"spec,omitempty"`
	Status NacosStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NacosList contains a list of Nacos
type NacosList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Nacos `json:"items"`
}

func init() {
    SchemeBuilder.Register(&Nacos{}, &NacosList{})
}

// 状况
type NacosCondition struct {
	// Type is the type of the condition.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions
	Type string `json:"type" protobuf:"bytes,1,opt,name=type,casttype=PodConditionType"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions
	Status string `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// Last time we probed the condition.
	// +optional
	//LastProbeTime metav1.Time `json:"lastProbeTime,omitempty" protobuf:"bytes,3,opt,name=lastProbeTime"`
	// Last time the condition transitioned from one status to another.
	// +optional
	//LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`

	Instance string `json:"instance,omitempty" protobuf:"bytes,4,opt,name=instance"`

	HostIP string `json:"hostIP,omitempty" protobuf:"bytes,4,opt,name=hostIP"`

	PodName string `json:"podName,omitempty" protobuf:"bytes,4,opt,name=nodeName"`

	NodeName string `json:"nodeName,omitempty" protobuf:"bytes,4,opt,name=nodeName"`
}

// 事件
type Event struct {
	Status bool `json:"status"`

	// 最早出现时间
	FirstAppearTime metav1.Time `json:"firstAppearTime,omitempty" protobuf:"bytes,3,opt,name=firstAppearTime"`

	// 更新事件
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`

	// 时间描述
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`

	// 错误码
	Code int `json:"code,omitempty" protobuf:"bytes,4,opt,name=reason"`
}

type Phase string

const (
    PhaseRunning  Phase = "Running"
    PhaseNone     Phase = ""
    PhaseCreating Phase = "Creating"
    PhaseFailed   Phase = "Failed"
    PhaseScale    Phase = "Scaling"
)

// PGStatus describes the observed state of Postgres initialization.
type PGStatus struct {
    Initialized                  bool   `json:"initialized,omitempty"`
    InitVersion                  int32  `json:"initVersion,omitempty"`
    LastInitTime                 metav1.Time `json:"lastInitTime,omitempty"`
    LastResult                   string `json:"lastResult,omitempty"`
    LastMessage                  string `json:"lastMessage,omitempty"`
}

// AdminStatus tracks admin password rotation
type AdminStatus struct {
    LastRotateTime            metav1.Time `json:"lastRotateTime,omitempty"`
    LastResult                string      `json:"lastResult,omitempty"`
    LastMessage               string      `json:"lastMessage,omitempty"`
    LastSecretResourceVersion string      `json:"lastSecretResourceVersion,omitempty"`
    LastSecretChecksum        string      `json:"lastSecretChecksum,omitempty"`
}
