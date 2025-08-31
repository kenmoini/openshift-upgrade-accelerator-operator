package controller

import (
	"context"
	"fmt"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// createOperatorNamespace creates the namespace for the Operator to deploy Jobs into
func (reconciler *UpgradeAcceleratorReconciler) createOperatorNamespace(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (targetNamespace string, err error) {
	targetNamespace = UpgradeAcceleratorDefaultNamespace
	// Check if the UpgradeAccelerator has any overrides for the namespace
	if upgradeAccelerator.Spec.Config.Scheduling.Namespace != "" {
		targetNamespace = upgradeAccelerator.Spec.Config.Scheduling.Namespace
	}

	// Set some basic labels
	labels := map[string]string{
		"app": "upgrade-accelerator",
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   targetNamespace,
			Labels: labels,
		},
	}
	// Check if the namespace exists already
	if err = reconciler.Get(ctx, types.NamespacedName{Name: targetNamespace}, namespace); err != nil {
		if kapierrors.IsNotFound(err) {
			// Namespace does not exist, create it
			if err := reconciler.Create(ctx, namespace); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}
	return targetNamespace, nil
}

func (reconciler *UpgradeAcceleratorReconciler) getCurrentVersion(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (string, error) {
	// Get the machine-config ClusterOperator
	clusterOperator := &configv1.ClusterOperator{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "machine-config", Namespace: "openshift-machine-api"}, clusterOperator); err != nil {
		return "", err
	}
	// Get the object with the key name matching operator
	matchedVersion := ""
	for _, version := range clusterOperator.Status.Versions {
		if version.Name == "operator" {
			matchedVersion = version.Version
		}
	}
	if matchedVersion == "" {
		return "", fmt.Errorf("Failed to find machine-config operator version")
	} else {
		return matchedVersion, nil
	}
}

func (reconciler *UpgradeAcceleratorReconciler) getDesiredVersion(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (string, string, error) {
	// Get the ClusterVersion
	clusterVersion := &configv1.ClusterVersion{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "version", Namespace: "openshift-cluster-version"}, clusterVersion); err != nil {
		return "", "", err
	}
	return clusterVersion.Status.Desired.Version, clusterVersion.Status.Desired.Image, nil
}

func (reconciler *UpgradeAcceleratorReconciler) getClusterVersionState(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (OpenShiftClusterVersionState, error) {
	// Get the current version from the machine-config ClusterOperator
	currentVersion, err := reconciler.getCurrentVersion(ctx, upgradeAccelerator)
	if err != nil {
		return OpenShiftClusterVersionState{}, err
	}

	// Get the desired version from the ClusterVersion
	desiredVersion, desiredImage, err := reconciler.getDesiredVersion(ctx, upgradeAccelerator)
	if err != nil {
		return OpenShiftClusterVersionState{}, err
	}

	return OpenShiftClusterVersionState{
		CurrentVersion: currentVersion,
		DesiredVersion: desiredVersion,
		DesiredImage:   desiredImage,
	}, nil
}

// hasClusterVersionChanged checks two instances of ClusterVersion and compares if some fields have changed
func hasClusterVersionChanged(o *configv1.ClusterVersion, n *configv1.ClusterVersion) bool {
	var oldStatus, newStatus configv1.ClusterVersionStatus
	// Must check the state of the inputs otherwise may end up with a nil pointer dereference
	if o == nil || n == nil {
		return false
	}
	if o != nil {
		oldStatus = o.Status
	}
	if n != nil {
		newStatus = n.Status
	}
	// Check if the desired version has changed
	if oldStatus.Desired.Version != newStatus.Desired.Version {
		log := logf.FromContext(context.TODO())
		log.Info("ClusterVersion has changed", "oldVersion", oldStatus.Desired.Version, "newVersion", newStatus.Desired.Version)
		return true
	}
	return false
}

// hasMachineConfigClusterOperatorChanged filters through the list of ClusterOperators and checks if the machine-config ClusterOperator has changed
func hasMachineConfigClusterOperatorChanged(o *configv1.ClusterOperator, n *configv1.ClusterOperator) bool {
	var oldStatus, newStatus configv1.ClusterOperatorStatus
	// Must check the state of the inputs otherwise may end up with a nil pointer dereference
	if o == nil || n == nil {
		return false
	}
	if o != nil {
		oldStatus = o.Status
	}
	if n != nil {
		newStatus = n.Status
	}
	// Check if this is the machine-config CO
	if o.Name == "machine-config" {
		// Get the .status.versions of both the old and new spec
		oldVersion, newVersion := "", ""
		for _, version := range oldStatus.Versions {
			if version.Name == "operator" {
				oldVersion = version.Version
			}
		}
		for _, version := range newStatus.Versions {
			if version.Name == "operator" {
				newVersion = version.Version
			}
		}
		// Make sure both aren't empty
		if oldVersion != "" && newVersion != "" {
			if oldVersion != newVersion {
				log := logf.FromContext(context.TODO())
				log.Info("machine-config ClusterOperator version has changed", "oldVersion", oldVersion, "newVersion", newVersion)
				return true
			}
		}
	}
	return false
}

// getOpenShiftInfrastructureType retrieves the OpenShift infrastructure type.
// This is used to filter out any unnecessary images.
func getOpenShiftInfrastructureType(ctx context.Context, req ctrl.Request, client client.Client) (string, error) {
	infrastructure := &configv1.Infrastructure{}
	if err := client.Get(ctx, types.NamespacedName{Name: "cluster"}, infrastructure); err != nil {
		return "", err
	}
	return string(infrastructure.Status.PlatformStatus.Type), nil
}
