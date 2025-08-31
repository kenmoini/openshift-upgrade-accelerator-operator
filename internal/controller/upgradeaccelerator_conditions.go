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
	CONDITION_STATUS_TRUE                     = "True"
	CONDITION_STATUS_FALSE                    = "False"
	CONDITION_TYPE_INFRASTRUCTURE_FOUND       = "InfrastructureFound"
	CONDITION_REASON_INFRASTRUCTURE_FOUND     = "InfrastructureFound"
	CONDITION_REASON_INFRASTRUCTURE_NOT_FOUND = "InfrastructureNotFound"
	CONDITION_MESSAGE_INFRASTRUCTURE_FOUND    = "Platform Type: %s"
)

// appendCondition appends a condition to the UpgradeAccelerator status.
func (reconciler *UpgradeAcceleratorReconciler) appendCondition(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	typeName string, status metav1.ConditionStatus, reason string, message string) error {

	log := logf.FromContext(ctx)
	time := metav1.Time{Time: time.Now()}
	condition := metav1.Condition{Type: typeName, Status: status, Reason: reason, Message: message, LastTransitionTime: time}
	upgradeAccelerator.Status.Conditions = append(upgradeAccelerator.Status.Conditions, condition)

	err := reconciler.Client.Status().Update(ctx, upgradeAccelerator)
	if err != nil {
		log.Info("UpgradeAccelerator resource status update failed.")
	}
	return nil
}

func (reconciler *UpgradeAcceleratorReconciler) deleteCondition(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator,
	typeName string, reason string) error {

	log := logf.FromContext(ctx)
	var newConditions = make([]metav1.Condition, 0)
	for _, condition := range upgradeAccelerator.Status.Conditions {
		if condition.Type != typeName && condition.Reason != reason {
			newConditions = append(newConditions, condition)
		}
	}
	upgradeAccelerator.Status.Conditions = newConditions

	err := reconciler.Client.Status().Update(ctx, upgradeAccelerator)
	if err != nil {
		log.Info("UpgradeAccelerator resource status update failed.")
	}
	return nil
}

func (reconciler *UpgradeAcceleratorReconciler) containsCondition(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, reason string) bool {

	output := false
	for _, condition := range upgradeAccelerator.Status.Conditions {
		if condition.Reason == reason {
			output = true
		}
	}
	return output
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionInfrastructureFound(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, infrastructureType string) error {
	if !reconciler.containsCondition(ctx, upgradeAccelerator, CONDITION_REASON_INFRASTRUCTURE_FOUND) {
		return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_INFRASTRUCTURE_FOUND, CONDITION_STATUS_TRUE,
			CONDITION_REASON_INFRASTRUCTURE_FOUND, fmt.Sprintf(CONDITION_MESSAGE_INFRASTRUCTURE_FOUND, infrastructureType))
	}
	return nil
}

func (reconciler *UpgradeAcceleratorReconciler) setConditionInfrastructureNotFound(ctx context.Context,
	upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, inErr error) error {
	if !reconciler.containsCondition(ctx, upgradeAccelerator, CONDITION_REASON_INFRASTRUCTURE_FOUND) {
		return reconciler.appendCondition(ctx, upgradeAccelerator, CONDITION_TYPE_INFRASTRUCTURE_FOUND, CONDITION_STATUS_FALSE,
			CONDITION_REASON_INFRASTRUCTURE_NOT_FOUND, inErr.Error())
	}
	return nil
}
