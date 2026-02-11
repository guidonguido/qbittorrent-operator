package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TorrentServerSpec defines the desired state of TorrentServer.
type TorrentServerSpec struct {
	// Image is the qBittorrent container image.
	// +kubebuilder:default="lscr.io/linuxserver/qbittorrent:amd64-5.1.4"
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the number of replicas. Must be 0 or 1.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines resource requests/limits for the qBittorrent container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Env defines additional environment variables for the qBittorrent container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// ConfigStorage defines the PVC configuration for the /config volume.
	// If not provided, a default 1Gi PVC is created.
	// +optional
	ConfigStorage *StorageSpec `json:"configStorage,omitempty"`

	// DownloadVolumes references existing PVCs for download storage.
	// These PVCs are NOT created or owned by TorrentServer; they must already exist.
	// +optional
	DownloadVolumes []DownloadVolumeSpec `json:"downloadVolumes,omitempty"`

	// CredentialsSecret references a Secret containing 'username' and 'password' keys
	// for the qBittorrent WebUI. If not specified, a default Secret is auto-generated.
	// +optional
	CredentialsSecret *SecretReference `json:"credentialsSecret,omitempty"`

	// ServiceType is the Kubernetes Service type for the qBittorrent WebUI.
	// +kubebuilder:default="ClusterIP"
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// WebUIPort is the port the qBittorrent WebUI listens on.
	// +kubebuilder:default=8080
	// +optional
	WebUIPort int32 `json:"webUIPort,omitempty"`
}

// StorageSpec defines PVC configuration for config storage.
type StorageSpec struct {
	// StorageClassName is the name of the StorageClass to use.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// Size is the storage size (e.g., "1Gi").
	// +kubebuilder:default="1Gi"
	// +optional
	Size string `json:"size,omitempty"`

	// AccessModes for the PVC.
	// +kubebuilder:default={"ReadWriteOnce"}
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// DownloadVolumeSpec references an existing PVC and its mount path.
type DownloadVolumeSpec struct {
	// ClaimName is the name of an existing PVC.
	ClaimName string `json:"claimName"`

	// MountPath is the path inside the container where this PVC is mounted.
	MountPath string `json:"mountPath"`
}

// SecretReference is a reference to a Secret in the same namespace.
type SecretReference struct {
	// Name of the Secret.
	Name string `json:"name"`
}

// TorrentServerStatus defines the observed state of TorrentServer.
type TorrentServerStatus struct {
	// DeploymentName is the name of the managed Deployment.
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the managed Service.
	ServiceName string `json:"serviceName,omitempty"`

	// ConfigPVCName is the name of the managed config PVC.
	ConfigPVCName string `json:"configPVCName,omitempty"`

	// ClientConfigurationName is the name of the auto-created TorrentClientConfiguration.
	ClientConfigurationName string `json:"clientConfigurationName,omitempty"`

	// ReadyReplicas is the number of ready replicas.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// URL is the internal service URL for the qBittorrent WebUI.
	URL string `json:"url,omitempty"`

	// Conditions represent the latest available observations of the TorrentServer state.
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ts
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".status.url"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TorrentServer is the Schema for the torrentservers API.
type TorrentServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TorrentServerSpec   `json:"spec,omitempty"`
	Status TorrentServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TorrentServerList contains a list of TorrentServer.
type TorrentServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TorrentServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TorrentServer{}, &TorrentServerList{})
}
