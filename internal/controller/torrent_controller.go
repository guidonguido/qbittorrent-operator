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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

// TorrentReconciler reconciles a Torrent object
type TorrentReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientPool *qbittorrent.ClientPool
}

// Condition types for Torrent status
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

func (r *TorrentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Torrent", "Request", req)

	// Step 1: Get the Torrent Resource
	torrent := &torrentv1alpha1.Torrent{}
	if err := r.Get(ctx, req.NamespacedName, torrent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 2: Check if the Torrent Resource is marked for deletion
	if !torrent.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, torrent)
	}

	// Step 3: Add Finalizer to the Torrent Resource
	if !controllerutil.ContainsFinalizer(torrent, TorrentFinalizer) {
		logger.Info("Adding finalizer to Torrent", "Name", torrent.Name)
		controllerutil.AddFinalizer(torrent, TorrentFinalizer)
		if err := r.Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Step 4: Execute the Reconciliation logic
	return r.reconcile(ctx, torrent)
}

func (r *TorrentReconciler) handleDeletion(ctx context.Context, torrent *torrentv1alpha1.Torrent) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling Torrent Deletion", "Name", torrent.Name)

	if torrent.Status.Hash != "" {
		// Resolve TCC to get a client for deletion
		qbtClient, err := r.getQBTClient(ctx, torrent)
		if err != nil {
			logger.Error(err, "Failed to get qBittorrent client for deletion, removing finalizer anyway")
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
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
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

func (r *TorrentReconciler) reconcile(ctx context.Context, torrent *torrentv1alpha1.Torrent) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Torrent", "Name", torrent.Name)

	// Step 4.0: Resolve TCC and get qBittorrent client
	qbtClient, err := r.getQBTClient(ctx, torrent)
	if err != nil {
		logger.Error(err, "Failed to resolve qBittorrent client")
		r.setDegradedCondition(torrent, "ClientResolutionFailed", err.Error())
		if statusErr := r.Status().Update(ctx, torrent); statusErr != nil {
			logger.Error(statusErr, "Failed to update Torrent status")
		}
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// Step 4.1: Get torrent hash from magnet URI
	logger.V(1).Info("Getting torrent hash from magnet URI", "MagnetURI", torrent.Spec.MagnetURI)
	hash, err := qbittorrent.GetTorrentHash(torrent.Spec.MagnetURI)
	if err != nil {
		logger.Error(err, "Failed to get torrent hash")
		return ctrl.Result{}, err
	}
	logger.V(1).Info("Torrent hash", "Hash", hash)

	// Step 4.2: Check if the Torrent Resource exists in qBittorrent
	torrentInfo, err := qbtClient.GetTorrentInfo(ctx, hash)
	if err != nil {
		logger.Error(err, "Failed to get Torrent info")
		r.setDegradedCondition(torrent, "FailedToGetTorrentInfo", err.Error())
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 4.3: Add torrent if not found
	if torrentInfo == nil {
		logger.Info("Torrent not found in qBittorrent, adding it", "Name", torrent.Name)
		if err := qbtClient.AddTorrent(ctx, torrent.Spec.MagnetURI); err != nil {
			logger.Error(err, "Failed to add Torrent to qBittorrent")
			r.setDegradedCondition(torrent, "FailedToAddTorrent", err.Error())
			if err := r.Status().Update(ctx, torrent); err != nil {
				logger.Error(err, "Failed to update Torrent status")
			}
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		r.setAvailableCondition(torrent, "TorrentAdded", "Torrent added to qBittorrent")
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Step 4.4: Update status reflecting the torrent info
	updated := r.updateTorrentStatus(ctx, torrent, torrentInfo)
	if updated {
		logger.Info("Updating status reflecting the torrent info", "Name", torrent.Name)
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
			return ctrl.Result{}, err
		}
	}

	// Step 4.5: Set success condition
	r.setAvailableCondition(torrent, "TorrentActive", "Torrent is active on qBittorrent")
	if err := r.Status().Update(ctx, torrent); err != nil {
		logger.Error(err, "Failed to update Torrent status")
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// resolveClientConfig finds the TCC for this Torrent (explicit ref or auto-discovery).
func (r *TorrentReconciler) resolveClientConfig(ctx context.Context, torrent *torrentv1alpha1.Torrent) (*torrentv1alpha1.TorrentClientConfiguration, error) {
	logger := log.FromContext(ctx)

	if torrent.Spec.ClientConfigRef != nil {
		// Explicit reference
		tcc := &torrentv1alpha1.TorrentClientConfiguration{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      torrent.Spec.ClientConfigRef.Name,
			Namespace: torrent.Namespace,
		}, tcc); err != nil {
			return nil, fmt.Errorf("referenced TorrentClientConfiguration %q not found: %w",
				torrent.Spec.ClientConfigRef.Name, err)
		}
		return tcc, nil
	}

	// Auto-discovery: list all TCCs in the namespace
	tccList := &torrentv1alpha1.TorrentClientConfigurationList{}
	if err := r.List(ctx, tccList, client.InNamespace(torrent.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list TorrentClientConfigurations: %w", err)
	}

	switch len(tccList.Items) {
	case 0:
		return nil, fmt.Errorf("no TorrentClientConfiguration found in namespace %s", torrent.Namespace)
	case 1:
		logger.V(1).Info("Auto-discovered TCC", "name", tccList.Items[0].Name)
		return &tccList.Items[0], nil
	default:
		return nil, fmt.Errorf("multiple TorrentClientConfigurations found in namespace %s; set spec.clientConfigRef to select one",
			torrent.Namespace)
	}
}

// getQBTClient resolves the TCC, reads its Secret, and returns a pooled client.
func (r *TorrentReconciler) getQBTClient(ctx context.Context, torrent *torrentv1alpha1.Torrent) (*qbittorrent.Client, error) {
	tcc, err := r.resolveClientConfig(ctx, torrent)
	if err != nil {
		return nil, err
	}

	// Check if TCC is available
	availableCondition := meta.FindStatusCondition(tcc.Status.Conditions, TypeAvailableTCC)
	if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
		return nil, fmt.Errorf("TorrentClientConfiguration %q is not available", tcc.Name)
	}

	// Update the resolved TCC name in torrent status
	torrent.Status.ClientConfigurationName = tcc.Name

	// Read credentials from the referenced Secret
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tcc.Spec.CredentialsSecret.Name,
		Namespace: tcc.Namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get credentials secret %q: %w", tcc.Spec.CredentialsSecret.Name, err)
	}

	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	key := tcc.Namespace + "/" + tcc.Name

	return r.ClientPool.GetOrCreate(ctx, key, tcc.Spec.URL, username, password)
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

// updateTorrentStatus updates the torrent status from qBittorrent data
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

// findTorrentsForTCC maps TCC changes to Torrent resources that reference them.
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
		// Re-reconcile torrents that explicitly reference this TCC
		if torrent.Spec.ClientConfigRef != nil && torrent.Spec.ClientConfigRef.Name == tcc.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      torrent.Name,
					Namespace: torrent.Namespace,
				},
			})
			continue
		}
		// Re-reconcile torrents using auto-discovery (no explicit ref)
		if torrent.Spec.ClientConfigRef == nil {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      torrent.Name,
					Namespace: torrent.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *TorrentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.Torrent{}).
		// Reconcile Torrents when their referenced TCC changes
		Watches(&torrentv1alpha1.TorrentClientConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.findTorrentsForTCC)).
		Named("torrent").
		Complete(r)
}
