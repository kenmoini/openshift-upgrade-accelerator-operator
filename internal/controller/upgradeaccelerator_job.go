package controller

import (
	"context"
	"fmt"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	"github.com/operator-framework/operator-lib/proxy"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// "k8s.io/utils/pointer"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type PullJob struct {
	// Name of the Job created - defaults to <node_name>-ua-puller-<hash=desiredVersion>
	Name string `json:"name"`
	// Namespace to create the Job in - defaults to openshift-upgrade-accelerator
	Namespace string `json:"namespace"`
	// TargetNodeName is the name of the Node to run the job on
	TargetNodeName string `json:"targetNodeName"`
	// ReleaseConfigMapName can override the name of the ConfigMap passed to the job for the Release JSON data
	// TODO: Currently there is no interface to elevate this from anywhere else
	ReleaseConfigMapName string `json:"releaseConfigMapName,omitempty"`
}

// nolint: unused
func (r *UpgradeAcceleratorReconciler) getCompletedJobNodeList(ctx context.Context, upgradeAcceleratorName string,
	releaseVersion string) (completedNodes []string, err error) {
	jobList := batchv1.JobList{}
	labelSelector := client.MatchingLabels{
		"app":                      UpgradeAcceleratorDefaultAppLabelValue,
		"upgrade-accelerator/name": upgradeAcceleratorName,
		"pull-job/release":         releaseVersion,
	}
	// Get all the jobs that match the current UpgradeAccelerator, error out else
	if err := r.List(ctx, &jobList, labelSelector); err != nil {
		return nil, err
	}

	// Loop through the jobs
	// If they're completed then add them to the completedNodes list with the name of the node
	for _, job := range jobList.Items {
		if job.Status.Succeeded > 0 {
			completedNodes = append(completedNodes, job.Spec.Template.Spec.NodeName)
		}
	}

	return completedNodes, nil
}

func (r *UpgradeAcceleratorReconciler) createPullJob(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, pullJob PullJob) (createdJob bool, err error) {
	// Implementation for creating a pull job
	// Determine Job Puller Image
	jobPullerImage := UpgradeAcceleratorDefaultJobPullerImage
	if upgradeAccelerator.Spec.Config.JobImage != "" {
		jobPullerImage = upgradeAccelerator.Spec.Config.JobImage
	}
	// Determine the release configmap name
	determinedReleaseJSONConfigMapName := fmt.Sprintf("release-%s", hashString(upgradeAccelerator.Status.DesiredVersion))
	if pullJob.ReleaseConfigMapName != "" {
		determinedReleaseJSONConfigMapName = pullJob.ReleaseConfigMapName
	}
	// Determine the pull script configmap name
	determinedPullScriptConfigMapName := fmt.Sprintf("release-puller-script-%s", hashString(upgradeAccelerator.Status.DesiredVersion))
	if upgradeAccelerator.Spec.Config.PullScriptConfigMapName != "" {
		determinedPullScriptConfigMapName = upgradeAccelerator.Spec.Config.PullScriptConfigMapName
	}
	// Determine the Job Tolerations
	determinedTolerations := UpgradeAcceleratorDefaultJobTolerations
	if len(upgradeAccelerator.Spec.Config.Scheduling.Tolerations) > 0 {
		determinedTolerations = upgradeAccelerator.Spec.Config.Scheduling.Tolerations
	}

	// Define some base labels
	jobLabels := map[string]string{
		"app":                      UpgradeAcceleratorDefaultAppLabelValue,
		"upgrade-accelerator/name": upgradeAccelerator.Name,
		"pull-job/release":         upgradeAccelerator.Status.DesiredVersion,
		"pull-job/name":            pullJob.Name,
		"pull-job/namespace":       pullJob.Namespace,
		"pull-job/targetNodeName":  pullJob.TargetNodeName,
	}

	// Metadata Assembly
	jobMetadata := metav1.ObjectMeta{Name: pullJob.Name, Namespace: pullJob.Namespace, Labels: jobLabels, OwnerReferences: []metav1.OwnerReference{
		*metav1.NewControllerRef(upgradeAccelerator, openshiftv1alpha1.GroupVersion.WithKind("UpgradeAccelerator")),
	}}

	// Env vars and Proxy Config
	baseEnvVars := []corev1.EnvVar{
		{Name: "PULL_JOB_NAME", Value: pullJob.Name},
		{Name: "PULL_JOB_NAMESPACE", Value: pullJob.Namespace},
		{Name: "PULL_JOB_IMAGE", Value: jobPullerImage},
		{Name: "PULL_JOB_RELEASE_CONFIGMAP", Value: determinedReleaseJSONConfigMapName},
		{Name: "PULL_JOB_SCRIPT_CONFIGMAP", Value: determinedPullScriptConfigMapName},
	}
	envProxyConfig := proxy.ReadProxyVarsFromEnv()
	logger := logf.FromContext(ctx)
	logger.Info("Proxy Environment Variables: ", "vars", envProxyConfig)
	baseEnvVars = append(baseEnvVars, envProxyConfig...)

	// nolint: gofmt // No clue why the formatting is off according to lint, it looks beautiful
	jobConstructor := &batchv1.Job{
		ObjectMeta: jobMetadata,
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To(int32(UpgradeAcceleratorDefaultJobTTL)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					HostPID:       true,
					HostNetwork:   true,
					HostIPC:       true,
					NodeName:      pullJob.TargetNodeName,
					Tolerations:   determinedTolerations,
					Containers: []corev1.Container{
						corev1.Container{
							Name:    "preheater",
							Image:   jobPullerImage,
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "--", "sh /opt/upgrade-accelerator/puller.sh;"},
							Env:     baseEnvVars,
							TTY:     true,
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To(true),
								RunAsUser:  ptr.To(int64(0)),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeUnconfined,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "release",
									MountPath: "/etc/upgrade-accelerator/release",
									ReadOnly:  true,
								},
								{
									Name:      "pull-script",
									MountPath: "/opt/upgrade-accelerator",
									ReadOnly:  true,
								},
								{
									Name:      "host",
									MountPath: "/host",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "release",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: determinedReleaseJSONConfigMapName,
									},
								},
							},
						},
						{
							Name: "pull-script",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: determinedPullScriptConfigMapName,
									},
								},
							},
						},
						{
							Name: "host",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/",
									Type: (*corev1.HostPathType)(ptr.To(string("Directory"))),
								},
							},
						},
					},
				},
			},
		},
	}

	// Check to see if the Job exists already
	existingJob := &batchv1.Job{}
	createdJob = false
	if err := r.Get(ctx, client.ObjectKey{Name: pullJob.Name, Namespace: pullJob.Namespace}, existingJob); err != nil {
		if kapierrors.IsNotFound(err) {
			// Job doesn't exist, create it
			err = r.Create(ctx, jobConstructor)
			if err != nil {
				return createdJob, err
			}
			createdJob = true
		} else {
			return createdJob, err
		}
	}
	return createdJob, nil
}
