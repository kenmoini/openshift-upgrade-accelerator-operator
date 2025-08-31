package controller

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpgradeAcceleratorReconciler reconciles a UpgradeAccelerator object
type UpgradeAcceleratorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type ReleaseImageTags struct {
	Name string `json:"name"`
	From string `json:"from"`
}

type OpenShiftClusterVersionState struct {
	// CurrentVersion is derived from the machine-config ClusterOperator
	CurrentVersion string `json:"currentVersion"`
	// DesiredVersion is derived from the ClusterVersion
	DesiredVersion string `json:"desiredVersion"`
	// DesiredImage is the release image that is intended to be used for the upgrade
	DesiredImage string `json:"desiredImage"`
}
