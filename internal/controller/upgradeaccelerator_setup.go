package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
)

// setupClusterVersion gets the current cluster version state and sets any needed status then passes the clusterVersionState
func (r *UpgradeAcceleratorReconciler) setupClusterVersion(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	logger *logr.Logger) (clusterVersionState OpenShiftClusterVersionState, upgradeAcceleratorStatusChanged bool, err error) {
	// Cluster Version State
	upgradeAcceleratorStatusChanged = false
	clusterVersionState, err = r.getClusterVersionState(ctx, upgradeAccelerator)
	if err != nil {
		logger.V(1).Error(err, "Failed to get Cluster Version State")
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_CLUSTER_VERSION, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_RUNNING, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return OpenShiftClusterVersionState{}, upgradeAcceleratorStatusChanged, err
	}
	logger.V(1).Info("Cluster Version State", "state", clusterVersionState)

	// Set the UpgradeAcceleratorStatus for the CurrentVersion, DesiredVersion, DesiredImage, and TargetVersion
	if upgradeAccelerator.Status.CurrentVersion != clusterVersionState.CurrentVersion {
		upgradeAccelerator.Status.CurrentVersion = clusterVersionState.CurrentVersion
		upgradeAcceleratorStatusChanged = true
		logger.V(1).Info("Updated UpgradeAccelerator status with currentVersion")
	}
	if upgradeAccelerator.Status.TargetVersion != clusterVersionState.DesiredVersion {
		upgradeAccelerator.Status.TargetVersion = clusterVersionState.DesiredVersion
		upgradeAcceleratorStatusChanged = true
		logger.V(1).Info("Updated UpgradeAccelerator status with targetVersion")
	}
	if upgradeAccelerator.Status.DesiredVersion != clusterVersionState.DesiredVersion {
		upgradeAccelerator.Status.DesiredVersion = clusterVersionState.DesiredVersion
		upgradeAcceleratorStatusChanged = true
		logger.V(1).Info("Updated UpgradeAccelerator status with desiredVersion")
	}
	if upgradeAccelerator.Status.DesiredImage != clusterVersionState.DesiredImage {
		upgradeAccelerator.Status.DesiredImage = clusterVersionState.DesiredImage
		upgradeAcceleratorStatusChanged = true
		logger.V(1).Info("Updated UpgradeAccelerator status with desiredImage")
	}
	if upgradeAcceleratorStatusChanged {
		err = r.Status().Update(ctx, upgradeAccelerator)
		if err != nil {
			logger.V(1).Error(err, "Failed to update UpgradeAccelerator status for versions")
			return OpenShiftClusterVersionState{}, upgradeAcceleratorStatusChanged, err
		}
	}
	return clusterVersionState, upgradeAcceleratorStatusChanged, nil
}

func (r *UpgradeAcceleratorReconciler) setupResources(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	clusterVersionState OpenShiftClusterVersionState, logger *logr.Logger) (operatorNamespace string, err error) {
	// ==========================================================================================
	// Get the Release Images
	// ==========================================================================================
	releaseImages, err := GetReleaseImages(clusterVersionState.DesiredImage)
	if err != nil {
		logger.V(1).Error(err, "Failed to get Release Images")
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_RELEASE_IMAGES, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return "", err
	}
	logger.V(1).Info("Release Images ("+fmt.Sprintf("%d", len(releaseImages))+"): ", "images", releaseImages)

	// ==========================================================================================
	// Filter the target release images
	// ==========================================================================================
	filteredReleaseImages := FilterReleaseImages(releaseImages, upgradeAccelerator.Status.ClusterInfrastructure)
	logger.V(1).Info("Filtered Release Images ("+fmt.Sprintf("%d", len(filteredReleaseImages))+"): ", "images", filteredReleaseImages)

	// ==========================================================================================
	// Create the Namespace for the operator workload components
	// ==========================================================================================
	operatorNamespace, err = r.createOperatorNamespace(ctx, upgradeAccelerator)
	if err != nil {
		logger.V(1).Error(err, "Failed to create Operator Namespace")
		_ = r.setConditionSetupNamespaceFailure(ctx, upgradeAccelerator, err.Error())
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return operatorNamespace, err
	}

	// ==========================================================================================
	// Create the Release Image ConfigMap
	// ==========================================================================================
	// Convert the releaseImages and filteredReleaseImages into their JSON structures to be passed as a string into the ConfigMap
	releaseImagesJSON, err := json.Marshal(releaseImages)
	if err != nil {
		logger.V(1).Error(err, "Failed to marshal releaseImages to JSON")
		_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return operatorNamespace, err
	}
	filteredReleaseImagesJSON, err := json.Marshal(filteredReleaseImages)
	if err != nil {
		logger.V(1).Error(err, "Failed to marshal filteredReleaseImages to JSON")
		_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return operatorNamespace, err
	}
	err = r.createReleaseConfigMap(ctx, upgradeAccelerator, string(releaseImagesJSON), string(filteredReleaseImagesJSON))
	if err != nil {
		logger.V(1).Error(err, "Failed to create Release ConfigMap")
		_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return operatorNamespace, err
	}

	// ==========================================================================================
	// Create Puller Script ConfigMap
	// ==========================================================================================
	if upgradeAccelerator.Spec.Config.PullScriptConfigMapName == "" {
		err = r.createPullerScriptConfigMap(ctx, upgradeAccelerator)
		if err != nil {
			logger.Error(err, "Failed to create Puller Script ConfigMap")
			_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
			_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
			_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
			return operatorNamespace, err
		}
	}

	return operatorNamespace, nil

}
