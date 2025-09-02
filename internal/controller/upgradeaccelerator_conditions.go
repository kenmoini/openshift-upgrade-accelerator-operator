package controller

import (
	"context"
	"fmt"
	"time"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	CONDITION_STATUS_TRUE  = "True"
	CONDITION_STATUS_FALSE = "False"

	CONDITION_TYPE_FAILURE              = "Failure"
	CONDITION_TYPE_SUCCESSFUL           = "Successful"
	CONDITION_TYPE_COMPLETED            = "Completed"
	CONDITION_TYPE_RUNNING              = "Running"
	CONDITION_TYPE_PRIMER               = "Primer"
	CONDITION_TYPE_PRIMING_IN_PROGRESS  = "PrimingInProgress"
	CONDITION_TYPE_INFRASTRUCTURE_FOUND = "InfrastructureFound"
	CONDITION_TYPE_SETUP_COMPLETE       = "SetupComplete"

	CONDITION_REASON_FAILURE_GET_RELEASE_IMAGES     = "GetReleaseImagesFailed"
	CONDITION_REASON_FAILURE_GET_CLUSTER_VERSION    = "GetClusterVersionFailed"
	CONDITION_REASON_FAILURE_GET_MACHINECONFIGPOOLS = "GetMachineConfigPoolsFailed"
	CONDITION_REASON_FAILURE_GET_NODES              = "GetNodesFailed"
	CONDITION_REASON_FAILURE_GET_INFRASTRUCTURE     = "GetInfrastructureFailed"
	CONDITION_REASON_FAILURE_SETUP                  = "SetupFailed"

	CONDITION_REASON_RUNNING_DISABLED                 = "UpdateAcceleratorDisabled"
	CONDITION_REASON_RUNNING_NO_NODES_SELECTED        = "NoNodesSelected"
	CONDITION_REASON_RUNNING_JOBS_SCHEDULED           = "JobsScheduled"
	CONDITION_REASON_RUNNING_RECONCILIATION_COMPLETED = "ReconciliationCompleted"

	CONDITION_REASON_PRIMER_COMPLETED   = "PrimingCompleted"
	CONDITION_REASON_PRIMER_SKIPPED     = "PrimingSkipped"
	CONDITION_REASON_PRIMER_IN_PROGRESS = "PrimingInProgress"

	CONDITION_REASON_INFRASTRUCTURE_FOUND     = "InfrastructureFound"
	CONDITION_REASON_INFRASTRUCTURE_NOT_FOUND = "InfrastructureNotFound"

	CONDITION_REASON_SETUP_COMPLETED         = "AllSetupComplete"
	CONDITION_REASON_SETUP_NAMESPACE_FAILURE = "NamespaceFailure"
	CONDITION_REASON_SETUP_CONFIGMAP_FAILURE = "ConfigMapFailure"

	CONDITION_MESSAGE_INFRASTRUCTURE_FOUND = "Platform Type: %s"

	CONDITION_MESSAGE_COMPLETED = "All targeted nodes have completed"
)

// appendCondition appends a condition to the UpgradeAccelerator status.
func (reconciler *UpgradeAcceleratorReconciler) appendCondition(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	typeName string, status metav1.ConditionStatus, reason string, message string) error {

	currentTime := metav1.Time{Time: time.Now()}
	condition := metav1.Condition{Type: typeName, Status: status, Reason: reason, Message: message, LastTransitionTime: currentTime}
	// Check if the condition exists on the current upgradeAccelerator
	// If so update it
	// If not then append it
	conditionFound := false
	for i, cond := range upgradeAccelerator.Status.Conditions {
		if cond.Type == typeName {
			// Check to see if the contents are actually changed
			if cond.Status != status || cond.Reason != reason || cond.Message != message {
				upgradeAccelerator.Status.Conditions[i] = condition
			}
			conditionFound = true
			break
		}
	}
	if !conditionFound {
		upgradeAccelerator.Status.Conditions = append(upgradeAccelerator.Status.Conditions, condition)
	}

	return reconciler.Client.Status().Update(ctx, upgradeAccelerator)
}
func (reconciler *UpgradeAcceleratorReconciler) deleteConditions(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	typeNames []string) (err error) {

	for _, typeName := range typeNames {
		err = reconciler.deleteCondition(ctx, upgradeAccelerator, typeName)
	}
	return err
}

