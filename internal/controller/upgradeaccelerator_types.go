package controller

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	UpgradeAcceleratorDefaultJobPullerImage = "registry.redhat.io/rhel9/support-tools:latest"
	UpgradeAcceleratorDefaultNamespace      = "openshift-upgrade-accelerator"
	UpgradeAcceleratorDefaultParallelism    = 5
	UpgradeAcceleratorDefaultRandomWaitMin  = 10
	UpgradeAcceleratorDefaultRandomWaitMax  = 30
)

// UpgradeAcceleratorReconciler reconciles a UpgradeAccelerator object
type UpgradeAcceleratorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// ReleaseImageTags represents the release image tags used for upgrades
type ReleaseImageTags struct {
	Name string `json:"name"`
	From string `json:"from"`
}

// OpenShiftClusterVersionState represents the state of the determined OpenShift versions
type OpenShiftClusterVersionState struct {
	// CurrentVersion is derived from the machine-config ClusterOperator
	CurrentVersion string `json:"currentVersion"`
	// DesiredVersion is derived from the ClusterVersion
	DesiredVersion string `json:"desiredVersion"`
	// DesiredImage is the release image that is intended to be used for the upgrade
	DesiredImage string `json:"desiredImage"`
}
