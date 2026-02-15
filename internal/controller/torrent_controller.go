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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

type TorrentReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	ClientPool *qbittorrent.ClientPool
}

const (
	TypeAvailableTorrent = "Available"
	TypeDegradedTorrent  = "Degraded"
)

const TorrentFinalizer = "torrent.qbittorrent.io/finalizer"

// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents/finalizers,verbs=update
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrentclientconfigurations/status,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *TorrentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Torrent", "Request", req)

	// 1. Request contains only name and namespace,
	// need to fetch the full resource to get spec and status
	torrent := &torrentv1alpha1.Torrent{}
	if err := r.Get(ctx, req.NamespacedName, torrent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion
	if !torrent.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, torrent)
	}

	// 3. Finalizer is needed so the resource does not get deleted
	// before the torrent is removed from qBittorrent
	if !controllerutil.ContainsFinalizer(torrent, TorrentFinalizer) {
		logger.Info("Adding finalizer to Torrent", "Name", torrent.Name)
		controllerutil.AddFinalizer(torrent, TorrentFinalizer)
		if err := r.Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// 4. Resolve TCC and get qBittorrent client
	qbtClient, err := r.getQBTClient(ctx, torrent)
	if err != nil {
		logger.Error(err, "Failed to resolve qBittorrent client")
		r.setDegradedCondition(torrent, "ClientResolutionFailed", err.Error())
		if statusErr := r.Status().Update(ctx, torrent); statusErr != nil {
			logger.Error(statusErr, "Failed to update Torrent status")
		}
		r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "ClientResolutionFailed",
			"Failed to resolve qBittorrent client for Torrent %q: %v", torrent.Name, err)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// 5. Check if the torrent is already added in qBittorrent and update status accordingly
	logger.V(1).Info("Getting torrent hash from magnet URI", "MagnetURI", torrent.Spec.MagnetURI)
	hash, err := qbittorrent.GetTorrentHash(torrent.Spec.MagnetURI)
	if err != nil {
		r.setDegradedCondition(torrent, "FailedToGetTorrentHash", err.Error())
		if statusErr := r.Status().Update(ctx, torrent); statusErr != nil {
			logger.Error(statusErr, "Failed to update Torrent status")
		}
		r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "InvalidMagnetURI",
			"Failed to get torrent hash from magnet URI %q: %v", torrent.Spec.MagnetURI, err)
		logger.Error(err, "Failed to get torrent hash")
		return ctrl.Result{}, err
	}
	logger.V(1).Info("Torrent hash", "Hash", hash)

	torrentInfo, err := qbtClient.GetTorrentInfo(ctx, hash)
	if err != nil {
		logger.Error(err, "Failed to get Torrent info")
		r.setDegradedCondition(torrent, "FailedToGetTorrentInfo", err.Error())
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}
		r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "FailedToGetTorrentInfo",
			"Failed to get torrent info for Torrent %q: %v", torrent.Name, err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if torrentInfo == nil {
		logger.Info("Torrent not found in qBittorrent, adding it", "Name", torrent.Name)
		if err := qbtClient.AddTorrent(ctx, torrent.Spec.MagnetURI); err != nil {
			logger.Error(err, "Failed to add Torrent to qBittorrent")
			r.setDegradedCondition(torrent, "FailedToAddTorrent", err.Error())
			if err := r.Status().Update(ctx, torrent); err != nil {
				logger.Error(err, "Failed to update Torrent status")
			}
			r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "FailedToAddTorrent",
				"Failed to add Torrent %q to qBittorrent: %v", torrent.Name, err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		r.setAvailableCondition(torrent, "TorrentAdded", "Torrent added to qBittorrent")
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}

		r.Recorder.Eventf(torrent, corev1.EventTypeNormal, "Added",
			"Successfully added Torrent %q to qBittorrent", torrent.Name)
		logger.Info("Successfully added Torrent to qBittorrent", "Name", torrent.Name)

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// 6. If torrent already exists, update status
	updated := r.updateTorrentStatus(ctx, torrent, torrentInfo)
	if updated {
		logger.Info("Updating status reflecting the torrent info", "Name", torrent.Name)
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
			return ctrl.Result{}, err
		}
	}

	r.setAvailableCondition(torrent, "TorrentActive", "Torrent is active on qBittorrent")
	if err := r.Status().Update(ctx, torrent); err != nil {
		logger.Error(err, "Failed to update Torrent status")
	}

	// If success, reconcile every 15 seconds to keep status updated
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *TorrentReconciler) handleDeletion(ctx context.Context, torrent *torrentv1alpha1.Torrent) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling Torrent Deletion", "Name", torrent.Name)

	r.Recorder.Eventf(torrent, corev1.EventTypeNormal, "DeletionStarted", "Starting deletion of Torrent %q", torrent.Name)

	if torrent.Status.Hash != "" {
		// Resolve TCC to get a client for deletion
		qbtClient, err := r.getQBTClient(ctx, torrent)
		if err != nil {
			// In case of error, do not remove finalizer so we can retry deletion later, once the issue is resolved
			logger.Error(err, "Failed to resolve qBittorrent client for deletion, will retry")
			r.setDegradedCondition(torrent, "ClientResolutionFailed", fmt.Sprintf("Failed to resolve qBittorrent client for deletion: %v", err))
			if statusErr := r.Status().Update(ctx, torrent); statusErr != nil {
				logger.Error(statusErr, "Failed to update Torrent status")
			}
			r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "ClientResolutionFailed",
				"Failed to resolve qBittorrent client for deletion of Torrent %q: %v, will retry", torrent.Name, err)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		} else {
			deleteFiles := true
			if torrent.Spec.DeleteFilesOnRemoval != nil {
				deleteFiles = *torrent.Spec.DeleteFilesOnRemoval
			}

			logger.Info("Deleting Torrent from qBittorrent", "Name", torrent.Name)
			if err := qbtClient.DeleteTorrent(ctx, torrent.Status.Hash, deleteFiles); err != nil {
				logger.Error(err, "Failed to delete Torrent from qBittorrent")
				r.setDegradedCondition(torrent, "FailedToDeleteTorrent", err.Error())
				if err := r.Status().Update(ctx, torrent); err != nil {
					logger.Error(err, "Failed to update Torrent status")
				}
				r.Recorder.Eventf(torrent, corev1.EventTypeWarning, "FailedToDeleteTorrent",
					"Failed to delete Torrent %q from qBittorrent: %v", torrent.Name, err)
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
			r.Recorder.Eventf(torrent, corev1.EventTypeNormal, "Deleted",
				"Successfully deleted Torrent %q from qBittorrent", torrent.Name)
			logger.Info("Successfully deleted Torrent from qBittorrent", "Name", torrent.Name)
		}
	}

	controllerutil.RemoveFinalizer(torrent, TorrentFinalizer)
	if err := r.Update(ctx, torrent); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Finalizer removed from Torrent, resource will be deleted", "Name", torrent.Name)
	return ctrl.Result{}, nil
}

