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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
)

const (
	TypeAvailableTorrentServer = "Available"
	TypeDegradedTorrentServer  = "Degraded"
	TorrentServerFinalizer     = "torrent.qbittorrent.io/torrentserver-finalizer"
)

// TorrentServerReconciler reconciles a TorrentServer object
type TorrentServerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	OperatorImage string // operator image for init containers, set from OPERATOR_IMAGE env var
}

// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

func (r *TorrentServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling TorrentServer", "Request", req)

	// Step 1: Get the TorrentServer resource
	ts := &torrentv1alpha1.TorrentServer{}
	if err := r.Get(ctx, req.NamespacedName, ts); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 2: Handle deletion
	if !ts.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(ts, TorrentServerFinalizer) {
			controllerutil.RemoveFinalizer(ts, TorrentServerFinalizer)
			if err := r.Update(ctx, ts); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Step 3: Add finalizer
	if !controllerutil.ContainsFinalizer(ts, TorrentServerFinalizer) {
		controllerutil.AddFinalizer(ts, TorrentServerFinalizer)
		if err := r.Update(ctx, ts); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Step 4: Ensure credentials Secret
	secretName, err := r.ensureCredentialsSecret(ctx, ts)
	if err != nil {
		r.setDegradedCondition(ts, "CredentialsSecretError", err.Error())
		if statusErr := r.Status().Update(ctx, ts); statusErr != nil {
			logger.Error(statusErr, "Failed to update TorrentServer status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 5: Ensure config PVC
	pvcName, err := r.ensureConfigPVC(ctx, ts)
	if err != nil {
		r.setDegradedCondition(ts, "ConfigPVCError", err.Error())
		if statusErr := r.Status().Update(ctx, ts); statusErr != nil {
			logger.Error(statusErr, "Failed to update TorrentServer status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 6: Ensure Deployment
	deploymentName, err := r.ensureDeployment(ctx, ts, pvcName, secretName)
	if err != nil {
		r.setDegradedCondition(ts, "DeploymentError", err.Error())
		if statusErr := r.Status().Update(ctx, ts); statusErr != nil {
			logger.Error(statusErr, "Failed to update TorrentServer status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 7: Ensure Service
	serviceName, err := r.ensureService(ctx, ts)
	if err != nil {
		r.setDegradedCondition(ts, "ServiceError", err.Error())
		if statusErr := r.Status().Update(ctx, ts); statusErr != nil {
			logger.Error(statusErr, "Failed to update TorrentServer status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 8: Ensure TorrentClientConfiguration
	serviceURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, ts.Namespace, ts.Spec.WebUIPort)
	tccName, err := r.ensureTorrentClientConfiguration(ctx, ts, serviceURL, secretName)
	if err != nil {
		r.setDegradedCondition(ts, "ClientConfigError", err.Error())
		if statusErr := r.Status().Update(ctx, ts); statusErr != nil {
			logger.Error(statusErr, "Failed to update TorrentServer status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 9: Update status
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: ts.Namespace}, deployment); err == nil {
		ts.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	}
	ts.Status.DeploymentName = deploymentName
	ts.Status.ServiceName = serviceName
	ts.Status.ConfigPVCName = pvcName
	ts.Status.CredentialsSecretName = secretName
	ts.Status.ClientConfigurationName = tccName
	ts.Status.URL = serviceURL

	r.setAvailableCondition(ts, "Reconciled", "All resources are reconciled")
	if err := r.Status().Update(ctx, ts); err != nil {
		logger.Error(err, "Failed to update TorrentServer status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *TorrentServerReconciler) ensureCredentialsSecret(ctx context.Context, ts *torrentv1alpha1.TorrentServer) (string, error) {
	logger := log.FromContext(ctx)

	// If user specified an existing secret, validate it exists
	if ts.Spec.CredentialsSecret != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: ts.Spec.CredentialsSecret.Name, Namespace: ts.Namespace}, secret); err != nil {
			return "", fmt.Errorf("credentials secret %q not found: %w", ts.Spec.CredentialsSecret.Name, err)
		}
		if _, ok := secret.Data["username"]; !ok {
			return "", fmt.Errorf("credentials secret %q missing 'username' key", ts.Spec.CredentialsSecret.Name)
		}
		if _, ok := secret.Data["password"]; !ok {
			return "", fmt.Errorf("credentials secret %q missing 'password' key", ts.Spec.CredentialsSecret.Name)
		}
		return ts.Spec.CredentialsSecret.Name, nil
	}

	// Auto-generate a credentials secret
	secretName := ts.Name + "-credentials"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ts.Namespace,
			Labels:    labelsForTorrentServer(ts.Name),
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if err := controllerutil.SetControllerReference(ts, secret, r.Scheme); err != nil {
			return err
		}
		// Only set data on creation (don't overwrite existing credentials)
		if secret.CreationTimestamp.IsZero() {
			password, err := generateRandomPassword(16)
			if err != nil {
				return fmt.Errorf("failed to generate password: %w", err)
			}
			secret.Data = map[string][]byte{
				"username": []byte("admin"),
				"password": []byte(password),
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure credentials secret: %w", err)
	}
	logger.V(1).Info("Credentials secret ensured", "name", secretName, "result", result)

	return secretName, nil
}

func (r *TorrentServerReconciler) ensureConfigPVC(ctx context.Context, ts *torrentv1alpha1.TorrentServer) (string, error) {
	logger := log.FromContext(ctx)
	pvcName := ts.Name + "-config"

	storageSize := "1Gi"
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	var storageClassName *string

	if ts.Spec.ConfigStorage != nil {
		if ts.Spec.ConfigStorage.Size != "" {
			storageSize = ts.Spec.ConfigStorage.Size
		}
		if len(ts.Spec.ConfigStorage.AccessModes) > 0 {
			accessModes = ts.Spec.ConfigStorage.AccessModes
		}
		storageClassName = ts.Spec.ConfigStorage.StorageClassName
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: ts.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		if err := controllerutil.SetControllerReference(ts, pvc, r.Scheme); err != nil {
			return err
		}
		// PVC spec is immutable after creation, only set on create
		if pvc.CreationTimestamp.IsZero() {
			pvc.Labels = labelsForTorrentServer(ts.Name)
			pvc.Spec = corev1.PersistentVolumeClaimSpec{
				AccessModes:      accessModes,
				StorageClassName: storageClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(storageSize),
					},
				},
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure config PVC: %w", err)
	}
	logger.V(1).Info("Config PVC ensured", "name", pvcName, "result", result)

	return pvcName, nil
}

func (r *TorrentServerReconciler) ensureDeployment(ctx context.Context, ts *torrentv1alpha1.TorrentServer, configPVCName, credentialsSecretName string) (string, error) {
	logger := log.FromContext(ctx)
	deploymentName := ts.Name

	labels := labelsForTorrentServer(ts.Name)
	replicas := int32(1)
	if ts.Spec.Replicas != nil {
		replicas = *ts.Spec.Replicas
	}

	port := ts.Spec.WebUIPort
	if port == 0 {
		port = 8080
	}

	image := ts.Spec.Image
	if image == "" {
		image = "lscr.io/linuxserver/qbittorrent:amd64-5.1.4"
	}

	// Build volumes and volume mounts
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: configPVCName,
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/config",
		},
	}

	for _, dv := range ts.Spec.DownloadVolumes {
		volName := "download-" + dv.ClaimName
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: dv.ClaimName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: dv.MountPath,
		})
	}

	// Add credentials volume and init container when operator image is available
	var initContainers []corev1.Container
	if r.OperatorImage != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: credentialsSecretName,
				},
			},
		})
		runAsUser := int64(0)
		allowPrivilegeEscalation := false
		readOnlyRootFilesystem := true
		initContainers = []corev1.Container{
			{
				Name:    "config-init",
				Image:   r.OperatorImage,
				Command: []string{"/manager", "config-init"},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "config", MountPath: "/config"},
					{Name: "credentials", MountPath: "/credentials", ReadOnly: true},
				},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:                &runAsUser,
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
					ReadOnlyRootFilesystem: &readOnlyRootFilesystem,
				},
			},
		}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: ts.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		if err := controllerutil.SetControllerReference(ts, deployment, r.Scheme); err != nil {
			return err
		}
		deployment.Labels = labels
		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					InitContainers: initContainers,
					Containers: []corev1.Container{
						{
							Name:            "qbittorrent",
							Image:           image,
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{
									Name:          "webui",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env:          ts.Spec.Env,
							VolumeMounts: volumeMounts,
							Resources:    ts.Spec.Resources,
						},
					},
					Volumes:       volumes,
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure deployment: %w", err)
	}
	logger.V(1).Info("Deployment ensured", "name", deploymentName, "result", result)

	return deploymentName, nil
}

func (r *TorrentServerReconciler) ensureService(ctx context.Context, ts *torrentv1alpha1.TorrentServer) (string, error) {
	logger := log.FromContext(ctx)
	serviceName := ts.Name

	port := ts.Spec.WebUIPort
	if port == 0 {
		port = 8080
	}

	serviceType := ts.Spec.ServiceType
	if serviceType == "" {
		serviceType = corev1.ServiceTypeClusterIP
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ts.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(ts, svc, r.Scheme); err != nil {
			return err
		}
		svc.Labels = labelsForTorrentServer(ts.Name)
		svc.Spec = corev1.ServiceSpec{
			Type:     serviceType,
			Selector: labelsForTorrentServer(ts.Name),
			Ports: []corev1.ServicePort{
				{
					Name:       "webui",
					Port:       port,
					TargetPort: intstr.FromString("webui"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure service: %w", err)
	}
	logger.V(1).Info("Service ensured", "name", serviceName, "result", result)

	return serviceName, nil
}

func (r *TorrentServerReconciler) ensureTorrentClientConfiguration(ctx context.Context, ts *torrentv1alpha1.TorrentServer, serviceURL, secretName string) (string, error) {
	logger := log.FromContext(ctx)
	tccName := ts.Name + "-client-config"

	tcc := &torrentv1alpha1.TorrentClientConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tccName,
			Namespace: ts.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, tcc, func() error {
		if err := controllerutil.SetControllerReference(ts, tcc, r.Scheme); err != nil {
			return err
		}
		tcc.Labels = labelsForTorrentServer(ts.Name)
		tcc.Labels["torrent.qbittorrent.io/managed-by"] = ts.Name
		tcc.Spec = torrentv1alpha1.TorrentClientConfigurationSpec{
			URL: serviceURL,
			CredentialsSecret: torrentv1alpha1.SecretReference{
				Name: secretName,
			},
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure TorrentClientConfiguration: %w", err)
	}
	logger.V(1).Info("TorrentClientConfiguration ensured", "name", tccName, "result", result)

	return tccName, nil
}

func (r *TorrentServerReconciler) setAvailableCondition(ts *torrentv1alpha1.TorrentServer, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeAvailableTorrentServer,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&ts.Status.Conditions, condition)
	meta.RemoveStatusCondition(&ts.Status.Conditions, TypeDegradedTorrentServer)
}

func (r *TorrentServerReconciler) setDegradedCondition(ts *torrentv1alpha1.TorrentServer, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeDegradedTorrentServer,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&ts.Status.Conditions, condition)
	meta.RemoveStatusCondition(&ts.Status.Conditions, TypeAvailableTorrentServer)
}

func labelsForTorrentServer(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "qbittorrent",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "qbittorrent-operator",
	}
}

func generateRandomPassword(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TorrentServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.TorrentServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&torrentv1alpha1.TorrentClientConfiguration{}).
		Named("torrentserver").
		Complete(r)
}
