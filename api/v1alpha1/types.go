package v1alpha1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:generate stringer -type=OpenDepotType
type OpenDepotType int

const (
	TypeModule OpenDepotType = iota
	TypeProvider
)

const (
	OpenDepotFinalizer                       = "opendepot.defdev.io/finalizer"
	OpenDepotGithubSecretDataFieldAppID      = "githubAppID"
	OpenDepotGithubSecretDataFieldInstallID  = "githubInstallID"
	OpenDepotGithubSecretDataFieldPrivateKey = "githubPrivateKey"
	OpenDepotGithubSecretName                = "opendepot-github-application-secret"
	OpenDepotModule                          = "Module"
	OpenDepotProvider                        = "Provider"
	OpenTofuRegistryHost                     = "registry.opentofu.org"
	TerraformRegistryHost                    = "registry.terraform.io"
)

// DepotSpec defines the desired state of Depot.
type DepotSpec struct {
	// The configuration that should be applied to all modules that are part
	// of this Depot.
	GlobalConfig *GlobalConfig `json:"global,omitempty"`
	// The module configuration and version details for each module that should be managed by the Depot controller.
	ModuleConfigs []ModuleConfig `json:"moduleConfigs,omitempty"`
	// The provider configuration and version details for each provider that should be managed by the Depot controller.
	ProviderConfigs []ProviderConfig `json:"providerConfigs,omitempty"`
	// The polling interval in minutes for how often the Depot controller should check for new versions of the modules it manages.
	// If not specified, the default is 0.
	PollingIntervalMinutes *int `json:"pollingIntervalMinutes,omitempty"`
}

// Defines the desired config of all OpenDepot modules managed by the Depot controller.
type GlobalConfig struct {
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	ModuleConfig       *ModuleConfig       `json:"moduleConfig,omitempty"`
	StorageConfig      *StorageConfig      `json:"storageConfig"`
}

// DepotStatus defines the observed state of Depot.
type DepotStatus struct {
	// The list of Module resource names created and managed by this Depot.
	Modules []string `json:"modules,omitempty"`
	// The list of Provider resource names created and managed by this Depot.
	Providers []string `json:"providers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="GlobalConfig",type="string",JSONPath=".spec.globalConfig",description="The global configuration applied to all modules managed by this Depot"

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
	// The number of versions to keep stored in the registry at any given time.
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
}

type GithubClientConfig struct {
	// This flag determines whether the GitHub client used to download modules
	// will be authenticated with a Github App. It's highly recommended
	// to enable this flag to avoid GitHub API rate limiting. When enabled, the namespace where the Module resource exists
	// must contain a Secret named 'opendepot-github-application-secret'. The secret must contain a githubAppID,
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

// ModuleSpec defines the desired state of a OpenDepot Module.
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

// ProviderConfig is the configuration settings for the Provider and for each Version created by the Provider controller.
type ProviderConfig struct {
	// The name of the provider. If omitted, the name of the Provider resource
	// is used in its place.
	Name *string `json:"name,omitempty"`
	// The namespace (organization) of the provider in the configured upstream registry,
	// e.g. 'hashicorp', 'integrations', 'DataDog'. Defaults to 'hashicorp' when omitted,
	// preserving backwards compatibility for existing Provider resources.
	Namespace *string `json:"namespace,omitempty"`
	// The canonical registry used to discover and download this provider and exposed by the Provider Network Mirror Protocol.
	// Defaults to registry.opentofu.org when omitted.
	// +kubebuilder:validation:Enum=registry.opentofu.org;registry.terraform.io
	// +kubebuilder:default=registry.opentofu.org
	UpstreamRegistry *string `json:"upstreamRegistry,omitempty"`
	// The OS(s) that the provider supports. This is used to set the 'os' constraint in the provider's versions.
	OperatingSystems []string `json:"operatingSystems,omitempty"`
	// The architecture(s) that the provider supports. This is used to set the 'arch' constraint in the provider's versions.
	Architectures []string `json:"architectures,omitempty"`
	// The Github client configuration settings for source scanning. When set with
	// useAuthenticatedClient: true, the version controller will use a GitHub App to
	// authenticate requests when fetching the provider's go.mod for source scanning.
	// This is recommended for private source repositories and to avoid GitHub API rate limiting.
	// The namespace where the Version resource exists must contain a Secret named
	// 'opendepot-github-application-secret' with githubAppID, githubInstallID, and
	// githubPrivateKey fields (private key must be base64 encoded).
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The URL of the provider's source repository on GitHub, e.g. 'https://github.com/hashicorp/terraform-provider-aws'.
	// When omitted, OpenDepot looks up the repository from the OpenTofu registry (api.opentofu.org).
	// If the registry lookup fails, it falls back to 'github.com/{namespace}/terraform-provider-{name}'.
	// If the repository cannot be resolved, scanning falls back to binary-only mode.
	SourceRepository *string `json:"sourceRepository,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// The version history limit for the provider.
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
	// A comma-separated list of version constraints such as
	// '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0'. This field is only
	// respected by the Depot controller.
	VersionConstraints string `json:"versionConstraints,omitempty"`
}

