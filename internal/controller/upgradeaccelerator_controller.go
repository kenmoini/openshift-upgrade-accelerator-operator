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
	"fmt"
	"slices"
	"strings"
	"time"

	//configv1 "github.com/openshift/api/config/v1"
	batchv1 "k8s.io/api/batch/v1"
	//corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	//"sigs.k8s.io/controller-runtime/pkg/handler"

	// logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	//"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

func ignoreDeletionPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore updates to CR status in which case metadata.Generation does not change
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

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
	// logger := logf.FromContext(ctx)
	globalLog = ctrl.Log.WithName("upgrade-accelerator")

	upgradeAccelerator := &openshiftv1alpha1.UpgradeAccelerator{}
	globalLog.Info("Reconciling UpgradeAccelerator", "NamespacedName", req.NamespacedName)
	if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
		return ctrl.Result{RequeueAfter: time.Second * 30}, client.IgnoreNotFound(err)
	} else {
		globalLog.Info("Successfully fetched UpgradeAccelerator", "NamespacedName", req.NamespacedName)
		logger := ctrl.Log.WithName(upgradeAccelerator.Name)
		// ============================================================================================
		// Base In-Reconciler Variables
		// ============================================================================================

		// ============================================================================================
		// State Check: Check if this UpdateAccelerator is enabled or not
		// ============================================================================================
		if strings.ToLower(upgradeAccelerator.Spec.State) == "disabled" {
			logger.Info("UpgradeAccelerator is disabled, finished")
			upgradeAccelerator.Status.NodesPreheated = nil
			upgradeAccelerator.Status.NodesSelected = nil
			upgradeAccelerator.Status.NodesWaiting = nil
			upgradeAccelerator.Status.NodesWarming = nil
			upgradeAccelerator.Status.PrimerNodes = nil
			_ = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_DISABLED, "UpgradeAccelerator is disabled")
			_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_FAILURE, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_INFRASTRUCTURE_FOUND, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
			return ctrl.Result{RequeueAfter: time.Second * 30}, nil
		} else {
			logger.Info("UpgradeAccelerator is enabled...")

			// ============================================================================================
			// Get preliminary assets
			// ============================================================================================
			// Cluster Infrastructure - Platform Type
			clusterInfrastructureType, err := getOpenShiftInfrastructureType(ctx, r.Client)
			if err != nil {
				logger.Error(err, "Failed to get OpenShift infrastructure type")
				_ = r.setConditionInfrastructureNotFound(ctx, upgradeAccelerator, err)
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_INFRASTRUCTURE, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			logger.Info("OpenShift Infrastructure Type: " + clusterInfrastructureType)
			// Update/Refetch the UpgradeAccelerator
			if upgradeAccelerator.Status.ClusterInfrastructure != clusterInfrastructureType {
				upgradeAccelerator.Status.ClusterInfrastructure = clusterInfrastructureType
				_ = r.setConditionInfrastructureFound(ctx, upgradeAccelerator, clusterInfrastructureType)

				if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
					logger.Error(err, "Failed to re-fetch UpgradeAccelerator after nodesSelected")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}

			// ==========================================================================================
			// Cluster Version State Determination
			clusterVersionState, changed, err := r.setupClusterVersion(ctx, upgradeAccelerator, &logger)
			if err != nil {
				logger.Error(err, "Failed to setup Cluster Version")
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			if changed {
				// Refetch the UpgradeAccelerator
				if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
					logger.Error(err, "Failed to re-fetch UpgradeAccelerator after nodesSelected")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}

			// ==========================================================================================
			// Targeted Nodes State Determination
			targetedNodes, prohibitedNodes, primerNodes, primingProhibitedNodes, err := r.determineTargetedNodes(ctx, upgradeAccelerator, &logger)
			if err != nil {
				logger.Error(err, "Failed to determine targeted nodes")
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			logger.V(1).Info("List of final targetedNodes", "targetedNodes", targetedNodes)
			logger.V(1).Info("List of final primerNodes", "primerNodes", primerNodes)
			logger.V(1).Info("List of final prohibitedNodes", "prohibitedNodes", prohibitedNodes)
			logger.V(1).Info("List of final primingProhibitedNodes", "primingProhibitedNodes", primingProhibitedNodes)

			// ==========================================================================================
			// Initial State Checks
			// If there are no targeted nodes, we do nothing, just reset
			if len(targetedNodes) == 0 {
				logger.Info("No targeted nodes found, exiting")
				upgradeAccelerator.Status.NodesSelected = nil
				upgradeAccelerator.Status.NodesPreheated = nil
				upgradeAccelerator.Status.NodesWaiting = nil
				upgradeAccelerator.Status.NodesWarming = nil
				upgradeAccelerator.Status.PrimerNodes = nil
				_ = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_NO_NODES_SELECTED, "No targeted nodes found")
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, nil
			} else {
				// There are targeted nodes!
				// Update some statuses and conditions
				// ========================================================================================
				// Wholesale apply some determined node lists
				upgradeAccelerator.Status.NodesSelected = targetedNodes
				upgradeAccelerator.Status.PrimerNodes = primerNodes

				// Primer Checks
				if len(primerNodes) == 0 {
					_ = r.setConditionPrimerSkipped(ctx, upgradeAccelerator, "Priming effectively disabled")
				} else {
					// Loop through all the primer nodes and check if they're in the NodesPreheated list
					// If all of them are we are done priming
					// If any of them are not then priming is still in progress - but this is condition is set later when we're scheduling things
					stillWaitingOnPrimerNodes := false
					for _, pn := range primerNodes {
						if !slices.Contains(upgradeAccelerator.Status.NodesPreheated, pn) {
							stillWaitingOnPrimerNodes = true
							break
						}
					}
					if !stillWaitingOnPrimerNodes {
						_ = r.setConditionPrimerCompleted(ctx, upgradeAccelerator, "All primer nodes are preheated")
					}
				}

				// Completion checks
				// Compare the targetNodes and the completed nodes, check if there are no waiting nodes
				// If they are the same, then exit early
				// If the lists are different, then reset the completed condition
				// TODO: BUG - Technically the number of nodes selected and the nodes completed could be the same - with different contents
				// Come back and fix this, deep copy reflection thing?
				if len(upgradeAccelerator.Status.NodesSelected) == len(upgradeAccelerator.Status.NodesPreheated) && len(upgradeAccelerator.Status.NodesWaiting) == 0 {
					logger.Info(CONDITION_MESSAGE_COMPLETED)
					_ = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_RECONCILIATION_COMPLETED, CONDITION_MESSAGE_COMPLETED)
					_ = r.setConditionCompleted(ctx, upgradeAccelerator, CONDITION_MESSAGE_COMPLETED)
					return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
				}

				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status for nodesSelected")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
				// Refetch the UpgradeAccelerator
				if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
					logger.Error(err, "Failed to re-fetch UpgradeAccelerator after nodesSelected")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}

			// ==========================================================================================
			// Setup Resources
			// ==========================================================================================
			operatorNamespace, err := r.setupResources(ctx, upgradeAccelerator, clusterVersionState, &logger)
			if err != nil {
				logger.Error(err, "Failed to setup resources")
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			// Conditional Gate: SetupComplete
			_ = r.setConditionSetupComplete(ctx, upgradeAccelerator)
			// TODO: Check for the Failure type, clear any previous failures

			// ==========================================================================================
			// Determine Node List States
			// Tip: Exit as early as possible from determined end states
			// ==========================================================================================
			upgradeAcceleratorStatusChanged := false

			if upgradeAcceleratorStatusChanged {
				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
				// Refetch the UpgradeAccelerator
				if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
					logger.Error(err, "Failed to re-fetch UpgradeAccelerator")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			} else {

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
				// Loop through the selected primer nodes
				// Check if they're in the nodesWaiting list
				// Check if they're already warming or preheated
				// ==========================================================================================
				if upgradeAccelerator.Spec.Prime && len(primerNodes) > 0 {
					// Loop through each node and schedule a Pod
					primerJobScheduled := false
					primerJobStillRunning := false
					for _, pNode := range primerNodes {
						// Make sure the node isn't already warming (running)
						if slices.Contains(upgradeAccelerator.Status.NodesWarming, pNode) {
							primerJobStillRunning = true
						}
						// Make sure the node isn't already preheated
						if !slices.Contains(upgradeAccelerator.Status.NodesWarming, pNode) || !slices.Contains(upgradeAccelerator.Status.NodesPreheated, pNode) {
							createdJob, err := r.createPullJob(ctx, upgradeAccelerator, PullJob{
								Name:           fmt.Sprintf("%s-ua-puller-%s", pNode, hashString(upgradeAccelerator.Status.DesiredVersion)),
								Namespace:      operatorNamespace,
								TargetNodeName: pNode,
							})
							if err != nil {
								logger.Error(err, "Failed to create Pod Puller Job")
								return ctrl.Result{RequeueAfter: time.Second * 30}, err
							} else {
								// Pod has been scheduled successfully
								if createdJob {
									primerJobScheduled = true
									upgradeAccelerator.Status.NodesWarming = append(upgradeAccelerator.Status.NodesWarming, pNode)
									// Remove the node from the NodesWaiting list
									// If upgradeAccelerator.Status.NodesWaiting is only one element, we can reset it
									if len(upgradeAccelerator.Status.NodesWaiting) == 1 && slices.Contains(upgradeAccelerator.Status.NodesWaiting, pNode) {
										upgradeAccelerator.Status.NodesWaiting = []string{}
									} else if len(upgradeAccelerator.Status.NodesWaiting) > 1 {
										upgradeAccelerator.Status.NodesWaiting = append(upgradeAccelerator.Status.NodesWaiting[:slices.Index(upgradeAccelerator.Status.NodesWaiting, pNode)], upgradeAccelerator.Status.NodesWaiting[slices.Index(upgradeAccelerator.Status.NodesWaiting, pNode)+1:]...)
									}
									err = r.Status().Update(ctx, upgradeAccelerator)
									if err != nil {
										logger.Error(err, "Failed to update UpgradeAccelerator status")
										return ctrl.Result{RequeueAfter: time.Second * 30}, err
									}
								}
							}
						}
					}
					if primerJobScheduled {
						_ = r.setConditionPrimerInProgress(ctx, upgradeAccelerator, "Primer nodes warming in progress: "+strings.Join(primerNodes, ", "))
					}
					if primerJobStillRunning {
						_ = r.setConditionRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_JOBS_SCHEDULED, "Following primer nodes currently warming: "+strings.Join(primerNodes, ", "))
					}
					if !primerJobStillRunning && !primerJobScheduled {
						_ = r.setConditionPrimerCompleted(ctx, upgradeAccelerator, "Primer nodes warming completed: "+strings.Join(primerNodes, ", "))
					}
				}

				// ==========================================================================================
				// Primer State Check
				// ==========================================================================================
				// If the priming is either disabled or we're done priming, continue to rotate through the remaining nodes
				primerCondition := r.getConditionReason(upgradeAccelerator, CONDITION_TYPE_PRIMER)
				if primerCondition == CONDITION_REASON_PRIMER_SKIPPED || primerCondition == CONDITION_REASON_PRIMER_COMPLETED {
					// Get the next set of nodes to process
					// If parallelism is set to 0, then just process all the remaining nodes
					nextNodesToProcess := []string{}
					if len(upgradeAccelerator.Status.NodesWaiting) > 0 {
						if upgradeAccelerator.Spec.Parallelism == 0 {
							// Process all remaining nodes
							nextNodesToProcess = upgradeAccelerator.Status.NodesWaiting
						} else {
							// Process nodes in batches
							for i := 0; i < int(upgradeAccelerator.Spec.Parallelism); i++ {
								nextNodesToProcess = append(nextNodesToProcess, upgradeAccelerator.Status.NodesWaiting[0])
								upgradeAccelerator.Status.NodesWaiting = upgradeAccelerator.Status.NodesWaiting[1:]
							}
						}
					}
					// Loop through each node and schedule a Pod
					for _, pNode := range nextNodesToProcess {
						// Make sure the node isn't already warming
						// Make sure the node isn't already preheated
						if !slices.Contains(upgradeAccelerator.Status.NodesWarming, pNode) || !slices.Contains(upgradeAccelerator.Status.NodesPreheated, pNode) {
							createdJob, err := r.createPullJob(ctx, upgradeAccelerator, PullJob{
								Name:           fmt.Sprintf("%s-ua-puller-%s", pNode, hashString(upgradeAccelerator.Status.DesiredVersion)),
								Namespace:      operatorNamespace,
								TargetNodeName: pNode,
							})
							if err != nil {
								logger.Error(err, "Failed to create Pod Puller Job")
								return ctrl.Result{RequeueAfter: time.Second * 30}, err
							} else {
								if createdJob {
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
					}
					_ = r.setConditionRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_JOBS_SCHEDULED, "Following nodes currently warming: "+strings.Join(nextNodesToProcess, ", "))
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
						err = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_RECONCILIATION_COMPLETED, CONDITION_MESSAGE_COMPLETED)
						if err != nil {
							logger.Error(err, "Failed to set UpgradeAccelerator condition")
							return ctrl.Result{RequeueAfter: time.Second * 30}, err
						}
						upgradeAccelerator.Status.LastCompletedVersion = upgradeAccelerator.Status.TargetVersion
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

	globalLog.Info("Upgrade Accelerator reconciliation completed")
	return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
}
