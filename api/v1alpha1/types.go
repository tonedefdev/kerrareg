package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:generate stringer -type=KerraregType
type KerraregType int

const (
	TypeModule KerraregType = iota
	TypeProvider
)

const (
	KerraregFinalizer                       = "kerrareg.io/finalizer"
	KerraregGithubSecretDataFieldAppID      = "githubAppID"
	KerraregGithubSecretDataFieldInstallID  = "githubInstallID"
	KerraregGithubSecretDataFieldPrivateKey = "githubPrivateKey"
	KerraregGithubSecretName                = "kerrareg-github-application-secret"
	KerraregModule                          = "Module"
	KerraregProvider                        = "Provider"
)

// DepotSpec defines the desired state of Depot.
type DepotSpec struct {
	// The configuration that should be applied to all modules that are part
	// of this Depot.
	GlobalConfig  *GlobalConfig  `json:"global,omitempty"`
	ModuleConfigs []ModuleConfig `json:"moduleConfigs"`
}

type GlobalConfig struct {
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	ModuleConfig       *ModuleConfig       `json:"moduleConfig,omitempty"`
	StorageConfig      *StorageConfig      `json:"storageConfig"`
}

// DepotStatus defines the observed state of Depot.
type DepotStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Depot is the Schema for the depots API.
type Depot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DepotSpec   `json:"spec,omitempty"`
	Status DepotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DepotList contains a list of Depot.
type DepotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Depot `json:"items"`
}

// ModuleConfig is the configuration settings for the Module and for each
// Version created by the Module controller.
type ModuleConfig struct {
	// The file format of the module
	// This must be one of 'zip' or 'tar'.
	FileFormat *string `json:"fileFormat,omitempty"`
	// The Github client configuration settings.
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// When true, enforces that the ChecksumSHA256 of the module archive
	// always matches the value stored in this field and in any destination storage config.
	Immutable *bool `json:"immutable,omitempty"`
	// The name of the module. If omitted, the name of the Module resource
	// is used in its place.
	Name *string `json:"name,omitempty"`
	// The main terraform or tofu provider required for this module.
	Provider string `json:"provider,omitempty"`
	// Owner of the Github repository.
	RepoOwner string `json:"repoOwner,omitempty"`
	// The full URL of the Github repository.
	RepoUrl *string `json:"repoUrl,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// A comma separated list of version constraints such as
	// '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0, != 1.0.2'. This field is only
	// respected by the Depot controller.
	VersionConstraints string `json:"versionConstraints,omitempty"`
}

type GithubClientConfig struct {
	// This flag determines whether the GitHub client used to download modules
	// will be authenticated with a Github App. It's highly recommended
	// to enable this flag to avoid GitHub API rate limiting. When enabled, the namespace where the Module resource exists
	// must contain a Secret named 'kerrareg-github-application-secret'. The secret must contain a githubAppID,
	// githubInstallID, and githubPrivateKey field. The private key must also be base64 encoded before being added
	// as data to the secret. When accessed, the controller will base64 decode the key to build an in-memory client
	// to authenticate with the Github API.
	UseAuthenticatedClient bool `json:"useAuthenticatedClient,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the module"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.moduleConfig.provider",description="The provider of the module"
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.moduleConfig.repoUrl",description="The source repository URL of the module"
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
	ModuleConfig ModuleConfig `json:"moduleConfig"`
	// The version of the module. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []ModuleVersion `json:"versions"`
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
	ModuleVersionRefs map[string]*ModuleVersion `json:"moduleVersionRefs,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleList contains a list of Module.
type ModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

// ModuleVersion holds details about the Version resource under management.
type ModuleVersion struct {
	// The randomly generated filename with its file extension.
	FileName *string `json:"fileName,omitempty"`
	// The name of the module.
	Name string `json:"name,omitempty"`
	// Whether the Version for the Module has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the module.
	Version string `json:"version,omitempty"`
}

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
	ModuleConfigRef *ModuleConfig `json:"moduleConfigRef,omitempty"`
	// The reference to the Provider resource's config.
	ProviderConfigRef *ProviderConfig `json:"providerConfigRef,omitempty"`
	// The type of resource. Either 'Module' or 'Provider'
	Type string `json:"type"`
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

// ProviderConfig is the configuration settings for the Provider and for each
// Version created by the Provider controller.
type ProviderConfig struct {
	// The name of the provider.
	Name *string `json:"name,omitempty"`
}

// The configuration settings for storing the module in an Amazon S3 bucket.
type AmazonS3Config struct {
	// The S3 bucket name.
	Bucket string `json:"bucket"`
	// The S3 bucket key, ie: 'my/bucket/prefix'
	// The file name will be automatically generated by the kerrareg-module-controller.
	Key *string `json:"key,omitempty"`
	// The AWS region for the bucket.
	Region string `json:"region"`
}

type AzureStorageConfig struct {
	// The Azure Storage Account name.
	AccountName string `json:"accountName"`
	// The Azure Storage Account URL.
	AccountUrl string `json:"accountUrl"`
	// The Azure subscription ID where the Azure Storage Account is located.
	SubscriptionID string `json:"subscriptionID"`
	// The Azure Resource Group where the Azure Storage Account is located.
	ResourceGroup string `json:"resourceGroup"`
}

type GoogleCloudStorageConfig struct {
	// The GCS bucket name.
	Bucket string `json:"bucket"`
}

// StorageConfig holds details about how to store a Version.
type StorageConfig struct {
	AzureStorage *AzureStorageConfig `json:"azureStorage,omitempty"`
	// The configuration settings for storing Versions on a local filesystem.
	FileSystem *FileSystemConfig `json:"fileSystem,omitempty"`
	// The configuration settings for storing Versions in an Amazon S3 bucket.
	S3 *AmazonS3Config `json:"s3,omitempty"`
	// The configuration settings for storing Versions in a Google Cloud Storage bucket.
	GCS *GoogleCloudStorageConfig `json:"gcs,omitempty"`
}

// The configuration settings for storing Versions on a local filesystem.
type FileSystemConfig struct {
	// The directory path on the file system where the Version will be stored.
	DirectoryPath *string `json:"directoryPath,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Depot{}, &DepotList{})
	SchemeBuilder.Register(&Module{}, &ModuleList{})
	SchemeBuilder.Register(&Version{}, &VersionList{})
}