func (r *TorrentReconciler) getQBTClient(ctx context.Context, torrent *torrentv1alpha1.Torrent) (*qbittorrent.Client, error) {
	logger := log.FromContext(ctx)

	// 1. Get TCC
	var tcc *torrentv1alpha1.TorrentClientConfiguration

	if torrent.Spec.ClientConfigRef != nil {
		// 1.1. Get the referenced TCC
		tcc := &torrentv1alpha1.TorrentClientConfiguration{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      torrent.Spec.ClientConfigRef.Name,
			Namespace: torrent.Namespace,
		}, tcc); err != nil {
			return nil, fmt.Errorf("referenced TorrentClientConfiguration %q not found: %w",
				torrent.Spec.ClientConfigRef.Name, err)
		}
	}

	// 1.2. If no explicit reference, try to auto-discover the only TCC in the namespace
	tccList := &torrentv1alpha1.TorrentClientConfigurationList{}
	if err := r.List(ctx, tccList, client.InNamespace(torrent.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list TorrentClientConfigurations: %w", err)
	}

	switch len(tccList.Items) {
	case 0:
		return nil, fmt.Errorf("no TorrentClientConfiguration found in namespace %s", torrent.Namespace)
	case 1:
		logger.V(1).Info("Auto-discovered TCC", "name", tccList.Items[0].Name)
		tcc = &tccList.Items[0]
	default:
		return nil, fmt.Errorf("multiple TorrentClientConfigurations found in namespace %s; set spec.clientConfigRef to select one",
			torrent.Namespace)
	}

	// 2. TCC must be available to connect to qBittorrent
	availableCondition := meta.FindStatusCondition(tcc.Status.Conditions, TypeAvailableTCC)
	if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("TorrentClientConfiguration %q is not available", tcc.Name)
	}

	// 3. Set the discovered TCC in the status
	torrent.Status.ClientConfigurationName = tcc.Name

	// 4. Get credentials and the related connection
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tcc.Spec.CredentialsSecret.Name,
		Namespace: tcc.Namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get credentials secret %q: %w", tcc.Spec.CredentialsSecret.Name, err)
	}

	return r.ClientPool.GetOrCreate(
		ctx,
		tcc.Spec.URL,
		string(secret.Data["username"]),
		string(secret.Data["password"]),
	)
}

