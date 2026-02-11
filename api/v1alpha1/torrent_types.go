package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TorrentSpec defines the desired state of Torrent.
type TorrentSpec struct {
	// MagnetURI is the magnet link for the torrent to download.
	// +kubebuilder:validation:Required
	MagnetURI string `json:"magnet_uri"`

	// ClientConfigRef is an explicit reference to a TorrentClientConfiguration in the same namespace.
	// If not set, the controller will auto-discover a TCC in the namespace
	// (exactly one must exist for auto-discovery to succeed).
	// +optional
	ClientConfigRef *LocalObjectReference `json:"clientConfigRef,omitempty"`

	// DeleteFilesOnRemoval controls whether downloaded files are deleted
	// when the Torrent resource is deleted.
	// +kubebuilder:default=true
	// +optional
	DeleteFilesOnRemoval *bool `json:"deleteFilesOnRemoval,omitempty"`
}

// LocalObjectReference is a reference to an object in the same namespace.
type LocalObjectReference struct {
	// Name of the referenced object.
	Name string `json:"name"`
}

// TorrentStatus defines the observed state of Torrent.
type TorrentStatus struct {
	ContentPath string `json:"content_path,omitempty"`
	AddedOn     int64  `json:"added_on,omitempty"`
	State       string `json:"state,omitempty"`
	TotalSize   int64  `json:"total_size,omitempty"`
	Name        string `json:"name,omitempty"`
	TimeActive  int64  `json:"time_active,omitempty"`
	AmountLeft  int64  `json:"amount_left,omitempty"`
	Hash        string `json:"hash,omitempty"`

	// ClientConfigurationName is the resolved TCC name being used.
	ClientConfigurationName string `json:"clientConfigurationName,omitempty"`

	// Conditions represent the latest available observations of a torrent's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=to
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".status.name"
// +kubebuilder:printcolumn:name="Size",type="string",JSONPath=".status.total_size"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.amount_left"

// Torrent is the Schema for the torrents API.
type Torrent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TorrentSpec   `json:"spec,omitempty"`
	Status TorrentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TorrentList contains a list of Torrent.
type TorrentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Torrent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Torrent{}, &TorrentList{})
}
