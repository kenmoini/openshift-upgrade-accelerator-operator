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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"dario.cat/mergo"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	configv1 "github.com/openshift/api/config/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=proxies,verbs=get;list;watch
// +kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openshift.kemo.dev,resources=upgradeaccelerators/finalizers,verbs=update
// +kubebuilder:rbac:groups=security.openshift.io,namespace=openshift-upgrade-accelerator,resources=securitycontextconstraints,verbs=get;list;watch;use,resourceNames=privileged
// +kubebuilder:rbac:groups=core,namespace=openshift-upgrade-accelerator,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=openshift-upgrade-accelerator,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=openshift-upgrade-accelerator,resources=pods,verbs=get;list;watch;delete

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
		return ctrl.Result{RequeueAfter: time.Second * 30}, client.IgnoreNotFound(err)
	} else {
		logger.Info("Successfully fetched UpgradeAccelerator", "NamespacedName", req.NamespacedName)
		//logger.Info("Spec", "spec", upgradeAccelerator.Spec)
		// ============================================================================================
		// Base In-Reconciler Variables
		// ============================================================================================
		uaSpec := upgradeAccelerator.Spec

		validMachineConfigPools := []string{}
		validMachineConfigPoolSelectors := make(map[string]*metav1.LabelSelector)
		targetedNodes := []string{}
		primingProhibitedNodes := []string{}

		// Determine Job Puller Image
		jobPullerImage := UpgradeAcceleratorDefaultJobPullerImage
		if uaSpec.Config.JobImage != "" {
			jobPullerImage = uaSpec.Config.JobImage
		}

		// ============================================================================================
		// State Check: Check if this UpdateAccelerator is enabled or not
		// ============================================================================================
		if strings.ToLower(uaSpec.State) == "disabled" {
			logger.Info("UpgradeAccelerator is disabled, finished")
			return ctrl.Result{RequeueAfter: time.Second * 30}, nil
		} else {
			logger.Info("UpgradeAccelerator is enabled...")

			// ============================================================================================
			// Get preliminary assets
			// ============================================================================================
			// Cluster Infrastructure - Platform Type
			clusterInfrastructureType, err := getOpenShiftInfrastructureType(ctx, req, r.Client)
			if err != nil {
				logger.Error(err, "Failed to get OpenShift infrastructure type")
				_ = r.setConditionInfrastructureNotFound(ctx, upgradeAccelerator, err)
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			logger.Info("OpenShift Infrastructure Type", "type", clusterInfrastructureType)

			// Cluster Version State
			clusterVersionState, err := r.getClusterVersionState(ctx, upgradeAccelerator)
			if err != nil {
				logger.Error(err, "Failed to get Cluster Version State")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			logger.Info("Cluster Version State", "state", clusterVersionState)

			// Set the UpgradeAcceleratorStatus for the CurrentVersion and TargetVersion
			upgradeAccelerator.Status.CurrentVersion = clusterVersionState.CurrentVersion
			upgradeAccelerator.Status.TargetVersion = clusterVersionState.DesiredVersion
			err = r.Status().Update(ctx, upgradeAccelerator)
			if err != nil {
				logger.Error(err, "Failed to update UpgradeAccelerator status for versions")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// Check if the Current Version is the Desired Version - if so, no need to proceed
			// PROD: This is enabled in production deployments
			// Not helpful for debugging full flow but nice to not make the cluster do things it doesn't need to
			//if clusterVersionState.CurrentVersion == clusterVersionState.DesiredVersion {
			//	logger.Info("No upgrades in progress, no action required")
			//	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
			//}

			// ==========================================================================================
			// Node Determination
			// ==========================================================================================
			// Get the list of nodes from the cluster, first filtering by MachineConfigPools and then by nodeSelector
			nodes := &corev1.NodeList{}
			if err := r.List(ctx, nodes, client.InNamespace(req.Namespace)); err != nil {
				logger.Error(err, "Failed to list nodes")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
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
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
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
						// Make sure that the node does not have the openshift.kemo.dev/disable-preheat: "true" label
						excludeLabelValue, ok := node.Labels["openshift.kemo.dev/disable-preheat"]
						if ok && strings.ToLower(excludeLabelValue) == "true" {
							logger.Info("Node has openshift.kemo.dev/disable-preheat: \"true\" label, skipping", "node", node.Name)
							continue
						}
						// If this node has priming explicitly disabled, add it to the primingProhibitedNodes
						primingLabelValue, ok := node.Labels["openshift.kemo.dev/primer-node"]
						if ok && strings.ToLower(primingLabelValue) == "false" {
							primingProhibitedNodes = append(primingProhibitedNodes, node.Name)
						}
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
								// Make sure that the node does not have the openshift.kemo.dev/disable-preheat: "true" label
								excludeLabelValue, ok := node.Labels["openshift.kemo.dev/disable-preheat"]
								if ok && strings.ToLower(excludeLabelValue) == "true" {
									logger.Info("Node has openshift.kemo.dev/disable-preheat: \"true\" label, skipping", "node", node.Name)
									continue
								}
								// If this node has priming explicitly disabled, add it to the primingProhibitedNodes
								primingLabelValue, ok := node.Labels["openshift.kemo.dev/primer-node"]
								if ok && strings.ToLower(primingLabelValue) == "false" {
									primingProhibitedNodes = append(primingProhibitedNodes, node.Name)
								}
								targetedNodes = append(targetedNodes, node.Name)
							}
						}
					}
				}
			}

			// ==========================================================================================
			// Targeted Nodes State Determination
			// ==========================================================================================
			logger.Info("List of targetedNodes", "targetedNodes", targetedNodes)
			if len(targetedNodes) == 0 {
				logger.Info("No targeted nodes found")
				return ctrl.Result{RequeueAfter: time.Second * 30}, nil
			} else {
				upgradeAccelerator.Status.NodesSelected = targetedNodes
				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status for nodesSelected")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}

			// ==========================================================================================
			// Get the Release Images
			// ==========================================================================================
			releaseImages, err := GetReleaseImages(clusterVersionState.DesiredImage)
			if err != nil {
				logger.Error(err, "Failed to get Release Images")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			//logger.Info("Release Images ("+fmt.Sprintf("%d", len(releaseImages))+"): ", "images", releaseImages)

			// ==========================================================================================
			// Filter the target release images
			// ==========================================================================================
			filteredReleaseImages := FilterReleaseImages(releaseImages, clusterInfrastructureType)
			//logger.Info("Filtered Release Images ("+fmt.Sprintf("%d", len(filteredReleaseImages))+"): ", "images", filteredReleaseImages)

			// ==========================================================================================
			// Create the Namespace for the operator workload components
			// ==========================================================================================
			operatorNamespace, err := r.createOperatorNamespace(ctx, upgradeAccelerator)
			if err != nil {
				logger.Error(err, "Failed to create Operator Namespace")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// ==========================================================================================
			// Create the Release Image ConfigMap
			// ==========================================================================================
			// Convert the releaseImages and filteredReleaseImages into their JSON structures to be passed as a string into the ConfigMap
			releaseImagesJSON, err := json.Marshal(releaseImages)
			if err != nil {
				logger.Error(err, "Failed to marshal releaseImages to JSON")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			filteredReleaseImagesJSON, err := json.Marshal(filteredReleaseImages)
			if err != nil {
				logger.Error(err, "Failed to marshal filteredReleaseImages to JSON")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			err = r.createReleaseConfigMap(ctx, upgradeAccelerator, clusterVersionState.DesiredVersion, string(releaseImagesJSON), string(filteredReleaseImagesJSON))
			if err != nil {
				logger.Error(err, "Failed to create Release ConfigMap")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// ==========================================================================================
			// Create Puller Script ConfigMap
			// ==========================================================================================
			err = r.createPullerScriptConfigMap(ctx, upgradeAccelerator, operatorNamespace)
			if err != nil {
				logger.Error(err, "Failed to create Puller Script ConfigMap")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// ==========================================================================================
			// Determine priming function
			// ==========================================================================================
			primerNodes := []string{}
			if uaSpec.Prime {
				// Check if there are manually set primer nodes
				nodeList := &corev1.NodeList{}
				if err := r.List(ctx, nodeList, client.MatchingLabels(map[string]string{"openshift.kemo.dev/primer-node": "true"})); err != nil {
					logger.Error(err, "Failed to list nodes", "selector", uaSpec.Selector.NodeSelector)
				} else {
					for _, node := range nodeList.Items {
						primerNodes = append(primerNodes, node.Name)
					}
				}
				// If no manually set primer nodes, then just pick one from the targeted nodes if it's not part of the primingProhibitedNodes list
				for _, tNode := range targetedNodes {
					if !slices.Contains(primingProhibitedNodes, tNode) {
						primerNodes = append(primerNodes, tNode)
					}
				}
				upgradeAccelerator.Status.PrimerNodes = primerNodes
				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}
			logger.Info("Primer Nodes", "nodes", primerNodes)

			// ==========================================================================================
			// Determine Node List States
			// ==========================================================================================
			upgradeAcceleratorStatusChanged := false
			nodesWaiting := upgradeAccelerator.Status.NodesWaiting

			// Loop through the targetedNodes
			for _, tNode := range targetedNodes {
				// If the node is in the NodesWarming or NodesPreheated lists don't add it to the NodesWaiting list
				if !slices.Contains(upgradeAccelerator.Status.NodesWarming, tNode) && !slices.Contains(upgradeAccelerator.Status.NodesPreheated, tNode) && !slices.Contains(nodesWaiting, tNode) {
					nodesWaiting = append(nodesWaiting, tNode)
					upgradeAcceleratorStatusChanged = true
				}
			}

			// Populate the nodes Waiting list
			if upgradeAcceleratorStatusChanged {
				upgradeAccelerator.Status.NodesWaiting = nodesWaiting
				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}

			// ==========================================================================================
			// Primer Node Scheduling
			// ==========================================================================================
			if uaSpec.Prime && len(primerNodes) > 0 {
				// Loop through each node and schedule a Pod
				for _, pNode := range primerNodes {
					// Make sure the node isn't already warming
					if slices.Contains(upgradeAccelerator.Status.NodesWarming, pNode) {
						continue
					}
					// Make sure the node isn't already preheated
					if slices.Contains(upgradeAccelerator.Status.NodesPreheated, pNode) {
						continue
					}
					err = r.createPullJob(ctx, upgradeAccelerator, PullJob{
						Name:           fmt.Sprintf("%s-ua-puller-%s", pNode, hashName(upgradeAccelerator.Name)),
						Namespace:      operatorNamespace,
						ContainerImage: jobPullerImage,
						ConfigMapName:  fmt.Sprintf("release-%s", clusterVersionState.DesiredVersion),
						TargetNodeName: pNode,
						ReleaseVersion: clusterVersionState.DesiredVersion,
					})
					if err != nil {
						logger.Error(err, "Failed to create Pod Puller Job")
						return ctrl.Result{RequeueAfter: time.Second * 30}, err
					} else {
						// Pod has been scheduled successfully
						upgradeAccelerator.Status.NodesWarming = append(upgradeAccelerator.Status.NodesWarming, pNode)
						// Remove the node from the NodesWaiting list
						upgradeAccelerator.Status.NodesWaiting = append(upgradeAccelerator.Status.NodesWaiting[:slices.Index(upgradeAccelerator.Status.NodesWaiting, pNode)], upgradeAccelerator.Status.NodesWaiting[slices.Index(upgradeAccelerator.Status.NodesWaiting, pNode)+1:]...)
						err = r.Status().Update(ctx, upgradeAccelerator)
						if err != nil {
							logger.Error(err, "Failed to update UpgradeAccelerator status")
							return ctrl.Result{RequeueAfter: time.Second * 30}, err
						}
					}
				}
			}

			// ==========================================================================================
			// Job Completion Scan
			// ==========================================================================================
			// Get all the Jobs in the namespace that match the label for this release
			jobs := &batchv1.JobList{}
			err = r.List(ctx, jobs, client.InNamespace(operatorNamespace), client.MatchingLabels{"pull-job/release": clusterVersionState.DesiredVersion})
			if err != nil {
				logger.Error(err, "Failed to list Jobs")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			} else {
				if len(jobs.Items) == 0 {
					// No Jobs?!!?!
					// Check if there are any NodesWarming - these should be reset and rescheduled by removing them from the status list
					if len(upgradeAccelerator.Status.NodesWarming) > 0 {
						upgradeAccelerator.Status.NodesWaiting = append(upgradeAccelerator.Status.NodesWaiting, upgradeAccelerator.Status.NodesWarming...)
						upgradeAccelerator.Status.NodesWarming = nil
						err = r.Status().Update(ctx, upgradeAccelerator)
						if err != nil {
							logger.Error(err, "Failed to update UpgradeAccelerator status")
							return ctrl.Result{RequeueAfter: time.Second * 30}, err
						}
					}
				} else {
					// Loop through the Jobs and check for any completed Jobs
					// If a job is completed successfully then move the Node it is running on into the NodesPreheated list and remove it from the NodesWaiting list
					for _, job := range jobs.Items {
						if job.Status.Succeeded > 0 {
							// Find the node the job is running on
							nodeName := job.Spec.Template.Spec.NodeName
							upgradeAcceleratorStatusChanged = false
							// Move the node to the NodesPreheated list if it is not already in it
							if !slices.Contains(upgradeAccelerator.Status.NodesPreheated, nodeName) {
								upgradeAccelerator.Status.NodesPreheated = append(upgradeAccelerator.Status.NodesPreheated, nodeName)
								upgradeAcceleratorStatusChanged = true
							}
							// Remove the node from the NodesWarming list if it exists
							if slices.Contains(upgradeAccelerator.Status.NodesWarming, nodeName) {
								upgradeAccelerator.Status.NodesWarming = append(upgradeAccelerator.Status.NodesWarming[:slices.Index(upgradeAccelerator.Status.NodesWarming, nodeName)], upgradeAccelerator.Status.NodesWarming[slices.Index(upgradeAccelerator.Status.NodesWarming, nodeName)+1:]...)
								upgradeAcceleratorStatusChanged = true
							}
							if upgradeAcceleratorStatusChanged {
								err = r.Status().Update(ctx, upgradeAccelerator)
								if err != nil {
									logger.Error(err, "Failed to update UpgradeAccelerator status")
									return ctrl.Result{RequeueAfter: time.Second * 30}, err
								}
							}
						}
					}
				}
			}

			// Check if all the nodes have been completed - if so return success
			if len(upgradeAccelerator.Status.NodesWaiting) == 0 && len(upgradeAccelerator.Status.NodesWarming) == 0 {
				if len(upgradeAccelerator.Status.NodesPreheated) == len(upgradeAccelerator.Status.NodesSelected) {
					// All nodes have been preheated successfully
					logger.Info("All nodes have been preheated successfully")
				}
			}
		}
	}

	logger.Info("Upgrade Accelerator reconciliation completed")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpgradeAcceleratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openshiftv1alpha1.UpgradeAccelerator{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.Job{}).
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
		Named("upgradeaccelerator").
		Complete(r)
}
