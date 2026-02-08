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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

const (
	TypeAvailableTCC = "Available"
	TypeDegradedTCC  = "Degraded"
)

// TorrentClientConfigurationReconciler reconciles a TorrentClientConfiguration object
type TorrentClientConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TorrentClientConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling TorrentClientConfiguration", "Request", req)

	// Step 1: Get the TCC resource
	tcc := &torrentv1alpha1.TorrentClientConfiguration{}
	if err := r.Get(ctx, req.NamespacedName, tcc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 2: Parse check interval
	checkInterval := 60 * time.Second
	if tcc.Spec.CheckInterval != "" {
		parsed, err := time.ParseDuration(tcc.Spec.CheckInterval)
		if err != nil {
			logger.Error(err, "Invalid checkInterval, using default 60s")
		} else {
			checkInterval = parsed
		}
	}

	// Step 3: Parse timeout
	timeout := 10 * time.Second
	if tcc.Spec.Timeout != "" {
		parsed, err := time.ParseDuration(tcc.Spec.Timeout)
		if err != nil {
			logger.Error(err, "Invalid timeout, using default 10s")
		} else {
			timeout = parsed
		}
	}

	// Step 4: Validate the referenced Secret exists and has required keys
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: tcc.Spec.CredentialsSecret.Name, Namespace: tcc.Namespace}, secret); err != nil {
		r.setDegradedCondition(tcc, "SecretNotFound",
			fmt.Sprintf("Credentials secret %q not found: %v", tcc.Spec.CredentialsSecret.Name, err))
		tcc.Status.Connected = false
		now := metav1.Now()
		tcc.Status.LastChecked = &now
		if statusErr := r.Status().Update(ctx, tcc); statusErr != nil {
			logger.Error(statusErr, "Failed to update TCC status")
		}
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	usernameBytes, hasUsername := secret.Data["username"]
	passwordBytes, hasPassword := secret.Data["password"]
	if !hasUsername || !hasPassword {
		r.setDegradedCondition(tcc, "SecretInvalid",
			fmt.Sprintf("Credentials secret %q missing 'username' or 'password' key", tcc.Spec.CredentialsSecret.Name))
		tcc.Status.Connected = false
		now := metav1.Now()
		tcc.Status.LastChecked = &now
		if statusErr := r.Status().Update(ctx, tcc); statusErr != nil {
			logger.Error(statusErr, "Failed to update TCC status")
		}
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	// Step 5: Test connectivity to qBittorrent
	qbtClient := qbittorrent.NewClientWithTimeout(tcc.Spec.URL, timeout)
	if err := qbtClient.Login(ctx, string(usernameBytes), string(passwordBytes)); err != nil {
		r.setDegradedCondition(tcc, "LoginFailed",
			fmt.Sprintf("Failed to login to qBittorrent at %s: %v", tcc.Spec.URL, err))
		tcc.Status.Connected = false
		now := metav1.Now()
		tcc.Status.LastChecked = &now
		if statusErr := r.Status().Update(ctx, tcc); statusErr != nil {
			logger.Error(statusErr, "Failed to update TCC status")
		}
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	// Step 6: Health check - try to list torrents
	if err := qbtClient.Ping(ctx); err != nil {
		r.setDegradedCondition(tcc, "HealthCheckFailed",
			fmt.Sprintf("qBittorrent health check failed at %s: %v", tcc.Spec.URL, err))
		tcc.Status.Connected = false
		now := metav1.Now()
		tcc.Status.LastChecked = &now
		if statusErr := r.Status().Update(ctx, tcc); statusErr != nil {
			logger.Error(statusErr, "Failed to update TCC status")
		}
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	// Step 7: Successfully connected
	r.setAvailableCondition(tcc, "Connected",
		fmt.Sprintf("Successfully connected to qBittorrent at %s", tcc.Spec.URL))
	tcc.Status.Connected = true
	now := metav1.Now()
	tcc.Status.LastChecked = &now

	if err := r.Status().Update(ctx, tcc); err != nil {
		logger.Error(err, "Failed to update TCC status")
		return ctrl.Result{}, err
	}

	logger.Info("TCC connectivity check passed", "url", tcc.Spec.URL)
	return ctrl.Result{RequeueAfter: checkInterval}, nil
}

func (r *TorrentClientConfigurationReconciler) setAvailableCondition(tcc *torrentv1alpha1.TorrentClientConfiguration, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeAvailableTCC,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&tcc.Status.Conditions, condition)
	meta.RemoveStatusCondition(&tcc.Status.Conditions, TypeDegradedTCC)
}

func (r *TorrentClientConfigurationReconciler) setDegradedCondition(tcc *torrentv1alpha1.TorrentClientConfiguration, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeDegradedTCC,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&tcc.Status.Conditions, condition)
	meta.RemoveStatusCondition(&tcc.Status.Conditions, TypeAvailableTCC)
}

// findTCCForSecret maps Secret changes back to TCC resources that reference them.
func (r *TorrentClientConfigurationReconciler) findTCCForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	tccList := &torrentv1alpha1.TorrentClientConfigurationList{}
	if err := r.List(ctx, tccList, client.InNamespace(secret.Namespace)); err != nil {
		logger.Error(err, "Failed to list TCCs for secret mapping")
		return nil
	}

	var requests []reconcile.Request
	for _, tcc := range tccList.Items {
		if tcc.Spec.CredentialsSecret.Name == secret.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tcc.Name,
					Namespace: tcc.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *TorrentClientConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.TorrentClientConfiguration{}).
		// Reconcile TCCs when their referenced Secret changes
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.findTCCForSecret)).
		Named("torrentclientconfiguration").
		Complete(r)
}
