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

	configv1 "github.com/openshift/api/config/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	//logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	//logger := logf.FromContext(ctx)
	globalLog = ctrl.Log.WithName("upgrade-accelerator")

	upgradeAccelerator := &openshiftv1alpha1.UpgradeAccelerator{}
	globalLog.Info("Reconciling UpgradeAccelerator", "NamespacedName", req.NamespacedName)
	if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
		return ctrl.Result{RequeueAfter: time.Second * 30}, client.IgnoreNotFound(err)
	} else {
		globalLog.Info("Successfully fetched UpgradeAccelerator", "NamespacedName", req.NamespacedName)
		logger := ctrl.Log.WithName(upgradeAccelerator.Name)
		//logger.Info("Spec", "spec", upgradeAccelerator.Spec)
		// ============================================================================================
		// Base In-Reconciler Variables
		// ============================================================================================
		//validMachineConfigPools := []string{}
		//validMachineConfigPoolSelectors := make(map[string]*metav1.LabelSelector)
		//targetedNodes := []string{}
		//primingProhibitedNodes := []string{}

		// Determine Job Puller Image
		jobPullerImage := UpgradeAcceleratorDefaultJobPullerImage
		if upgradeAccelerator.Spec.Config.JobImage != "" {
			jobPullerImage = upgradeAccelerator.Spec.Config.JobImage
		}

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
			clusterInfrastructureType, err := getOpenShiftInfrastructureType(ctx, req, r.Client)
			if err != nil {
				logger.Error(err, "Failed to get OpenShift infrastructure type")
				_ = r.setConditionInfrastructureNotFound(ctx, upgradeAccelerator, err)
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_INFRASTRUCTURE, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			logger.Info("OpenShift Infrastructure Type: " + clusterInfrastructureType)
			_ = r.setConditionInfrastructureFound(ctx, upgradeAccelerator, clusterInfrastructureType)

			// Cluster Version State
			clusterVersionState, err := r.getClusterVersionState(ctx, upgradeAccelerator)
			if err != nil {
				logger.Error(err, "Failed to get Cluster Version State")
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_CLUSTER_VERSION, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			logger.Info("Cluster Version State", "state", clusterVersionState)

			// Set the UpgradeAcceleratorStatus for the CurrentVersion and TargetVersion
			upgradeAcceleratorStatusChanged := false
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
			if upgradeAcceleratorStatusChanged {
				err = r.Status().Update(ctx, upgradeAccelerator)
				if err != nil {
					logger.Error(err, "Failed to update UpgradeAccelerator status for versions")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}

				// Let's re-fetch the UpgradeAccelerator Custom Resource after updating the status
				// so that we have the latest state of the resource on the cluster and we will avoid
				// raising the error "the object has been modified, please apply
				// your changes to the latest version and try again" which would re-trigger the reconciliation
				// if we try to update it again in the following operations
				if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
					logger.Error(err, "Failed to re-fetch UpgradeAccelerator")
					return ctrl.Result{RequeueAfter: time.Second * 30}, err
				}
			}
			//upgradeAcceleratorStatusChanged = false

			// Check if the Current Version is the Desired Version - if so, no need to proceed
			// PROD: This is enabled in production deployments
			// Not helpful for debugging full flow but nice to not make the cluster do things it doesn't need to
			//if clusterVersionState.CurrentVersion == clusterVersionState.DesiredVersion {
			//	logger.Info("No upgrades in progress, no action required")
			//	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
			//}

			// ==========================================================================================
			// Targeted Nodes State Determination
			// ==========================================================================================
			targetedNodes, prohibitedNodes, primingProhibitedNodes, err := r.determineTargetedNodes(ctx, upgradeAccelerator, &logger)
			logger.Info("List of final targetedNodes", "targetedNodes", targetedNodes)
			logger.Info("List of final prohibitedNodes", "prohibitedNodes", prohibitedNodes)
			logger.Info("List of final primingProhibitedNodes", "primingProhibitedNodes", primingProhibitedNodes)
			if len(targetedNodes) == 0 {
				logger.Info("No targeted nodes found")
				upgradeAccelerator.Status.NodesSelected = nil
				upgradeAccelerator.Status.NodesPreheated = nil
				upgradeAccelerator.Status.NodesWaiting = nil
				upgradeAccelerator.Status.NodesWarming = nil
				upgradeAccelerator.Status.PrimerNodes = nil
				_ = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_NO_NODES_SELECTED, "No targeted nodes found")
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, nil
			} else {
				upgradeAccelerator.Status.NodesSelected = targetedNodes
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
			// Get the Release Images
			// ==========================================================================================
			releaseImages, err := GetReleaseImages(clusterVersionState.DesiredImage)
			if err != nil {
				logger.Error(err, "Failed to get Release Images")
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_RELEASE_IMAGES, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}
			//logger.V(1).Info("Release Images ("+fmt.Sprintf("%d", len(releaseImages))+"): ", "images", releaseImages)

			// ==========================================================================================
			// Filter the target release images
			// ==========================================================================================
			filteredReleaseImages := FilterReleaseImages(releaseImages, clusterInfrastructureType)
			//logger.V(1).Info("Filtered Release Images ("+fmt.Sprintf("%d", len(filteredReleaseImages))+"): ", "images", filteredReleaseImages)

			// ==========================================================================================
			// Create the Namespace for the operator workload components
			// ==========================================================================================
			operatorNamespace, err := r.createOperatorNamespace(ctx, upgradeAccelerator)
			if err != nil {
				logger.Error(err, "Failed to create Operator Namespace")
				_ = r.setConditionSetupNamespaceFailure(ctx, upgradeAccelerator, err.Error())
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
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
				_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// ==========================================================================================
			// Create Puller Script ConfigMap
			// ==========================================================================================
			err = r.createPullerScriptConfigMap(ctx, upgradeAccelerator, operatorNamespace)
			if err != nil {
				logger.Error(err, "Failed to create Puller Script ConfigMap")
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_RUNNING, CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				_ = r.setConditionSetupConfigMapFailure(ctx, upgradeAccelerator, err.Error())
				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			}

			// ==========================================================================================
			// Conditional Gate: SetupComplete
			// ==========================================================================================
			_ = r.setConditionSetupComplete(ctx, upgradeAccelerator)
			// TODO: Check for the Failure type, clear any previous failures

			// ==========================================================================================
			// Determine Node List States
			// Tip: Exit as early as possible from determined end states
			// ==========================================================================================
			upgradeAcceleratorStatusChanged = false

			// WTF is this code?  I should have slept sooner...
			// if upgradeAccelerator.Status.LastCompletedVersion != "" {
			// 	if upgradeAccelerator.Status.LastCompletedVersion == upgradeAccelerator.Status.TargetVersion {
			// 		upgradeAccelerator.Status.NodesWaiting = nil
			// 		upgradeAccelerator.Status.NodesWarming = nil
			// 		upgradeAccelerator.Status.NodesPreheated = nil
			// 		upgradeAccelerator.Status.PrimerNodes = nil
			// 	}
			// }

			// 	// If the lastCompletedVersion not is equal to the targetVersion then clear out the nodeLists
			// 	if upgradeAccelerator.Status.LastCompletedVersion != upgradeAccelerator.Status.TargetVersion {
			// 		if upgradeAccelerator.Status.NodesWaiting != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesWaiting = nil
			// 		}
			// 		if upgradeAccelerator.Status.NodesWarming != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesWarming = nil
			// 		}
			// 		if upgradeAccelerator.Status.NodesPreheated != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesPreheated = nil
			// 		}
			// 		if upgradeAccelerator.Status.PrimerNodes != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.PrimerNodes = nil
			// 		}
			// 		logger.Info("New target version detected, resetting node status lists", "lastCompletedVersion", upgradeAccelerator.Status.LastCompletedVersion, "targetVersion", upgradeAccelerator.Status.TargetVersion)
			// 	} else {
			// 		// If the lastCompletedVersion is equal to the targetVersion then clear out the nodeLists and finish reconciliation
			// 		if upgradeAccelerator.Status.NodesWaiting != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesWaiting = nil
			// 		}
			// 		if upgradeAccelerator.Status.NodesWarming != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesWarming = nil
			// 		}
			// 		if upgradeAccelerator.Status.NodesPreheated != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.NodesPreheated = nil
			// 		}
			// 		if upgradeAccelerator.Status.PrimerNodes != nil {
			// 			upgradeAcceleratorStatusChanged = true
			// 			upgradeAccelerator.Status.PrimerNodes = nil
			// 		}
			// 		logger.Info("No new target version detected, all nodes preheated for target version", "lastCompletedVersion", upgradeAccelerator.Status.LastCompletedVersion, "targetVersion", upgradeAccelerator.Status.TargetVersion)

			// 		if upgradeAcceleratorStatusChanged {
			// 			err = r.Status().Update(ctx, upgradeAccelerator)
			// 			if err != nil {
			// 				logger.Error(err, "Failed to update UpgradeAccelerator status")
			// 				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			// 			}
			// 			// Refetch the UpgradeAccelerator
			// 			if err := r.Get(ctx, req.NamespacedName, upgradeAccelerator); err != nil {
			// 				logger.Error(err, "Failed to re-fetch UpgradeAccelerator")
			// 				return ctrl.Result{RequeueAfter: time.Second * 30}, err
			// 			}
			// 			//return ctrl.Result{RequeueAfter: time.Second * 15}, nil
			// 		}
			// 	}
			// }

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
				//return ctrl.Result{RequeueAfter: time.Second * 15}, nil
			} else {

				// ==========================================================================================
				// Determine priming function
				// ==========================================================================================
				primerNodes := []string{}
				if upgradeAccelerator.Spec.Prime {
					// Check if there are manually set primer nodes
					nodeList := &corev1.NodeList{}
					if err := r.List(ctx, nodeList, client.MatchingLabels(map[string]string{"openshift.kemo.dev/primer-node": "true"})); err != nil {
						_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_NODES, err.Error())
						_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS})
						logger.Error(err, "Failed to list nodes", "selector", upgradeAccelerator.Spec.Selector.NodeSelector)
						return ctrl.Result{RequeueAfter: time.Second * 30}, err
					} else {
						// Labeled Nodes are listed - add manually set primer nodes
						for _, node := range nodeList.Items {
							primerNodes = append(primerNodes, node.Name)
						}
					}
					// If no manually set primer nodes, then just pick one from the targeted nodes if it's not part of the primingProhibitedNodes list or already in the primerNodes list
					for _, tNode := range targetedNodes {
						if !slices.Contains(primingProhibitedNodes, tNode) && !slices.Contains(primerNodes, tNode) {
							primerNodes = append(primerNodes, tNode)
						}
					}
					upgradeAccelerator.Status.PrimerNodes = primerNodes
					err = r.Status().Update(ctx, upgradeAccelerator)
					if err != nil {
						logger.Error(err, "Failed to update UpgradeAccelerator status")
						return ctrl.Result{RequeueAfter: time.Second * 30}, err
					}
					logger.Info("Primer Nodes", "nodes", primerNodes)
				} else {
					r.setConditionPrimerSkipped(ctx, upgradeAccelerator, "Priming effectively disabled")
					logger.Info("Priming effectively disabled")
				}

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
					for _, pNode := range primerNodes {
						// Make sure the node isn't already warming
						// Make sure the node isn't already preheated
						if !slices.Contains(upgradeAccelerator.Status.NodesWarming, pNode) || !slices.Contains(upgradeAccelerator.Status.NodesPreheated, pNode) {
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
								// If upgradeAccelerator.Status.NodesWaiting is only one element, we can reset it
								if len(upgradeAccelerator.Status.NodesWaiting) == 1 {
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
					_ = r.setConditionPrimerInProgress(ctx, upgradeAccelerator, "Primer nodes warming in progress: "+strings.Join(primerNodes, ", "))
					_ = r.setConditionRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_JOBS_SCHEDULED, "Following primer nodes currently warming: "+strings.Join(primerNodes, ", "))
				}

				// ==========================================================================================
				// Primer State Check
				// ==========================================================================================
				// If the priming is either disabled or we're done priming, continue to rotate through the remaining nodes
				primerCondition := r.getConditionReason(ctx, upgradeAccelerator, CONDITION_TYPE_PRIMER)
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
						err = r.setConditionNotRunning(ctx, upgradeAccelerator, CONDITION_REASON_RUNNING_RECONCILIATION_COMPLETED, "All nodes preheated")
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
		WithEventFilter(ignoreDeletionPredicate()).
		Named("upgradeaccelerator").
		Complete(r)
}
