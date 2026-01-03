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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModuleVersionSpec defines a specific version of a Kerrareg Module.
type ModuleVersionSpec struct {
	// The SHA256 checksum of the module as a base64 encoded string.
	Checksum *string `json:"checksum,omitempty"`
	// The name of the module archive file with its extension.
	// The file extension must be one of .zip or .tar.gz
	// since terraform and tofu currently only support these two
	// extension types.
	FileName *string `json:"fileName,omitempty"`
	// The reference to the Module resource
	ModuleConfig ModuleConfig `json:"moduleConfig"`
	// The version of the module
	Version string `json:"version"`
}

// ModuleConfig is the configuration settings passed down from the Module controller
// when it creates the ModuleVersion.
type ModuleConfig struct {
	// The Github client configuration settings
	GithubClientConfig GithubClientConfig `json:"githubClientConfig,omitempty"`
	// When true, enforces that the ChecksumSHA256 of the module archive
	// always matches the value stored in this field and in any destination storage config
	Immutable bool `json:"immutable"`
	// The name of the module
	Name string `json:"name"`
	// Owner of the Github repository
	RepoOwner string `json:"repoOwner"`
	// The external storage configuration settings
	StorageConfig StorageConfig `json:"storageConfig,omitempty"`
}

// StorageConfig is the configuration for where the module should be externally
// stored.
type StorageConfig struct {
	S3 *AmazonS3Config `json:"s3,omitempty"`
}

type AmazonS3Config struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key,omitempty"`
	Region string `json:"region"`
}

type GithubClientConfig struct {
	// This flag determines whether the GitHub client used to download modules
	// will be authenticated with a Github App. It's highly recommended
	// to enable this flag to avoid GitHub API rate limiting. When enabled, the namespace where the module is deployed
	// must contain a Secret named 'kerrareg-github-application-secret'. The secret must contain a githubAppID,
	// githubInstallID, and githubPrivateKey. The private key must be base64 encoded before being added
	// as data to the secret. When accessed, the controller will base64 decode the key to build an in-memory client
	// to authenticate with the Github API.
	UseAuthenticatedClient bool `json:"useAuthenticatedClient"`
}

type ModuleVersionStatus struct {
	Synced     bool   `json:"synced"`
	SyncStatus string `json:"syncStatus"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="FileName",type="string",JSONPath=".spec.fileName",description="The auto generated file name for the module version"
// +kubebuilder:printcolumn:name="Checksum",type="string",JSONPath=".spec.checksum",description="The checksum of the module version"

// ModuleVersion is the Schema for the ModuleVersion API
type ModuleVersion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleVersionSpec   `json:"spec,omitempty"`
	Status ModuleVersionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleVersionList contains a list of ModuleVersion.
type ModuleVersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModuleVersion `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModuleVersion{}, &ModuleVersionList{})
}
