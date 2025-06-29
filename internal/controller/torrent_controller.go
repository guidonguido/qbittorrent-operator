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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	torrentv1alpha1 "github.com/guidonguido/qbittorrent-operator/api/v1alpha1"
	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

// TorrentReconciler reconciles a Torrent object
type TorrentReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	QBTClient *qbittorrent.Client
}

// Conditions pattern
// Available: Torrent resource is working as expected
// Degraded: There is an error in the torrent
// Condition types for Torrent status
const (
	// Status used to indicate if the torrent is available in qBittorrent
	TypeAvailableTorrent = "Available"
	// Status used to indicate if the torrent is degraded
	TypeDegradedTorrent = "Degraded"
)

// Finalizer name for cleanup
const TorrentFinalizer = "torrent.qbittorrent.io/finalizer"

// RBAC rules for the controller
// Allow the controller to manage the Torrent resource
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents,verbs=get;list;watch;create;update;patch;delete
// Allow the controller to manage the Torrent status
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents/status,verbs=get;update;patch
// Allow the controller to manage the Torrent finalizers
// +kubebuilder:rbac:groups=torrent.qbittorrent.io,resources=torrents/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Torrent object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *TorrentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Torrent", "Request", req)

	// Step 1: Get the Torrent Resource
	torrent := &torrentv1alpha1.Torrent{}
	if err := r.Get(ctx, req.NamespacedName, torrent); err != nil {
		logger.Error(err, "Failed to get Torrent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 2: Check if the Torrent Resource is marked for deletion
	if !torrent.DeletionTimestamp.IsZero() {
		// Step 2.1: Delete the Torrent Resource from qBittorrent
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

	// Step 2.2: Delete the Torrent Resource from qBittorrent
	if torrent.Status.Hash != "" {
		logger.Info("Deleting Torrent from qBittorrent", "Name", torrent.Name)

		// Delete the Torrent Resource from qBittorrent and delete the files by default
		if err := r.QBTClient.DeleteTorrent(ctx, torrent.Status.Hash, true); err != nil {
			logger.Error(err, "Failed to delete Torrent from qBittorrent")

			// Update resource status to reflect the error
			r.setDegradedCondition(torrent, "FailedToDeleteTorrent", err.Error())
			if err := r.Status().Update(ctx, torrent); err != nil {
				logger.Error(err, "Failed to update Torrent status")
			}

			// Retry after 10 seconds
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		logger.Info("Successfully deleted Torrent from qBittorrent", "Name", torrent.Name)
	}

	// Remove the finalizer from the Torrent Resource
	// so that kubernetes can delete the resource
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

	logger.V(1).Info("Getting torrent hash from magnet URI", "MagnetURI", torrent.Spec.MagnetURI)
	hash, err := qbittorrent.GetTorrentHash(torrent.Spec.MagnetURI)
	if err != nil {
		logger.Error(err, "Failed to get torrent hash")
		return ctrl.Result{}, err
	}
	logger.V(1).Info("Torrent hash", "Hash", hash)

	// Step 4.1: Check if the Torrent Resource exists in qBittorrent
	torrentInfo, err := r.QBTClient.GetTorrentInfo(ctx, hash)
	if err != nil {
		logger.Error(err, "Failed to get Torrent info")

		// Update resource status to reflect the error
		r.setDegradedCondition(torrent, "FailedToGetTorrentInfo", err.Error())
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}

		// Retry after 10 seconds
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Step 4.2: Check if the Torrent Resource exists in qBittorrent
	if torrentInfo == nil {
		logger.Info("Torrent not found in qBittorrent, adding it", "Name", torrent.Name)

		// Add the Torrent Resource to qBittorrent
		if err := r.QBTClient.AddTorrent(ctx, torrent.Spec.MagnetURI); err != nil {
			logger.Error(err, "Failed to add Torrent to qBittorrent")

			// Update resource status to reflect the error
			r.setDegradedCondition(torrent, "FailedToAddTorrent", err.Error())
			if err := r.Status().Update(ctx, torrent); err != nil {
				logger.Error(err, "Failed to update Torrent status")
			}

			// Retry after 10 seconds
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Step 4.3: Update status reflecting the torrent info and set the available condition
		r.setAvailableCondition(torrent, "TorrentAdded", "Torrent added to qBittorrent")
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
		}

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Step 4.3: Update status reflecting the torrent info
	updated := r.updateTorrentStatus(ctx, torrent, torrentInfo)

	if updated {
		logger.Info("Updating status reflecting the torrent info", "Name", torrent.Name)
		if err := r.Status().Update(ctx, torrent); err != nil {
			logger.Error(err, "Failed to update Torrent status")
			return ctrl.Result{}, err
		}
	}

	// Step 4.4: Set success condition
	// Update resource status to reflect the success
	r.setAvailableCondition(torrent, "TorrentActive", "Torrent is active on qBittorrent")
	if err := r.Status().Update(ctx, torrent); err != nil {
		logger.Error(err, "Failed to update Torrent status")
	}

	// Step 4.5: Return success and requeue after 30 seconds
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// set the Degraded condition to True
func (r *TorrentReconciler) setDegradedCondition(torrent *torrentv1alpha1.Torrent, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeDegradedTorrent,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&torrent.Status.Conditions, condition)

	// Remove available condition if it exists
	meta.RemoveStatusCondition(&torrent.Status.Conditions, TypeAvailableTorrent)
}

// set the Available condition to True
func (r *TorrentReconciler) setAvailableCondition(torrent *torrentv1alpha1.Torrent, reason, message string) {
	condition := metav1.Condition{
		Type:               TypeAvailableTorrent,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&torrent.Status.Conditions, condition)

	// Remove degraded condition if it exists
	meta.RemoveStatusCondition(&torrent.Status.Conditions, TypeDegradedTorrent)
}

// updateTorrentStatus updates the torrent status from qBittorrent data
func (r *TorrentReconciler) updateTorrentStatus(ctx context.Context, torrent *torrentv1alpha1.Torrent, qbTorrent *qbittorrent.TorrentInfo) bool {
	logger := log.FromContext(ctx)
	updated := false

	// Compare and update each field
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

// SetupWithManager sets up the controller with the Manager.
func (r *TorrentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&torrentv1alpha1.Torrent{}).
		Named("torrent").
		Complete(r)
}
