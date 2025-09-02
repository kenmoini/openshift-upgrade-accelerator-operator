package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	UpgradeAcceleratorDefaultJobPullerImage                 = "registry.redhat.io/rhel9/support-tools:latest"
	UpgradeAcceleratorDefaultNamespace                      = "openshift-upgrade-accelerator"
	UpgradeAcceleratorFinalizer                             = "openshift.kemo.dev/finalizer"
	UpgradeAcceleratorDefaultParallelism                    = 5
	UpgradeAcceleratorDefaultRandomWaitMin                  = 10
	UpgradeAcceleratorDefaultRandomWaitMax                  = 30
	UpgradeAcceleratorMachineConfigOperatorStatusVersionKey = "operator"
	UpgradeAcceleratorDefaultJobTTL                         = 7200
	UpgradeAcceleratorDefaultAppLabelValue                  = "upgrade-accelerator"
)

var (
	UpgradeAcceleratorDefaultJobTolerations = []corev1.Toleration{{
		Operator: corev1.TolerationOpExists,
	}}
)

// UpgradeAcceleratorReconciler reconciles a UpgradeAccelerator object
type UpgradeAcceleratorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
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
