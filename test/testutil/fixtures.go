package testutil

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	nacosgroupv1alpha1 "nacos.io/nacos-operator/api/v1alpha1"
)

// NewNacosStandalone creates a standalone Nacos CR for testing
func NewNacosStandalone(name, namespace string) *nacosgroupv1alpha1.Nacos {
	replicas := int32(1)
	return &nacosgroupv1alpha1.Nacos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nacosgroupv1alpha1.NacosSpec{
			Type:     "standalone",
			Image:    "nacos/nacos-server:v2.1.0",
			Replicas: &replicas,
			Database: nacosgroupv1alpha1.Database{
				TypeDatabase: "embedded",
			},
		},
		Status: nacosgroupv1alpha1.NacosStatus{
			Phase: nacosgroupv1alpha1.PhaseNone,
		},
	}
}

// NewNacosCluster creates a cluster Nacos CR for testing
func NewNacosCluster(name, namespace string, replicas int32) *nacosgroupv1alpha1.Nacos {
	return &nacosgroupv1alpha1.Nacos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nacosgroupv1alpha1.NacosSpec{
			Type:     "cluster",
			Image:    "nacos/nacos-server:v2.1.0",
			Replicas: &replicas,
			Database: nacosgroupv1alpha1.Database{
				TypeDatabase: "embedded",
			},
		},
		Status: nacosgroupv1alpha1.NacosStatus{
			Phase: nacosgroupv1alpha1.PhaseNone,
		},
	}
}

// NewNacosWithMySQL creates a Nacos CR with MySQL database
func NewNacosWithMySQL(name, namespace string) *nacosgroupv1alpha1.Nacos {
	replicas := int32(3)
	return &nacosgroupv1alpha1.Nacos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nacosgroupv1alpha1.NacosSpec{
			Type:           "cluster",
			Image:          "nacos/nacos-server:v2.1.0",
			Replicas:       &replicas,
			MysqlInitImage: "nacos/nacos-mysql:8.0",
			Database: nacosgroupv1alpha1.Database{
				TypeDatabase:  "mysql",
				MysqlHost:     "mysql.default.svc.cluster.local",
				MysqlPort:     "3306",
				MysqlDb:       "nacos",
				MysqlUser:     "nacos",
				MysqlPassword: "nacos123",
			},
		},
		Status: nacosgroupv1alpha1.NacosStatus{
			Phase: nacosgroupv1alpha1.PhaseNone,
		},
	}
}

// NewNacosWithPostgreSQL creates a Nacos CR with PostgreSQL database
func NewNacosWithPostgreSQL(name, namespace string) *nacosgroupv1alpha1.Nacos {
	replicas := int32(3)
	return &nacosgroupv1alpha1.Nacos{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nacosgroupv1alpha1.NacosSpec{
			Type:     "cluster",
			Image:    "nacos/nacos-server:v2.1.0",
			Replicas: &replicas,
			Postgres: nacosgroupv1alpha1.NacosPostgresSpec{
				Host:     "postgres.default.svc.cluster.local",
				Port:     "5432",
				Database: "nacos",
				CredentialsSecretRef: nacosgroupv1alpha1.PGCredentialsSecretRef{
					Name:        "pg-credentials",
					UsernameKey: "username",
					PasswordKey: "password",
				},
			},
			PGInit: nacosgroupv1alpha1.PGInitSpec{
				Enabled:        true,
				TimeoutSeconds: 60,
				SchemaVersion:  1,
				Policy:         "IfNotPresent",
			},
		},
		Status: nacosgroupv1alpha1.NacosStatus{
			Phase: nacosgroupv1alpha1.PhaseNone,
		},
	}
}

// NewNacosWithAdminSecret creates a Nacos CR with admin credentials
func NewNacosWithAdminSecret(name, namespace string) *nacosgroupv1alpha1.Nacos {
	nacos := NewNacosWithPostgreSQL(name, namespace)
	nacos.Spec.AdminCredentialsSecretRef = nacosgroupv1alpha1.AdminCredentialsSecretRef{
		Name:            "admin-credentials",
		UsernameKey:     "username",
		PasswordHashKey: "passwordHash",
	}
	return nacos
}

// NewSecret creates a Secret for testing
func NewSecret(name, namespace string, data map[string]string) *corev1.Secret {
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: secretData,
	}
}

// NewPGCredentialsSecret creates a PostgreSQL credentials secret
func NewPGCredentialsSecret(namespace string) *corev1.Secret {
	return NewSecret("pg-credentials", namespace, map[string]string{
		"username": "nacos",
		"password": "nacos123",
	})
}

// NewAdminCredentialsSecret creates an admin credentials secret
func NewAdminCredentialsSecret(namespace string) *corev1.Secret {
	return NewSecret("admin-credentials", namespace, map[string]string{
		"username":     "nacos",
		"passwordHash": "$2a$10$EuWPZHzz32dJN7jexM34MOeYirDdFAZm2kuWj7VEOJhhZkDrxfvUu", // bcrypt hash of "nacos"
	})
}

// NewReadyPod creates a ready Pod for testing
func NewReadyPod(name, namespace, nacosName string, podIP string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        nacosName,
				"middleware": "nacos",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: podIP,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}
