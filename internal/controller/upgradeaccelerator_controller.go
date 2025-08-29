/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"slices"

	"dario.cat/mergo"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
)

// UpgradeAcceleratorReconciler reconciles a UpgradeAccelerator object
type UpgradeAcceleratorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the UpgradeAccelerator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *UpgradeAcceleratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	upgradeAccelerator := &openshiftv1alpha1.UpgradeAccelerator{}
	logger.Info("Reconciling UpgradeAccelerator", "NamespacedName", req.NamespacedName)
	if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else {
		logger.Info("Successfully fetched UpgradeAccelerator", "NamespacedName", req.NamespacedName)
		logger.Info("Spec", "spec", upgradeAccelerator.Spec)
		uaSpec := upgradeAccelerator.Spec

		validMachineConfigPools := []string{}
		validMachineConfigPoolSelectors := make(map[string]*metav1.LabelSelector)
		targetedNodes := []string{}

		// Check if this is enabled or not
		if uaSpec.State == "disabled" {
			logger.Info("UpgradeAccelerator is disabled, finished")
			return ctrl.Result{}, nil
		} else {
			logger.Info("UpgradeAccelerator is enabled, determining list of Nodes...")

			// Get the list of nodes from the cluster, first filtering by MachineConfigPools and then by nodeSelector
			nodes := &corev1.NodeList{}
			if err := r.List(ctx, nodes, client.InNamespace(req.Namespace)); err != nil {
				logger.Error(err, "Failed to list nodes")
				return ctrl.Result{}, err
			} else {
				logger.Info("Successfully listed nodes", "count", len(nodes.Items))
			}

			// If there is no selector defined, then all nodes are targeted
			if uaSpec.Selector.NodeSelector == nil && len(uaSpec.Selector.MachineConfigPools) == 0 {
				logger.Info("No selectors defined, targeting all nodes")
				for _, node := range nodes.Items {
					targetedNodes = append(targetedNodes, node.Name)
				}
			}

			// ==========================================================================================
			// Check if the spec has a machineConfigPool selector defined
			if len(uaSpec.Selector.MachineConfigPools) > 0 {
				logger.Info("MachineConfigPools selector found", "pools", uaSpec.Selector.MachineConfigPools)
				// Get the list of MachineConfigPools
				listOpts := []client.ListOption{}
				machineConfigPools := &machineconfigv1.MachineConfigPoolList{}
				if err := r.List(ctx, machineConfigPools, listOpts...); err != nil {
					logger.Error(err, "Failed to list MachineConfigPools")
					return ctrl.Result{}, err
				} else {
					logger.Info("Successfully listed MachineConfigPools", "count", len(machineConfigPools.Items))
				}
				// Extract the names from the machineConfigPools and set as a list of strings
				for _, pool := range machineConfigPools.Items {
					validMachineConfigPools = append(validMachineConfigPools, pool.Name)
				}

				// Check if the MachineConfigPool defined is valid
				for _, pool := range uaSpec.Selector.MachineConfigPools {
					if !slices.Contains(validMachineConfigPools, pool) {
						logger.Error(err, "Invalid MachineConfigPool: "+pool, "pool", pool)
					} else {
						logger.Info("Valid MachineConfigPool: "+pool, "pool", pool)
						validMachineConfigPools = append(validMachineConfigPools, pool)
						// Get the NodeSelector for the MachineConfigPool
						machineConfigPool := &machineconfigv1.MachineConfigPool{}
						if err := r.Get(ctx, client.ObjectKey{Name: pool}, machineConfigPool); err != nil {
							logger.Error(err, "Failed to get MachineConfigPool", "pool", pool)
						} else {
							validMachineConfigPoolSelectors[pool] = machineConfigPool.Spec.NodeSelector
						}
					}
				}

				// If no valid MachineConfigPools were found, return an error
				if len(validMachineConfigPools) == 0 {
					logger.Error(err, "No valid MachineConfigPools found - not used for filtering/selection")
				}
			}

			// ==========================================================================================
			// Check if we have any existing validMachineConfigPoolSelectors
			if len(validMachineConfigPoolSelectors) > 0 {
				logger.Info("Valid MachineConfigPoolSelectors found", "selectors", validMachineConfigPoolSelectors)
			}

			// ==========================================================================================
			// Check if we have NodeSelectors applied on this UpgradeAccelerator
			if uaSpec.Selector.NodeSelector != nil {
				logger.Info("NodeSelector found", "selector", uaSpec.Selector.NodeSelector)
			}

			// ==========================================================================================
			// Merge the NodeSelector into the validMachineConfigPoolSelectors
			if uaSpec.Selector.NodeSelector != nil && len(validMachineConfigPoolSelectors) > 0 {
				// Loop through each validMachineConfigPoolSelector and add the nodeSelector to it
				for pool, selector := range validMachineConfigPoolSelectors {
					if selector != nil {
						if err := mergo.Merge(selector, uaSpec.Selector.NodeSelector); err != nil {
							logger.Error(err, "Failed to merge NodeSelector into MachineConfigPoolSelector", "pool", pool)
						}
					}
				}
				logger.Info("Merged NodeSelector into MachineConfigPoolSelectors", "selectors", validMachineConfigPoolSelectors)
			}

			// Next, assemble the list of targetedNodes
			// If there are nodeSelectors but not machineConfigPools defined, simply filter by the nodeSelectors
			if uaSpec.Selector.NodeSelector != nil && len(validMachineConfigPoolSelectors) == 0 {
				// Get the list of nodes that match the nodeSelector
				nodeList := &corev1.NodeList{}
				if err := r.List(ctx, nodeList, client.MatchingLabels(uaSpec.Selector.NodeSelector.MatchLabels)); err != nil {
					logger.Error(err, "Failed to list nodes", "selector", uaSpec.Selector.NodeSelector)
				} else {
					for _, node := range nodeList.Items {
						targetedNodes = append(targetedNodes, node.Name)
					}
				}
			}
			// If both nodeSelectors and machineConfigPools are defined, then loop through the validMachineConfigPoolSelectors and assemble the targetedNodes
			if len(validMachineConfigPoolSelectors) > 0 {
				for _, selector := range validMachineConfigPoolSelectors {
					if selector != nil {
						// Get the list of nodes that match the MachineConfigPool's NodeSelector
						nodeList := &corev1.NodeList{}
						if err := r.List(ctx, nodeList, client.MatchingLabels(selector.MatchLabels)); err != nil {
							logger.Error(err, "Failed to list nodes", "selector", selector)
						} else {
							for _, node := range nodeList.Items {
								targetedNodes = append(targetedNodes, node.Name)
							}
						}
					}
				}
			}

			logger.Info("List of targetedNodes", "targetedNodes", targetedNodes)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAcceleratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openshiftv1alpha1.UpgradeAccelerator{}).
		Named("upgradeaccelerator").
		Complete(r)
}