func (r *TorrentReconciler) setDegradedCondition(torrent *torrentv1alpha1.Torrent, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeDegradedTorrent,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&torrent.Status.Conditions, condition)
	meta.RemoveStatusCondition(&torrent.Status.Conditions, TypeAvailableTorrent)
}

func (r *TorrentReconciler) setAvailableCondition(torrent *torrentv1alpha1.Torrent, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeAvailableTorrent,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&torrent.Status.Conditions, condition)
	meta.RemoveStatusCondition(&torrent.Status.Conditions, TypeDegradedTorrent)
}

func (r *TorrentReconciler) updateTorrentStatus(ctx context.Context, torrent *torrentv1alpha1.Torrent, qbTorrent *qbittorrent.TorrentInfo) bool {
	logger := log.FromContext(ctx)
	updated := false

	if torrent.Status.Hash != qbTorrent.Hash {
		torrent.Status.Hash = qbTorrent.Hash
		updated = true
	}

	if torrent.Status.Name != qbTorrent.Name {
		torrent.Status.Name = qbTorrent.Name
		updated = true
	}

	if torrent.Status.State != qbTorrent.State {
		logger.Info("Torrent state changed",
			"old_state", torrent.Status.State,
			"new_state", qbTorrent.State)
		torrent.Status.State = qbTorrent.State
		updated = true
	}

	if torrent.Status.TotalSize != qbTorrent.TotalSize {
		torrent.Status.TotalSize = qbTorrent.TotalSize
		updated = true
	}

	if torrent.Status.ContentPath != qbTorrent.ContentPath {
		torrent.Status.ContentPath = qbTorrent.ContentPath
		updated = true
	}

	if torrent.Status.AddedOn != qbTorrent.AddedOn {
		torrent.Status.AddedOn = qbTorrent.AddedOn
		updated = true
	}

	if torrent.Status.TimeActive != qbTorrent.TimeActive {
		torrent.Status.TimeActive = qbTorrent.TimeActive
		updated = true
	}

	if torrent.Status.AmountLeft != qbTorrent.AmountLeft {
		torrent.Status.AmountLeft = qbTorrent.AmountLeft
		updated = true
	}

	if updated {
		logger.V(1).Info("Status fields updated", "hash", qbTorrent.Hash)
	}

	return updated
}

func (r *TorrentReconciler) findTorrentsForTCC(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)
	tcc, ok := obj.(*torrentv1alpha1.TorrentClientConfiguration)
	if !ok {
		return nil
	}

	torrentList := &torrentv1alpha1.TorrentList{}
	if err := r.List(ctx, torrentList, client.InNamespace(tcc.Namespace)); err != nil {
		logger.Error(err, "Failed to list Torrents for TCC mapping")
		return nil
	}

	var requests []reconcile.Request
	for _, torrent := range torrentList.Items {
		// Requeue reconciliation request for torrents using the updated TCC
		if (torrent.Spec.ClientConfigRef != nil && torrent.Spec.ClientConfigRef.Name == tcc.Name) ||
			(torrent.Spec.ClientConfigRef == nil && torrent.Status.ClientConfigurationName == tcc.Name) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      torrent.Name,
					Namespace: torrent.Namespace,
				},
			})
			continue
		}
	}
	return requests
}

func (r *TorrentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.Torrent{}).
		Watches(&torrentv1alpha1.TorrentClientConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.findTorrentsForTCC)).
		Named("torrent").
		Complete(r)
}
