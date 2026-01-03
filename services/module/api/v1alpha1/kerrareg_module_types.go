/*
Copyright 2025 Anthony Owens.

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
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModuleSpec defines the desired state of a Kerrareg Module.
type ModuleSpec struct {
	// The file format of the module
	// This must be one of 'zip' or 'tar'
	FileFormat string `json:"fileFormat"`
	// When true, enforces that the ChecksumSHA256 of the module archive
	// always matches the value stored in this field and in any destination storage config
	Immutable bool `json:"immutable"`
	// The configuration settings for the Github API client
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The main terraform provider required for this module
	Provider string `json:"provider"`
	// The owner of the Github repository
	RepoOwner string `json:"repoOwner"`
	// The full URL of the Github repository
	RepoUrl string `json:"repoUrl"`
	// The configuration settings for storing the module
	StorageConfig StorageConfig `json:"storageConfig,omitempty"`
	// The version of the module. This should be a semantic version tag. For example, v1.0.0, or 1.0.0.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []ModuleVersion `json:"versions"`
}

type ModuleVersion struct {
	Version string `json:"version"`
}

type ModuleVersionRef struct {
	Name     string `json:"name"`
	FileName string `json:"fileName"`
}

type GithubClientConfig struct {
	// This flag determines whether the GitHub client used to download modules
	// will be authenticated with a Github App. It's highly recommended
	// to enable this flag to avoid GitHub API rate limiting. When enabled, the namespace where the module is deployed
	// must contain a Secret named 'kerrareg-github-application-secret'. The secret must contain a githubAppID,
	// githubInstallID, and githubPrivateKey. The private key must be base64 encoded before being added
	// as data to the secret. When accessed, the controller will base64 decode the key to build an in-memory client
	// to authenticate with the Github API.
	UseAuthenticatedClient bool `json:"useAuthenticatedClient,omitempty"`
}

type StorageConfig struct {
	// The configuration settings for storing the module in an Amazon S3 bucket
	S3 *AmazonS3Config `json:"s3,omitempty"`
}

type AmazonS3Config struct {
	// The S3 bucket name
	Bucket string `json:"bucket"`
	// The AWS region for the bucket
	Region string `json:"region"`
}

// ModuleStatus defines the observed state of a module.
type ModuleStatus struct {
	// A flag to determine if the module has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the ModuleVersionRefs that have been successfully created by the controller
	ModuleVersionRefs map[string]ModuleVersionRef `json:"moduleVersionRefs,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider",description="The provider of the module"
// +kubebuilder:printcolumn:name="RepoURL",type="string",JSONPath=".spec.repoUrl",description="The source repository URL of the module"
// +kubebuilder:printcolumn:name="StorageConfig",type="string",JSONPath=".spec.storageConfig",description="The configuration for module storage"

// Module is the Schema for the Modules API.
type Module struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleList contains a list of Module.
type ModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Module{}, &ModuleList{})
	SchemeBuilder.Register(&versionv1alpha1.ModuleVersion{}, &versionv1alpha1.ModuleVersionList{})
}
