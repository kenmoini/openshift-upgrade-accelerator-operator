package controller

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"dario.cat/mergo"
	"github.com/go-logr/logr"
	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO: Add MatchingFieldSelector functionality, dummy
func (r *UpgradeAcceleratorReconciler) determineTargetedNodes(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, logger *logr.Logger) (targetedNodes []string,
	prohibitedNodes []string, primerNodes []string, primingProhibitedNodes []string, err error) {

	validMachineConfigPoolSelectors := make(map[string]*metav1.LabelSelector)
	listOpts := []client.ListOption{}

	// Get the list of nodes from the cluster, first filtering by MachineConfigPools and then by nodeSelector
	nodes := &corev1.NodeList{}
	machineConfigPools := &machineconfigv1.MachineConfigPoolList{}

	// Get all the Nodes
	if err := r.List(ctx, nodes, listOpts...); err != nil {
		logger.Error(err, "determineTargetedNodes: Failed to list nodes")
		_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_NODES, err.Error())
		_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_RUNNING, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
		return nil, nil, nil, nil, err
	} else {
		logger.Info("determineTargetedNodes: Successfully listed " + strconv.Itoa(len(nodes.Items)) + " nodes")
	}

	// ===============================================================================================================
	// Preliminary Node Filter Checks
	for _, node := range nodes.Items {
		// Filtering - Determine if the node has preheating disabled
		// If this node has preheating explicitly disabled, add it to the prohibitedNodes list
		// This disables the node from being preheated no matter what other selectors are applied
		preheatingProhibited := false
		preheatingLabelValue, ok := node.Labels["openshift.kemo.dev/disable-preheat"]
		if ok && strings.ToLower(preheatingLabelValue) == "true" {
			logger.Info("determineTargetedNodes: Manually set as prohibited node " + node.Name)
			prohibitedNodes = append(prohibitedNodes, node.Name)
			preheatingProhibited = true
		}
		// Filtering - Determine if the node has priming disabled
		// If this node has priming explicitly disabled, add it to the primingProhibitedNodes
		primingLabelValue, ok := node.Labels["openshift.kemo.dev/primer-node"]
		if ok && strings.ToLower(primingLabelValue) == "false" {
			logger.Info("determineTargetedNodes: Manually set as priming prohibited node " + node.Name)
			primingProhibitedNodes = append(primingProhibitedNodes, node.Name)
		} else {
			if !preheatingProhibited {
				logger.Info("determineTargetedNodes: Manually set as primer node " + node.Name)
				primerNodes = append(primerNodes, node.Name)
			}
		}
	}

	// ===============================================================================================================
	// Default Configuration - All Nodes Targeted
	// If there are no selectors defined, then all nodes are targeted
	if upgradeAccelerator.Spec.Selector.NodeSelector == nil && len(upgradeAccelerator.Spec.Selector.MachineConfigPools) == 0 {
		logger.Info("determineTargetedNodes: No selectors defined, targeting all " + strconv.Itoa(len(nodes.Items)) + " nodes")
		for _, node := range nodes.Items {
			// Check to make sure the node is not prohibited
			if !slices.Contains(prohibitedNodes, node.Name) {
				targetedNodes = append(targetedNodes, node.Name)
			}
		}
	}

	// ===============================================================================================================
	// If MachineConfigPools are defined then first we apply that as a selector, and finally filter additionally on NodeSelectors if those are defined
	if len(upgradeAccelerator.Spec.Selector.MachineConfigPools) > 0 {
		logger.Info("determineTargetedNodes: MachineConfigPools selector definition found in UpgradeAccelerator", "definedPools", upgradeAccelerator.Spec.Selector.MachineConfigPools)

		// Get all the MachineConfigPools
		if err := r.List(ctx, machineConfigPools, listOpts...); err != nil {
			logger.Error(err, "determineTargetedNodes: Failed to list MachineConfigPools")
			_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_MACHINECONFIGPOOLS, err.Error())
			_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
			return nil, nil, nil, nil, err
		} else {
			logger.Info("determineTargetedNodes: Successfully listed " + strconv.Itoa(len(machineConfigPools.Items)) + " MachineConfigPools")
		}

		// Extract the names from the machineConfigPools and set as a list of strings
		for _, pool := range machineConfigPools.Items {
			if slices.Contains(upgradeAccelerator.Spec.Selector.MachineConfigPools, pool.Name) {
				if pool.Spec.NodeSelector != nil {
					validMachineConfigPoolSelectors[pool.Name] = pool.Spec.NodeSelector
				} else {
					logger.Info("determineTargetedNodes: MachineConfigPool has no valid selectors, skipping", "pool", pool.Name)
				}
			}
		}

		// Check the length of the validMachineConfigPoolSelectors
		// If it is zero then none of the specified MCPs were found
		if len(validMachineConfigPoolSelectors) == 0 {
			logger.Info("determineTargetedNodes: No valid MachineConfigPools found")
		}
	}

	// ===============================================================================================================
	// Check for NodeSelectors
	if upgradeAccelerator.Spec.Selector.NodeSelector != nil {
		logger.Info("determineTargetedNodes: NodeSelector definition found in UpgradeAccelerator", "nodeSelector", upgradeAccelerator.Spec.Selector.NodeSelector)
		if len(validMachineConfigPoolSelectors) > 0 {
			// ===============================================================================================================
			// If there are BOTH NodeSelectors and validMachineConfigPoolSelectors then get a new list of nodes that match both selector sets
			logger.Info("determineTargetedNodes: Merging nodeSelector and validMachineConfigPoolSelectors")
			// Loop through each validMachineConfigPoolSelector and add the nodeSelector to it
			for pool, selector := range validMachineConfigPoolSelectors {
				// Every MCP should have a set of nodeSelectors applied to it, checks are done earlier
				// Save here in case of weird nil dereference errors?
				// if selector != nil {
				// Merge the nodeSelectors
				if err := mergo.Merge(selector, upgradeAccelerator.Spec.Selector.NodeSelector); err != nil {
					_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_SETUP, err.Error())
					_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
					logger.Error(err, "determineTargetedNodes: Failed to merge NodeSelector into MachineConfigPoolSelector", "pool", pool)
					return nil, nil, nil, nil, err
				}
				logger.Info("determineTargetedNodes: Merged NodeSelector into MachineConfigPoolSelectors", pool, selector)
				// }
			}
			for pool, selector := range validMachineConfigPoolSelectors {
				// Next get the nodes that match this pool's merged selectors
				// If they're not already in the targetedNodes list then add it
				mcpSelectedNodesList := &corev1.NodeList{}
				if err := r.List(ctx, mcpSelectedNodesList, client.MatchingLabels(selector.MatchLabels)); err != nil {
					logger.Error(err, "determineTargetedNodes: Failed to list nodes by MachineConfigPoolSelector for pool "+pool, "selector", selector)
					_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_NODES, err.Error())
					_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
					return nil, nil, nil, nil, err
				}
				// Check the length
				if len(mcpSelectedNodesList.Items) == 0 {
					logger.Info("determineTargetedNodes: No nodes found for MachineConfigPoolSelector for pool "+pool, "selector", selector)
				}
				// Loop through each node and add it to the targetedNodes list if it's not already added (eg SNO/3-Node)
				for _, node := range mcpSelectedNodesList.Items {
					if !slices.Contains(targetedNodes, node.Name) {
						targetedNodes = append(targetedNodes, node.Name)
					}
				}
			}
		} else {
			// ===============================================================================================================
			// If there are ONLY NodeSelectors and no validMachineConfigPoolSelectors, then just get the list of nodes that match the nodeSelectors
			logger.Info("determineTargetedNodes: Simply filtering by nodeSelector without any validMachineConfigPoolSelectors")

			nodeSelectorList := &corev1.NodeList{}
			if err := r.List(ctx, nodeSelectorList, client.MatchingLabels(upgradeAccelerator.Spec.Selector.NodeSelector.MatchLabels)); err != nil {
				logger.Error(err, "determineTargetedNodes: Failed to list nodes by nodeSelector", "selector", upgradeAccelerator.Spec.Selector.NodeSelector)
				_ = r.setConditionFailure(ctx, upgradeAccelerator, CONDITION_REASON_FAILURE_GET_NODES, err.Error())
				_ = r.deleteConditions(ctx, upgradeAccelerator, []string{CONDITION_TYPE_SUCCESSFUL, CONDITION_TYPE_PRIMER, CONDITION_TYPE_PRIMING_IN_PROGRESS, CONDITION_TYPE_SETUP_COMPLETE})
				return nil, nil, nil, nil, err
			} else {
				// Loop through each node and add it to the list
				for _, node := range nodeSelectorList.Items {
					// Make sure that the node does not have the openshift.kemo.dev/disable-preheat: "true" label
					// We examine the node with the current list context to keep things fresh
					excludeLabelValue, ok := node.Labels["openshift.kemo.dev/disable-preheat"]
					if ok && strings.ToLower(excludeLabelValue) == "true" {
						logger.Info("determineTargetedNodes: Node has openshift.kemo.dev/disable-preheat: \"true\" label, skipping", "node", node.Name)
					} else {
						targetedNodes = append(targetedNodes, node.Name)
					}
				}
			}
		}
	}

	// Sort the list of targetNodes
	if len(targetedNodes) > 1 {
		targetedNodes = sortSliceOfStrings(targetedNodes)
	}

	// Check if priming is even enabled
	if upgradeAccelerator.Spec.Prime {
		// Priming is set as a functional state
		// It toggles the priming functionality entirely, even if nodes are manually specified to be primed
		logger.Info("determineTargetedNodes: Priming is enabled")
		// Check if there are currently defined primerNodes and any targetedNodes
		if len(targetedNodes) > 0 {
			if len(primerNodes) == 0 {
				// If not we should loop through the list of sorted targetedNodes
				// and pick the first one alphabetically if it's not part of the primingProhibitedNodes list
				for _, nodeName := range targetedNodes {
					if !slices.Contains(primingProhibitedNodes, nodeName) {
						primerNodes = append(primerNodes, nodeName)
						break
					}
				}
			} else { // primerNodes > 0
				// Make sure the manually defined primerNodes are in the targetedNodes list, and if not add it
				// Discount double check!
				for _, prmrNode := range primerNodes {
					if !slices.Contains(targetedNodes, prmrNode) {
						targetedNodes = append(targetedNodes, prmrNode)
					}
				}
			}
		} else { // targetedNodes == 0
			// If there are no targetedNodes, then check if there are manually defined primerNodes
			// If so, those are the new targetedNodes
			if len(primerNodes) > 0 {
				if len(primerNodes) > 1 {
					targetedNodes = sortSliceOfStrings(primerNodes)
				} else {
					targetedNodes = primerNodes
				}
			} // else: no nodes at all!
		}
	} else {
		// Priming is disabled - Empty primerNodes
		primerNodes = []string{}
	}

	// This function should return a configured state idempotent list of nodes
	// Sort the prohibitedNodes, primerNodes, and primingProhibitedNodes if they're not empty
	if len(prohibitedNodes) > 1 {
		prohibitedNodes = sortSliceOfStrings(prohibitedNodes)
	}
	if len(primerNodes) > 1 {
		primerNodes = sortSliceOfStrings(primerNodes)
	}
	if len(primingProhibitedNodes) > 1 {
		primingProhibitedNodes = sortSliceOfStrings(primingProhibitedNodes)
	}

	// Size determination and status checks are completed external to this function on the consuming end
	return targetedNodes, prohibitedNodes, primerNodes, primingProhibitedNodes, nil
}
