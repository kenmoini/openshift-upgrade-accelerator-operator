package controller

import (
	"context"
	"fmt"
	"reflect"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// createReleaseConfigMap creates a ConfigMap in the upgradeAccelerator Namespace
func (reconciler *UpgradeAcceleratorReconciler) createReleaseConfigMap(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, releaseVersion string, totalList string, filteredList string) error {

	configMapName := fmt.Sprintf("release-%s", releaseVersion)
	targetNamespace := UpgradeAcceleratorDefaultNamespace
	// Check if the UpgradeAccelerator has any overrides for the namespace
	if upgradeAccelerator.Spec.Config.Scheduling.Namespace != "" {
		targetNamespace = upgradeAccelerator.Spec.Config.Scheduling.Namespace
	}

	// Set some basic labels
	labels := map[string]string{
		"app":                                "upgrade-accelerator",
		"upgrade-accelerator/releaseVersion": releaseVersion,
		"upgrade-accelerator/name":           upgradeAccelerator.Name,
	}

	// Create the ConfigMap object
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: targetNamespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(upgradeAccelerator, openshiftv1alpha1.GroupVersion.WithKind("UpgradeAccelerator")),
			},
		},
		Data: map[string]string{
			"total_release_image_list.json":    totalList,
			"filtered_release_image_list.json": filteredList,
		},
	}

	// Check if the ConfigMap already exists
	if err := reconciler.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: targetNamespace}, configMap); err != nil {
		if kapierrors.IsNotFound(err) {
			// Create the ConfigMap if it doesn't exist
			if err := reconciler.Create(ctx, configMap); err != nil {
				return err
			}
		} else {
			// Check if it's the same
			if !reflect.DeepEqual(configMap.Data, map[string]string{
				"total_release_image_list.json":    totalList,
				"filtered_release_image_list.json": filteredList,
			}) {
				// Update the ConfigMap if the data has changed
				configMap.Data = map[string]string{
					"total_release_image_list.json":    totalList,
					"filtered_release_image_list.json": filteredList,
				}
				if err := reconciler.Update(ctx, configMap); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
