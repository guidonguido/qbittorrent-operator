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

// TorrentClientConfigurationSpec defines the desired state of TorrentClientConfiguration.
type TorrentClientConfigurationSpec struct {
	// URL is the base URL of the qBittorrent WebUI (e.g., "http://qbittorrent:8080").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// CredentialsSecret references a Secret containing 'username' and 'password' keys.
	// +kubebuilder:validation:Required
	CredentialsSecret SecretReference `json:"credentialsSecret"`

	// Timeout is the HTTP client timeout for connecting to qBittorrent (e.g., "10s").
	// +kubebuilder:default="10s"
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// CheckInterval is how often the controller checks connectivity (e.g., "60s").
	// +kubebuilder:default="60s"
	// +optional
	CheckInterval string `json:"checkInterval,omitempty"`
}

// TorrentClientConfigurationStatus defines the observed state of TorrentClientConfiguration.
type TorrentClientConfigurationStatus struct {
	// Connected indicates whether the operator can currently reach qBittorrent.
	Connected bool `json:"connected,omitempty"`

	// LastChecked is the timestamp of the last connectivity check.
	LastChecked *metav1.Time `json:"lastChecked,omitempty"`

	// QBittorrentVersion is the version reported by the qBittorrent instance.
	QBittorrentVersion string `json:"qbittorrentVersion,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tcc
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.url"
// +kubebuilder:printcolumn:name="Connected",type="boolean",JSONPath=".status.connected"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TorrentClientConfiguration is the Schema for the torrentclientconfigurations API.
type TorrentClientConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TorrentClientConfigurationSpec   `json:"spec,omitempty"`
	Status TorrentClientConfigurationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TorrentClientConfigurationList contains a list of TorrentClientConfiguration.
type TorrentClientConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TorrentClientConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TorrentClientConfiguration{}, &TorrentClientConfigurationList{})
}
