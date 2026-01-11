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
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the module"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.moduleConfig.provider",description="The provider of the module"
// +kubebuilder:printcolumn:name="RepoURL",type="string",JSONPath=".spec.moduleConfig.repoUrl",description="The source repository URL of the module"
// +kubebuilder:printcolumn:name="StorageConfig",type="string",JSONPath=".spec.moduleConfig.storageConfig",description="The configuration for module storage"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Module has synced successfully"

// Module is the Schema for the Modules API.
type Module struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

// ModuleSpec defines the desired state of a Kerrareg Module.
type ModuleSpec struct {
	// A flag to force a module to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the module that will be used to create each ModuleVersion
	ModuleConfig types.ModuleConfig `json:"moduleConfig"`
	// The version of the module. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []types.ModuleVersion `json:"versions"`
}

// ModuleStatus defines the observed state of a module.
type ModuleStatus struct {
	// The latest available version of the module
	LatestVersion *string `json:"latestVersion,omitempty"`
	// The randomly generated filename with its file extension.
	FileName string `json:"fileName,omitempty"`
	// A flag to determine if the module has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the ModuleVersionRefs that have been successfully created by the controller
	ModuleVersionRefs map[string]*types.ModuleVersion `json:"moduleVersionRefs,omitempty"`
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
	SchemeBuilder.Register(&versionv1alpha1.Version{}, &versionv1alpha1.VersionList{})
}
