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

// ModuleSpec defines the desired state of a Kerrareg Module.
type ModuleSpec struct {
	Provider      string          `json:"provider"`
	RepoUrl       string          `json:"repoUrl"`
	Versions      []ModuleVersion `json:"versions"`
	StorageConfig StorageConfig   `json:"storageConfig,omitempty"`
}

type StorageConfig struct {
	S3 AmazonS3Config `json:"s3,omitempty"`
}

type AmazonS3Config struct {
	Bucket string `json:"bucket"`
	Region string `json:"region"`
}

type ModuleVersion struct {
	Checksum string        `json:"checksum,omitempty"`
	Version  string        `json:"version"`
	Storage  ModuleStorage `json:"storage,omitempty"`
}

// ModuleStatus defines the observed state of Module.
type ModuleStatus struct {
	Synced bool `json:"synced"`
}

type ModuleStorage struct {
	S3 AmazonS3 `json:"s3,omitempty"`
}

type AmazonS3 struct {
	Config AmazonS3Config `json:"config,omitempty"`
	Key    string         `json:"key"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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

// ProviderSpec defines the desired state of a Kerrareg Provider.
type ProviderSpec struct {
	Checksum string `json:"checksum"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Version  string `json:"version"`
}

// ProviderStatus defines the observed state of Provider.
type ProviderStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Provider is the Schema for the Providers API.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

// ModuleDepotSpec
type ModuleDepotSpec struct {
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	RepoUrl     string `json:"repoUrl"`
	Owner       string `json:"owner"`
	SemVerMatch string `json:"semVerMatch"`
}

// ModuleDepotStatus defines the observed state of a ModuleDepot.
type ModuleDepotStatus struct {
	Modules []Module `json:"modules"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Provider is the Schema for the Providers API.
type ModuleDepot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleList contains a list of Module.
type ModuleDepoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModuleDepot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Module{}, &ModuleList{})
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}
