package controller

import (
	"context"
	"time"

	//"github.com/operator-framework/operator-lib/proxy"
	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/resource"
)

type PullJob struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	ContainerImage string `json:"containerImage"`
	ConfigMapName  string `json:"configMapName"`
}

type DebugOptions struct {
	PreservePod   bool              `json:"preservePod"`
	NoStdin       bool              `json:"noStdin"`
	TTY           bool              `json:"tty"`
	DisableTTY    bool              `json:"disableTTY"`
	Timeout       time.Duration     `json:"timeout"`
	Quiet         bool              `json:"quiet"`
	Command       []string          `json:"command"`
	Annotations   map[string]string `json:"annotations"`
	AsRoot        bool              `json:"asRoot"`
	AsNonRoot     bool              `json:"asNonRoot"`
	Namespace     string            `json:"namespace"`
	AsUser        int64             `json:"asUser"`
	ContainerName string            `json:"containerName"`
	NodeName      string            `json:"nodeName"`
	Resources     []string          `json:"resources"`
	AddEnv        []corev1.EnvVar   `json:"addEnv"`

	Builder func() *resource.Builder
}

func createPullJob(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) error {
	// Implementation for creating a pull pod
	//envProxyConfig := proxy.ReadProxyVarsFromEnv()
	// pullPod := &corev1.Pod{
	// 	ObjectMeta: metav1.ObjectMeta{Name: "rtw"},
	// 	Spec: corev1.PodSpec{
	// 		RestartPolicy: corev1.RestartPolicyNever,
	// 		Containers: []corev1.Container{
	// 			corev1.Container{
	// 				Name:    "main",
	// 				Image:   "python:3.8",
	// 				Command: []string{"python"},
	// 				Args:    []string{"-c", "print('hello world')"},
	// 			},
	// 		},
	// 	},
	// }
	return nil
}
