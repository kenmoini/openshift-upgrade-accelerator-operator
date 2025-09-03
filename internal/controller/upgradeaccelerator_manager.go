package controller

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
)

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAcceleratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openshiftv1alpha1.UpgradeAccelerator{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.Job{}).
		// TODO: Check about moving the predicates directly into the Watchers
		// https://book.kubebuilder.io/reference/watching-resources/predicates-with-watch
		// Watch for updates to the ClusterVersion and run a reconciliation for all UpgradeAccelerators
		Watches(&configv1.ClusterVersion{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				upgradeAcceleratorList := &openshiftv1alpha1.UpgradeAcceleratorList{}
				client := mgr.GetClient()

				err := client.List(context.TODO(), upgradeAcceleratorList)
				if err != nil {
					return []reconcile.Request{}
				}
				var reconcileRequests []reconcile.Request
				if _, ok := obj.(*configv1.ClusterVersion); ok {
					// Reconcile all UpgradeAccelerators
					for _, ua := range upgradeAcceleratorList.Items {
						rec := reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      ua.Name,
								Namespace: ua.Namespace,
							},
						}
						reconcileRequests = append(reconcileRequests, rec)
					}
				}
				return reconcileRequests
			}),
		).
		// Watch for updates to the ClusterOperators and run a reconciliation for all UpgradeAccelerators
		Watches(&configv1.ClusterOperator{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				upgradeAcceleratorList := &openshiftv1alpha1.UpgradeAcceleratorList{}
				client := mgr.GetClient()

				err := client.List(context.TODO(), upgradeAcceleratorList)
				if err != nil {
					return []reconcile.Request{}
				}
				var reconcileRequests []reconcile.Request
				if _, ok := obj.(*configv1.ClusterOperator); ok {
					// Reconcile all UpgradeAccelerators
					for _, ua := range upgradeAcceleratorList.Items {
						rec := reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      ua.Name,
								Namespace: ua.Namespace,
							},
						}
						reconcileRequests = append(reconcileRequests, rec)
					}
				}
				return reconcileRequests
			}),
		).
		// Filter watched events to check only some fields on relevant ClusterVersion updates
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				if _, ok := e.ObjectNew.(*configv1.ClusterVersion); ok {
					return hasClusterVersionChanged(
						e.ObjectOld.(*configv1.ClusterVersion),
						e.ObjectNew.(*configv1.ClusterVersion))
				}
				return true
			},
		}).
		// Filter watched events to check only some fields on relevant machine-config ClusterOperator updates
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				if _, ok := e.ObjectNew.(*configv1.ClusterOperator); ok {
					return hasMachineConfigClusterOperatorChanged(
						e.ObjectOld.(*configv1.ClusterOperator),
						e.ObjectNew.(*configv1.ClusterOperator))
				}
				return true
			},
		}).
		WithEventFilter(ignoreDeletionPredicate()).
		Named("upgradeaccelerator").
		Complete(r)
}
