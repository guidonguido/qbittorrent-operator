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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TorrentSpec defines the desired state of Torrent.
// This is what users will define in their YAML
type TorrentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	MagnetURI string `json:"magnet_uri,omitempty"`
}

// TorrentStatus defines the observed state of Torrent.
// This is what the operator updates
type TorrentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ContentPath string `json:"content_path,omitempty"`
	AddedOn     int64  `json:"added_on,omitempty"`
	State       string `json:"state,omitempty"`
	TotalSize   int64  `json:"total_size,omitempty"`
	Name        string `json:"name,omitempty"`
	TimeActive  int64  `json:"time_active,omitempty"`
	AmountLeft  int64  `json:"amount_left,omitempty"`
	Hash        string `json:"hash,omitempty"`

	// Conditions represent the latest available observations of a torrent's current state
	// Standard Kubernetes pattern for representing status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
//+kubebuilder:printcolumn:name="Name",type="string",JSONPath=".status.name"
//+kubebuilder:printcolumn:name="Size",type="string",JSONPath=".status.total_size"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.amount_left"

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
