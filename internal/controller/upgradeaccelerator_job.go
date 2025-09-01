package controller

import (
	"context"

	openshiftv1alpha1 "github.com/kenmoini/openshift-upgrade-accelerator-operator/api/v1alpha1"
	"github.com/operator-framework/operator-lib/proxy"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type PullJob struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	ContainerImage string `json:"containerImage"`
	ConfigMapName  string `json:"configMapName"`
	TargetNodeName string `json:"targetNodeName"`
	ReleaseVersion string `json:"releaseVersion"`
}

func (reconciler *UpgradeAcceleratorReconciler) createPullJob(ctx context.Context, upgradeAccelerator *openshiftv1alpha1.UpgradeAccelerator, pullJob PullJob) error {
	// Implementation for creating a pull job
	// Define some base labels
	jobLabels := map[string]string{
		"app":                      "upgrade-accelerator",
		"upgrade-accelerator/name": upgradeAccelerator.Name,
		"pull-job/release":         pullJob.ReleaseVersion,
		"pull-job/name":            pullJob.Name,
		"pull-job/namespace":       pullJob.Namespace,
		"pull-job/targetNodeName":  pullJob.TargetNodeName,
	}

	jobMetadata := metav1.ObjectMeta{Name: pullJob.Name, Namespace: pullJob.Namespace, Labels: jobLabels, OwnerReferences: []metav1.OwnerReference{
		*metav1.NewControllerRef(upgradeAccelerator, openshiftv1alpha1.GroupVersion.WithKind("UpgradeAccelerator")),
	}}

	baseEnvVars := []corev1.EnvVar{
		{Name: "PULL_JOB_NAME", Value: pullJob.Name},
		{Name: "PULL_JOB_NAMESPACE", Value: pullJob.Namespace},
		{Name: "PULL_JOB_IMAGE", Value: pullJob.ContainerImage},
		{Name: "PULL_JOB_CONFIGMAP", Value: pullJob.ConfigMapName},
	}
	envProxyConfig := proxy.ReadProxyVarsFromEnv()
	logger := logf.FromContext(ctx)
	logger.Info("Proxy Environment Variables: ", "vars", envProxyConfig)
	baseEnvVars = append(baseEnvVars, envProxyConfig...)

	jobConstructor := &batchv1.Job{
		ObjectMeta: jobMetadata,
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: pointer.Int32Ptr(3600),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					HostPID:       true,
					HostNetwork:   true,
					HostIPC:       true,
					NodeName:      pullJob.TargetNodeName,
					Tolerations:   UpgradeAcceleratorDefaultJobTolerations,
					Containers: []corev1.Container{
						corev1.Container{
							Name:    "preheater",
							Image:   pullJob.ContainerImage,
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "--", "sh /opt/upgrade-accelerator/puller.sh;"},
							Env:     baseEnvVars,
							TTY:     true,
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.BoolPtr(true),
								RunAsUser:  pointer.Int64Ptr(0),
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
										Name: pullJob.ConfigMapName,
									},
								},
							},
						},
						{
							Name: "pull-script",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "release-puller-script",
									},
								},
							},
						},
						{
							Name: "host",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/",
									Type: (*corev1.HostPathType)(pointer.StringPtr(string("Directory"))),
								},
							},
						},
					},
				},
			},
		},
	}

	// Tolerations
	if len(upgradeAccelerator.Spec.Config.Scheduling.Tolerations) > 0 {
		jobConstructor.Spec.Template.Spec.Tolerations = upgradeAccelerator.Spec.Config.Scheduling.Tolerations
	}

	// Check to see if the Job exists already
	existingJob := &batchv1.Job{}
	if err := reconciler.Get(ctx, client.ObjectKey{Name: pullJob.Name, Namespace: pullJob.Namespace}, existingJob); err != nil {
		if kapierrors.IsNotFound(err) {
			// Job doesn't exist, create it
			err = reconciler.Create(ctx, jobConstructor)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}
