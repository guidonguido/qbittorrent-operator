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

type TorrentClientConfigurationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientPool *qbittorrent.ClientPool
}

// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TorrentClientConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling TorrentClientConfiguration", "Request", req)

	// 1. Request contains only name and namespace,
	// need to fetch the full resource to get spec and status
	tcc := &torrentv1alpha1.TorrentClientConfiguration{}
	if err := r.Get(ctx, req.NamespacedName, tcc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Parse check interval
	checkInterval := 60 * time.Second
	if tcc.Spec.CheckInterval != "" {
		parsed, err := time.ParseDuration(tcc.Spec.CheckInterval)
		if err != nil {
			logger.Error(err, "Invalid checkInterval, using default 60s")
		} else {
			checkInterval = parsed
		}
	}

	// 4. Validate the creds Secret exists and has required keys
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

	// 5. Test connectivity to qBittorrent
	qbtClient, err := r.ClientPool.GetOrCreate(
		ctx,
		tcc.Spec.URL,
		string(usernameBytes),
		string(passwordBytes),
	)
	if err != nil {
		r.setDegradedCondition(tcc, "ClientCreationFailed",
			fmt.Sprintf("Failed to create qBittorrent client for %s: %v", tcc.Spec.URL, err))
		tcc.Status.Connected = false
		now := metav1.Now()
		tcc.Status.LastChecked = &now
		if statusErr := r.Status().Update(ctx, tcc); statusErr != nil {
			logger.Error(statusErr, "Failed to update TCC status")
		}
		return ctrl.Result{RequeueAfter: checkInterval}, nil
	}

	// 6. Health check towards qBittorrent server
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

	// 7. If previous checks passed, TCC is available
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

// Check if changed secret is referenced by any TCC and return reconcile request to enqueue
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

func (r *TorrentClientConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.TorrentClientConfiguration{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.findTCCForSecret)).
		Named("torrentclientconfiguration").
		Complete(r)
}