// ProviderUpstreamRegistry returns the configured canonical provider registry.
func ProviderUpstreamRegistry(config *ProviderConfig) string {
	if config != nil && config.UpstreamRegistry != nil {
		upstreamRegistry := strings.TrimSpace(*config.UpstreamRegistry)

		if upstreamRegistry != "" {
			return upstreamRegistry
		}
	}

	return OpenTofuRegistryHost
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the provider"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.providerConfig.name",description="The name of the provider"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Provider has synced successfully"

// Provider is the Schema for the Providers API.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// ProviderSpec defines the desired state of a OpenDepot Provider.
type ProviderSpec struct {
	// A flag to force a provider to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the provider that will be used to create each ProviderVersion
	ProviderConfig ProviderConfig `json:"providerConfig"`
	// The version of the provider. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []ProviderVersion `json:"versions"`
}

// SecurityFinding represents a single vulnerability finding from a Trivy scan.
type SecurityFinding struct {
	// The CVE or GHSA identifier for the vulnerability.
	VulnerabilityID string `json:"vulnerabilityID"`
	// The name of the package containing the vulnerability.
	PkgName string `json:"pkgName"`
	// The version of the package currently in use.
	InstalledVersion string `json:"installedVersion"`
	// The minimum version of the package that resolves the vulnerability, if known.
	FixedVersion string `json:"fixedVersion,omitempty"`
	// The severity of the vulnerability: CRITICAL, HIGH, MEDIUM, LOW, or UNKNOWN.
	Severity string `json:"severity"`
	// A short description of the vulnerability.
	Title string `json:"title,omitempty"`
}

// SourceScan holds the results of a Trivy source scan. Used for both module IaC (HCL filesystem)
// scans and provider go.mod dependency scans. Stored on VersionStatus so each Version CR carries
// its own result; the JSON key is "sourceScan".
type SourceScan struct {
	// The RFC3339 timestamp at which the source scan completed.
	ScannedAt string `json:"scannedAt"`
	// The list of findings produced by the scan.
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// BinaryScan holds the results of a Trivy gobinary scan for a specific provider artifact.
// Stored on VersionStatus because each OS/arch binary may embed different Go stdlib versions
// or runtime dependencies; the JSON key is "binaryScan".
type BinaryScan struct {
	// The RFC3339 timestamp at which the binary scan completed.
	ScannedAt string `json:"scannedAt"`
	// The list of vulnerabilities found in the compiled provider binary.
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// ProviderStatus defines the observed state of a provider.
type ProviderStatus struct {
	// The latest available version of the provider
	LatestVersion *string `json:"latestVersion,omitempty"`
	// The randomly generated filename with its file extension.
	FileName string `json:"fileName,omitempty"`
	// A flag to determine if the provider has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the ProviderVersionRefs that have been successfully created by the controller
	ProviderVersionRefs map[string]*ProviderVersion `json:"providerVersionRefs,omitempty"`
	// ResolvedSourceRepository is the VCS source URL discovered by the version controller
	// from the OpenTofu registry (api.opentofu.org). Populated automatically on first scan;
	// spec.providerConfig.sourceRepository takes precedence if set.
	ResolvedSourceRepository string `json:"resolvedSourceRepository,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

// ProviderVersion holds details about the Version resource under management.
type ProviderVersion struct {
	// The system architecture this Version of the Provider supports.
	Architecture string `json:"architecture,omitempty"`
	// The name of the provider.
	Name string `json:"name,omitempty"`
	// The operating system this Version of the Provider supports.
	OperatingSystem string `json:"operatingSystem,omitempty"`
	// Whether the Version for the Provider has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the provider.
	Version string `json:"version,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="The type of resource. Either 'Module' or 'Provider'"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Version has synced successfully"
// +kubebuilder:printcolumn:name="Checksum",type="string",JSONPath=".status.checksum",description="The base64 encoded SHA256 checksum of the file Version"

// Version is the Schema for the Version API.
type Version struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VersionSpec   `json:"spec,omitempty"`
	Status VersionStatus `json:"status,omitempty"`
}

// VersionSpec defines a specific version of a OpenDepot Module or Provider.
type VersionSpec struct {
	// The system architecture this Version of the Provider supports.
	Architecture string `json:"architecture,omitempty"`
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
	// The operating system this Version of the Provider supports.
	OperatingSystem string `json:"operatingSystem,omitempty"`
	// The type of resource. Either 'Module' or 'Provider'
	Type string `json:"type"`
	// The version of the Module or Provider.
	Version string `json:"version"`
}

// VersionStatus defines the current status of the resource.
type VersionStatus struct {
	// The SHA256 checksum of the module as a base64 encoded string.
	Checksum *string `json:"checksum,omitempty"`
	// A flag that determines whether the Version has been successfully reconciled.
	Synced bool `json:"synced"`
	// The Version's reconciliation status.
	SyncStatus string `json:"syncStatus"`
	// ArchiveSizeBytes is the size in bytes of the stored archive, set by the Version
	// controller after a successful PutObject. Nil when the archive has not yet been uploaded.
	// +optional
	ArchiveSizeBytes *int64 `json:"archiveSizeBytes,omitempty"`
	// The binary vulnerability scan result for this specific provider artifact.
	// Only populated for provider Version resources when scanning is enabled.
	BinaryScan *BinaryScan `json:"binaryScan,omitempty"`
	// The source vulnerability scan result for this Version. Populated for provider Versions
	// (go.mod scan) and module Versions (IaC filesystem scan) when scanning is enabled.
	SourceScan *SourceScan `json:"sourceScan,omitempty"`
	// ReadmeConfigMapRef references the ConfigMap holding this module Version's base64
	// encoded README.md content. Only populated for module Version resources when a
	// README could be resolved from Github or the module archive.
	ReadmeConfigMapRef *ReadmeConfigMapRef `json:"readmeConfigMapRef,omitempty"`
}

// ReadmeConfigMapRef references the ConfigMap and data key holding a module Version's
// base64 encoded README.md content.
type ReadmeConfigMapRef struct {
	// The name of the ConfigMap holding the README content.
	Name string `json:"name"`
	// The key within the ConfigMap's data holding the base64 encoded README content.
	Key string `json:"key"`
}

// +kubebuilder:object:root=true

// VersionList contains a list of Version.
type VersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Version `json:"items"`
}

// The configuration settings for storing the module in an Amazon S3 bucket.
type AmazonS3Config struct {
	// The S3 bucket name.
	Bucket string `json:"bucket"`
	// The S3 bucket key, ie: 'my/bucket/prefix'
	// The file name will be automatically generated by the opendepot-module-controller.
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

// PresignConfig controls pre-signed URL generation for provider and module downloads.
type PresignConfig struct {
	// When true, download requests are redirected to the storage backend via a pre-signed URL.
	Enabled *bool `json:"enabled,omitempty"`
	// TTL controls how long the pre-signed URL remains valid (e.g. "15m", "1h").
	// If omitted, defaults to 15 minutes.
	// +kubebuilder:default="15m"
	TTL *metav1.Duration `json:"ttl,omitempty"`
	// When true, if pre-sign generation fails the server falls back to proxying the download.
	// Defaults to true.
	// +kubebuilder:default=true
	FallbackToProxy *bool `json:"fallbackToProxy,omitempty"`
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
	// Presign is the optional configuration for pre-signed URL generation.
	Presign *PresignConfig `json:"presign,omitempty"`
}

// The configuration settings for storing Versions on a local filesystem.
type FileSystemConfig struct {
	// The directory path on the file system where the Version will be stored.
	DirectoryPath *string `json:"directoryPath,omitempty"`
}

// GroupBindingExprEnv is the variable environment exposed to GroupBinding expressions.
type GroupBindingExprEnv struct {
	Groups []string `expr:"groups"`
}

// GroupBindingSpec defines the desired state of GroupBinding.
type GroupBindingSpec struct {
	// Expression is an expr-lang boolean expression evaluated against the OIDC JWT groups claim.
	// The only available variable is `groups` ([]string).
	// Examples:
	//   '"platform" in groups'
	//   '"platform" in groups || "platform-readonly" in groups'
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Expression string `json:"expression"`

	// ModuleResources is the list of glob patterns for Module resource names this binding grants access to.
	// The * wildcard is supported (e.g. "terraform-aws-*").
	// Empty or omitted means no modules are accessible.
	// +optional
	ModuleResources []string `json:"moduleResources,omitempty"`

	// ProviderResources is the list of exact Provider resource names this binding grants access to.
	// Empty or omitted means no providers are accessible.
	// +optional
	ProviderResources []string `json:"providerResources,omitempty"`
}

// GroupBindingStatus defines the observed state of GroupBinding.
type GroupBindingStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Expression",type="string",JSONPath=".spec.expression"

// GroupBinding is the Schema for the groupbindings API.
type GroupBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GroupBindingSpec   `json:"spec,omitempty"`
	Status GroupBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GroupBindingList contains a list of GroupBinding.
type GroupBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GroupBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Depot{}, &DepotList{})
	SchemeBuilder.Register(&GroupBinding{}, &GroupBindingList{})
	SchemeBuilder.Register(&Module{}, &ModuleList{})
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
	SchemeBuilder.Register(&Version{}, &VersionList{})
}
