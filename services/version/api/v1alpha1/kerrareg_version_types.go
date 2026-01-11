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
	"kerrareg/api/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="The type of resource. Either 'Module' or 'Provider'"
// +kubebuilder:printcolumn:name="FileName",type="string",JSONPath=".spec.fileName",description="The auto generated file name for the Version"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Version has synced successfully"
// +kubebuilder:printcolumn:name="Checksum",type="string",JSONPath=".status.checksum",description="The base64 encoded SHA256 checksum of the file Version"

// Version is the Schema for the Version API.
type Version struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VersionSpec   `json:"spec,omitempty"`
	Status VersionStatus `json:"status,omitempty"`
}

// VersionSpec defines a specific version of a Kerrareg Module or Provider.
type VersionSpec struct {
	// The name of the file with its extension.
	// For a Module the file extension must be one of .zip or .tar.gz
	// since terraform/tofu currently only support these two
	// extension types.
	FileName *string `json:"fileName,omitempty"`
	// A flag to force a module version to synchronize.
	ForceSync bool `json:"forceSync,omitempty"`
	// The reference to the Module resource's config.
	ModuleConfigRef types.ModuleConfig `json:"moduleConfigRef,omitempty"`
	// The reference to the Provider resource's config.
	ProviderConfigRef types.ProviderConfig `json:"providerConfigRef,omitempty"`
	// The type of resource. Either 'Module' or 'Provider'
	Type types.KerraregType `json:"type"`
	// The version of the Module or Provider.
	Version string `json:"version"`
}

// VersionStatus defines the current status of the resource.
type VersionStatus struct {
	// The SHA256 checksum of the module as a base64 encoded string.
	Checksum *string `json:"checksum"`
	// A flag that determines whether the Version has been successfully reconciled.
	Synced bool `json:"synced"`
	// The Version's reconciliation status.
	SyncStatus string `json:"syncStatus"`
}

// +kubebuilder:object:root=true

// VersionList contains a list of Version.
type VersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Version `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Version{}, &VersionList{})
}
