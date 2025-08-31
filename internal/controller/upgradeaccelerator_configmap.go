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

const (
	DefaultPullBinary   = "crictl"
	DefaultPullerScript = `#!/bin/bash
set -euo pipefail
export TOTAL_RELEASE_IMAGE_LIST=$(cat /etc/upgrade-accelerator/release/total_release_image_list.json)
export FILTERED_RELEASE_IMAGE_LIST=$(cat /etc/upgrade-accelerator/release/filtered_release_image_list.json)
chroot /host /bin/bash <<EOT
export PULL_BINARY=` + DefaultPullBinary + `
export KUBECONFIG=/etc/kubernetes/static-pod-resources/kube-apiserver-certs/secrets/node-kubeconfigs/localhost.kubeconfig;
oc whoami;
echo '${FILTERED_RELEASE_IMAGE_LIST}' | jq -r '.[] | [.name, .from] | @tsv' | \
  while IFS=\$'\t' read -r name from; do
    echo "Processing image \$name: \$from"
		\$PULL_BINARY pull \$from
  done
EOT

echo "===== PULLER SCRIPT FINISHED ====="
`
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
	testConfigMap := &corev1.ConfigMap{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: targetNamespace}, testConfigMap); err != nil {
		if kapierrors.IsNotFound(err) {
			// Create the ConfigMap if it doesn't exist
			if err := reconciler.Create(ctx, configMap); err != nil {
				return err
			}
		} else {
			// Check if it's the same
			if !reflect.DeepEqual(testConfigMap.Data, configMap.Data) {
				// Update the ConfigMap if the data has changed
				if err := reconciler.Update(ctx, configMap); err != nil {
					return err
				}
			}
		}
	} else {
		// Check if it's the same
		if !reflect.DeepEqual(testConfigMap.Data, configMap.Data) {
			// Update the ConfigMap if the data has changed
			if err := reconciler.Update(ctx, configMap); err != nil {
				return err
			}
		}
	}
	return nil
}

func (reconciler *UpgradeAcceleratorReconciler) createPullerScriptConfigMap(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, pullerScript string) error {

	configMapName := fmt.Sprintf("release-%s", "puller-script")
	targetNamespace := UpgradeAcceleratorDefaultNamespace
	// Check if the UpgradeAccelerator has any overrides for the namespace
	if upgradeAccelerator.Spec.Config.Scheduling.Namespace != "" {
		targetNamespace = upgradeAccelerator.Spec.Config.Scheduling.Namespace
	}

	// Set some basic labels
	labels := map[string]string{
		"app":                      "upgrade-accelerator",
		"upgrade-accelerator/name": upgradeAccelerator.Name,
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
			"puller.sh": DefaultPullerScript,
		},
	}

	// Check if the ConfigMap exists already
	testConfigMap := &corev1.ConfigMap{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: targetNamespace}, testConfigMap); err != nil {
		if kapierrors.IsNotFound(err) {
			// Create the ConfigMap if it doesn't exist
			if err := reconciler.Create(ctx, configMap); err != nil {
				return err
			}
		} else {
			// Check if it's the same
			if !reflect.DeepEqual(testConfigMap.Data, configMap.Data) {
				// Update the ConfigMap if the data has changed
				if err := reconciler.Update(ctx, configMap); err != nil {
					return err
				}
			}
		}
	} else {
		// Check if it's the same
		if !reflect.DeepEqual(testConfigMap.Data, configMap.Data) {
			// Update the ConfigMap if the data has changed
			if err := reconciler.Update(ctx, configMap); err != nil {
				return err
			}
		}
	}
	return nil
}
