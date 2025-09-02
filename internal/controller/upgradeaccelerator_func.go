package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type slice struct{ sort.StringSlice }

func (s slice) Less(d, e int) bool {
	t := strings.Map(unicode.ToUpper, s.StringSlice[d])
	u := strings.Map(unicode.ToUpper, s.StringSlice[e])
	return t < u
}

func sortSliceOfStrings(input []string) []string {
	a := slice{
		sort.StringSlice(input),
	}
	sort.Sort(a)
	return a.StringSlice
}

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

func (reconciler *UpgradeAcceleratorReconciler) getCurrentVersion(ctx context.Context) (string, error) {
	// Get the machine-config ClusterOperator
	clusterOperator := &configv1.ClusterOperator{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "machine-config", Namespace: "openshift-machine-api"}, clusterOperator); err != nil {
		return "", err
	}
	// Get the object with the key name matching operator
	matchedVersion := ""
	for _, version := range clusterOperator.Status.Versions {
		if version.Name == UpgradeAcceleratorMachineConfigOperatorStatusVersionKey {
			matchedVersion = version.Version
		}
	}
	if matchedVersion == "" {
		return "", fmt.Errorf("failed to find machine-config operator version")
	} else {
		return matchedVersion, nil
	}
}

func (reconciler *UpgradeAcceleratorReconciler) getDesiredVersion(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (string, string, error) {

	// Check if there is an override for the desired version
	if upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version != "" && upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image != "" {
		return upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version, upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image, nil
	}
	// Check if we need to determine the image from the release version
	if upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version != "" && upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image == "" {
		// Get the release image from the version
		releaseImage, err := GetReleaseImageFromVersion(upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version)
		if err != nil {
			return "", "", err
		}
		return upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version, releaseImage, nil
	}
	// Alternatively, check if the version needs to be determined from the image
	if upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image != "" && upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Version == "" {
		// Get the release version from the image
		releaseVersion, err := GetReleaseVersionFromImage(upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image)
		if err != nil {
			return "", "", err
		}
		return releaseVersion, upgradeAccelerator.Spec.Config.OverrideReleaseVersion.Image, nil
	}

	// Get the ClusterVersion
	clusterVersion := &configv1.ClusterVersion{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "version", Namespace: "openshift-cluster-version"}, clusterVersion); err != nil {
		return "", "", err
	}
	return clusterVersion.Status.Desired.Version, clusterVersion.Status.Desired.Image, nil
}

func (reconciler *UpgradeAcceleratorReconciler) getClusterVersionState(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) (OpenShiftClusterVersionState, error) {
	// Get the current version from the machine-config ClusterOperator
	currentVersion, err := reconciler.getCurrentVersion(ctx)
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
			if version.Name == UpgradeAcceleratorMachineConfigOperatorStatusVersionKey {
				oldVersion = version.Version
			}
		}
		for _, version := range newStatus.Versions {
			if version.Name == UpgradeAcceleratorMachineConfigOperatorStatusVersionKey {
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
func getOpenShiftInfrastructureType(ctx context.Context, c client.Client) (string, error) {
	infrastructure := &configv1.Infrastructure{}
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster"}, infrastructure); err != nil {
		return "", err
	}
	return string(infrastructure.Status.PlatformStatus.Type), nil
}

// hashString generates a short 8 character SHA256 hash of an input string
func hashString(name string) string {
	h := sha256.New()
	h.Write([]byte(name))
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}
