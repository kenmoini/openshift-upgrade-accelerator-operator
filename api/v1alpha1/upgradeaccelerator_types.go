/*
Copyright 2025.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// UpgradeAcceleratorSpec defines the desired state of UpgradeAccelerator.
type UpgradeAcceleratorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// State indicates whether the upgrade accelerator is enabled or disabled.
	// +kubebuilder:default:=enabled
	// +optional
	State string `json:"state,omitempty"`
	// Selector defines the selection criteria for nodes to which the upgrade accelerator applies.
	// +optional
	Selector Selector `json:"selector,omitempty"`
	// Prime indicates whether to pull images to a single node first before rolling out to other nodes.
	// +kubebuilder:default:=true
	// +optional
	Prime bool `json:"prime,omitempty"`
	// Parallelism defines the number of nodes to upgrade in parallel.
	// Default is 5.  If set to 0, all nodes will be upgraded in parallel.
	// +kubebuilder:default:=5
	// +optional
	Parallelism int32 `json:"parallelism,omitempty"`
	// Config provides some overrides and guidance to the operation of the UpgradeAccelerator
	Config UpgradeAcceleratorConfig `json:"config,omitempty"`
}

type UpgradeAcceleratorConfig struct {
	// Scheduling allows for overriding the default tolerations, namespace Jobs are created in, etc.
	Scheduling UpgradeAcceleratorConfigScheduling `json:"scheduling,omitempty"`
	// RandomWait allows for adding a random wait time before starting the image pulling.
	RandomWait UpgradeAcceleratorConfigRandomWait `json:"randomWait,omitempty"`
	// JobImage is the image used for the upgrade jobs.
	// Defaults to registry.redhat.io/rhel9/support-tools:latest
	// +kubebuilder:default:="registry.redhat.io/rhel9/support-tools:latest"
	// +optional
	JobImage string `json:"jobImage,omitempty"`
	// OverrideReleaseVersion allows for specifying a different release version to be pulled.
	// +optional
	OverrideReleaseVersion OverrideReleaseVersion `json:"overrideReleaseVersion,omitempty"`
	// PullScriptConfigMapName is the name of the ConfigMap containing the pull script.
	// If specified the operator will skip creating its own and use the defined one for Jobs.
	// +optional
	PullScriptConfigMapName string `json:"pullScriptConfigMapName,omitempty"`
}

type OverrideReleaseVersion struct {
	Version string `json:"version,omitempty"`
	Image   string `json:"image,omitempty"`
}

// TODO: UpgradeAcceleratorConfigRandomWait
type UpgradeAcceleratorConfigRandomWait struct {
	// Enabled indicates whether random wait is enabled.
	// +kubebuilder:default:=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// MinDuration is the minimum wait duration in seconds.
	// +kubebuilder:default:=10
	// +optional
	MinDuration int32 `json:"minDuration,omitempty"`
	// MaxDuration is the maximum wait duration in seconds.
	// +kubebuilder:default:=30
	// +optional
	MaxDuration int32 `json:"maxDuration,omitempty"`
}

type UpgradeAcceleratorConfigScheduling struct {
	// Tolerations is an array of tolerations for the Jobs created by the UpgradeAccelerator
	// By default the Jobs created will tolerate all taints.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Namespace is the namespace in which the Jobs are created.
	// By default the Jobs are created in the openshift-upgrade-accelerator namespace.
	// If you specify a different namespace you will need to give permissions to the
	// ServiceAccount used by the UpgradeAccelerator controller to manage ConfigMaps/Jobs/Pods.
	Namespace string `json:"namespace,omitempty"`
}

// Selector defines the selection criteria for nodes to which the upgrade accelerator applies.
type Selector struct {
	// NodeSelector is a selector which must be true for the pod to fit on a node.
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	// MachineConfigPools is a list of MachineConfigPools to target.
	MachineConfigPools []string `json:"machineConfigPools,omitempty"`
}

// UpgradeAcceleratorStatus defines the observed state of UpgradeAccelerator.
type UpgradeAcceleratorStatus struct {
	// ClusterInfrastructure indicates the detected infrastructure type of the cluster.
	ClusterInfrastructure string `json:"clusterInfrastructure,omitempty"`
	// CurrentVersion indicates the current version of OpenShift.  This is derived from the machine-config ClusterOperator.
	CurrentVersion string `json:"currentVersion,omitempty"`
	// TargetVersion indicates the target version of OpenShift that is reported by the ClusterVersion CR.
	TargetVersion string `json:"targetVersion,omitempty"`
	// DesiredVersion indicates the desired version of OpenShift that the UpgradeAccelerator is trying to achieve.
	// This is either inferred from the cluster via the TargetVersion if it does not match the CurrentVersion
	// Or it is provided as a status for the config override
	DesiredVersion string `json:"desiredVersion,omitempty"`
	// DesiredImage is the desired release image for the UpgradeAccelerator.
	DesiredImage string `json:"desiredImage,omitempty"`
	// LastCompletedVersion indicates the last completed version of OpenShift releases pulled.
	LastCompletedVersion string `json:"lastCompletedVersion,omitempty"`
	// Conditions represents the latest available observations of the UpgradeAccelerator's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// NodesSelected is a list of nodes that match the selector criteria.
	NodesSelected []string `json:"nodesSelected,omitempty"`
	// NodesPreheated is a list of nodes that have been preheated with the targetVersion release images.
	NodesPreheated []string `json:"nodesPreheated,omitempty"`
	// NodesWarming is a list of nodes that are currently being preheated with the targetVersion release images.
	NodesWarming []string `json:"nodesWarming,omitempty"`
	// NodesWaiting is a list of nodes that are waiting to be preheated with the targetVersion release images.
	NodesWaiting []string `json:"nodesWaiting,omitempty"`
	// PrimerNodes are a list of Nodes that are scheduled to pull first before others
	PrimerNodes []string `json:"primerNodes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// UpgradeAccelerator is the Schema for the upgradeaccelerators API.
type UpgradeAccelerator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UpgradeAcceleratorSpec   `json:"spec,omitempty"`
	Status UpgradeAcceleratorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UpgradeAcceleratorList contains a list of UpgradeAccelerator.
type UpgradeAcceleratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpgradeAccelerator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UpgradeAccelerator{}, &UpgradeAcceleratorList{})
}