func (reconciler *UpgradeAcceleratorReconciler) deleteCondition(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	typeName string) error {

	log := logf.FromContext(ctx)
	var newConditions = make([]metav1.Condition, 0)
	for _, condition := range upgradeAccelerator.Status.Conditions {
		if condition.Type != typeName {
			newConditions = append(newConditions, condition)
		}
	}
	upgradeAccelerator.Status.Conditions = newConditions

	err := reconciler.Client.Status().Update(ctx, upgradeAccelerator)
	if err != nil {
		log.Info("UpgradeAccelerator resource status update failed.")
	}
	return err
}

// func (reconciler *UpgradeAcceleratorReconciler) containsCondition(ctx context.Context,
//	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, typeName string) bool {
//
//	output := false
//	for _, condition := range upgradeAccelerator.Status.Conditions {
//		if condition.Type == typeName {
//			output = true
//		}
//	}
//	return output
// }

func (reconciler *UpgradeAcceleratorReconciler) getConditionReason(upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, typeName string) string {

	for _, condition := range upgradeAccelerator.Status.Conditions {
		if condition.Type == typeName {
			return condition.Reason
		}
	}
	return ""
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionFailure(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, reason string, errorMessage string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_FAILURE, CONDITION_STATUS_TRUE,
		reason, errorMessage)
}

// ==========================================================================================================================
// Infrastructure
func (reconciler *UpgradeAcceleratorReconciler) setConditionInfrastructureFound(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, infrastructureType string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_INFRASTRUCTURE_FOUND, CONDITION_STATUS_TRUE,
		CONDITION_REASON_INFRASTRUCTURE_FOUND, fmt.Sprintf(CONDITION_MESSAGE_INFRASTRUCTURE_FOUND, infrastructureType))
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionInfrastructureNotFound(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, inErr error) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_INFRASTRUCTURE_FOUND, CONDITION_STATUS_FALSE,
		CONDITION_REASON_INFRASTRUCTURE_NOT_FOUND, inErr.Error())
}

// ==========================================================================================================================
// Completed
func (reconciler *UpgradeAcceleratorReconciler) setConditionCompleted(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {

	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_COMPLETED, CONDITION_STATUS_TRUE,
		CONDITION_REASON_RUNNING_RECONCILIATION_COMPLETED, message)
}

// ==========================================================================================================================
// Running
func (reconciler *UpgradeAcceleratorReconciler) setConditionRunning(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, reason string, message string) error {

	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_RUNNING, CONDITION_STATUS_TRUE,
		reason, message)
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionNotRunning(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, reason string, message string) error {

	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_RUNNING, CONDITION_STATUS_FALSE,
		reason, message)
}

// ==========================================================================================================================
// Primer
func (reconciler *UpgradeAcceleratorReconciler) setConditionPrimerCompleted(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_PRIMER, CONDITION_STATUS_TRUE,
		CONDITION_REASON_PRIMER_COMPLETED, message)
}
func (reconciler *UpgradeAcceleratorReconciler) setConditionPrimerInProgress(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_PRIMER, CONDITION_STATUS_TRUE,
		CONDITION_REASON_PRIMER_IN_PROGRESS, message)
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionPrimerSkipped(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_PRIMER, CONDITION_STATUS_FALSE,
		CONDITION_REASON_PRIMER_SKIPPED, message)
}

// ==========================================================================================================================
// SetupComplete

func (reconciler *UpgradeAcceleratorReconciler) setConditionSetupComplete(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_SETUP_COMPLETE, CONDITION_STATUS_TRUE,
		CONDITION_REASON_SETUP_COMPLETED, "All setup tasks completed successfully")
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionSetupNamespaceFailure(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_SETUP_COMPLETE, CONDITION_STATUS_FALSE,
		CONDITION_REASON_SETUP_NAMESPACE_FAILURE, message)
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionSetupConfigMapFailure(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, message string) error {
	return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_SETUP_COMPLETE, CONDITION_STATUS_FALSE,
		CONDITION_REASON_SETUP_CONFIGMAP_FAILURE, message)
}
