package main

import opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"

// ServiceDiscoveryResponse is the JSON body returned at /.well-known/terraform.json.
// It advertises the module and provider registry URLs and, when OIDC is configured,
// the login.v1 block required by the OpenTofu CLI login flow.
type ServiceDiscoveryResponse struct {
	ModulesURL   string       `json:"modules.v1"`
	ProvidersURL string       `json:"providers.v1"`
	LoginV1      *LoginV1Info `json:"login.v1,omitempty"`
}

// LoginV1Info carries the OIDC authorization endpoints advertised to tofu CLI
// clients via the service-discovery document. When OIDC is not enabled this
// field is nil and omitted from the JSON response, preserving existing behaviour.
type LoginV1Info struct {
	Client     string   `json:"client"`
	GrantTypes []string `json:"grant_types"`
	Authz      string   `json:"authz"`
	Token      string   `json:"token"`
	Scopes     []string `json:"scopes"`
	Ports      []int    `json:"ports"`
}

// ModuleVersionsResponse is the JSON body returned by the module versions endpoint.
type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

// ModuleVersions holds the list of available versions for a single module.
type ModuleVersions struct {
	Versions []opendepotv1alpha1.ModuleVersion `json:"versions"`
}

// ProviderVersionsResponse is the JSON body returned by the provider versions endpoint.
type ProviderVersionsResponse struct {
	Versions []ProviderVersionDetails `json:"versions"`
}

// ProviderVersionDetails describes a single published provider version, including its
// supported protocols and target platforms.
type ProviderVersionDetails struct {
	Version   string             `json:"version"`
	Protocols []string           `json:"protocols,omitempty"`
	Platforms []ProviderPlatform `json:"platforms,omitempty"`
}

// ProviderPlatform represents an OS/architecture combination for a provider binary.
type ProviderPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// ProviderMirrorVersionsResponse is the version index returned by the
// OpenTofu Provider Network Mirror Protocol.
type ProviderMirrorVersionsResponse struct {
	Versions map[string]struct{} `json:"versions"`
}

// ProviderMirrorArchivesResponse lists the available installation packages for
// one provider version, keyed by target platform.
type ProviderMirrorArchivesResponse struct {
	Archives map[string]ProviderMirrorArchive `json:"archives"`
}

// ProviderMirrorArchive describes one provider package available from the mirror.
type ProviderMirrorArchive struct {
	URL    string   `json:"url"`
	Hashes []string `json:"hashes,omitempty"`
}

// ProviderPackageMetadataResponse is the JSON body returned by the provider package
// metadata endpoint. It provides download, checksum, and signing-key details consumed
// by the OpenTofu provider installer.
type ProviderPackageMetadataResponse struct {
	Protocols           []string            `json:"protocols"`
	OS                  string              `json:"os"`
	Arch                string              `json:"arch"`
	Filename            string              `json:"filename"`
	DownloadURL         string              `json:"download_url"`
	SHASumsURL          string              `json:"shasums_url"`
	SHASumsSignatureURL string              `json:"shasums_signature_url"`
	SHASum              string              `json:"shasum"`
	SigningKeys         ProviderSigningKeys `json:"signing_keys"`
}

// ProviderSigningKeys wraps the list of GPG public keys used to verify provider binaries.
type ProviderSigningKeys struct {
	GPGPublicKeys []ProviderSigningKey `json:"gpg_public_keys"`
}

// ProviderSigningKey represents a single GPG public key used for provider signature verification.
type ProviderSigningKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
	SourceURL  string `json:"source_url,omitempty"`
}

// BrowseScanCounts holds compact per-severity finding counts for UI card icons.
type BrowseScanCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// BrowseResource is a card-ready summary of a Module or Provider resource.
type BrowseResource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// LatestVersion is the most recent version string, if any.
	LatestVersion string `json:"latestVersion,omitempty"`
	Synced        bool   `json:"synced"`
	SyncStatus    string `json:"syncStatus,omitempty"`
	// HasUnsyncedVersions is true when at least one Version resource for this
	// Module or Provider has Synced == false or a SyncStatus containing "failed"
	// or "error" (case-insensitive).
	HasUnsyncedVersions bool `json:"hasUnsyncedVersions,omitempty"`
	// Module-specific fields.
	Provider string `json:"provider,omitempty"`
	RepoURL  string `json:"repoUrl,omitempty"`
	// Provider-specific fields.
	ProviderNamespace string             `json:"providerNamespace,omitempty"`
	UpstreamRegistry  string             `json:"upstreamRegistry,omitempty"`
	Platforms         []ProviderPlatform `json:"platforms,omitempty"`
	// Scan metadata.
	ScanCounts  *BrowseScanCounts `json:"scanCounts,omitempty"`
	LastScanned string            `json:"lastScanned,omitempty"`
	// Public reports whether the namespace and resource are both explicitly public.
	Public bool `json:"public"`
	// Download stats (populated from Valkey).
	TotalDownloads   int64  `json:"totalDownloads,omitempty"`
	LastDownloadedAt string `json:"lastDownloadedAt,omitempty"`
}

// BrowseResourceList is the JSON body returned by the browse resources list endpoint.
type BrowseResourceList struct {
	Items      []BrowseResource `json:"items"`
	TotalCount int              `json:"totalCount"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
}

// BrowseNamespace is a namespace visible to the caller.
type BrowseNamespace struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

// BrowseNamespaceList is the JSON body returned by the browse namespaces endpoint.
type BrowseNamespaceList struct {
	Items []BrowseNamespace `json:"items"`
}

// BrowseVersionSummary summarizes a single version for the detail page.
type BrowseVersionSummary struct {
	Name             string            `json:"name,omitempty"`
	Version          string            `json:"version"`
	Synced           bool              `json:"synced"`
	SyncStatus       string            `json:"syncStatus,omitempty"`
	OS               string            `json:"os,omitempty"`
	Arch             string            `json:"arch,omitempty"`
	ScanCounts       *BrowseScanCounts `json:"scanCounts,omitempty"`
	LastScanned      string            `json:"lastScanned,omitempty"`
	FileName         *string           `json:"fileName,omitempty"`
	Checksum         *string           `json:"checksum,omitempty"`
	DownloadCount    int64             `json:"downloadCount,omitempty"`
	LastDownloadedAt string            `json:"lastDownloadedAt,omitempty"`
	ArchiveSizeBytes *int64            `json:"archiveSizeBytes,omitempty"`
}

// BrowseVersionList is the paginated response for the version listing endpoint.
type BrowseVersionList struct {
	Items         []BrowseVersionSummary `json:"items"`
	TotalCount    int                    `json:"totalCount"`
	Page          int                    `json:"page"`
	PageSize      int                    `json:"pageSize"`
	AvailableOS   []string               `json:"availableOS,omitempty"`
	AvailableArch []string               `json:"availableArch,omitempty"`
}

// BrowseStorageConfig is a display-ready summary of a StorageConfig.
type BrowseStorageConfig struct {
	Backend        string  `json:"backend"`
	Bucket         string  `json:"bucket,omitempty"`
	Region         string  `json:"region,omitempty"`
	Key            *string `json:"key,omitempty"`
	DirectoryPath  *string `json:"directoryPath,omitempty"`
	AccountName    string  `json:"accountName,omitempty"`
	AccountUrl     string  `json:"accountUrl,omitempty"`
	SubscriptionID string  `json:"subscriptionID,omitempty"`
	ResourceGroup  string  `json:"resourceGroup,omitempty"`
	PresignEnabled *bool   `json:"presignEnabled,omitempty"`
	PresignTTL     string  `json:"presignTTL,omitempty"`
}

// BrowseGithubConfig summarises the GitHub client configuration for display.
type BrowseGithubConfig struct {
	UseAuthenticatedClient bool `json:"useAuthenticatedClient"`
}

// BrowseDepotRef links a resource to the Depot that manages it.
type BrowseDepotRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// BrowseDepot is a summary of a Depot resource for the depot list endpoint.
type BrowseDepot struct {
	Namespace              string   `json:"namespace"`
	Name                   string   `json:"name"`
	Modules                []string `json:"modules,omitempty"`
	Providers              []string `json:"providers,omitempty"`
	PollingIntervalMinutes *int     `json:"pollingIntervalMinutes,omitempty"`
	StorageBackend         string   `json:"storageBackend,omitempty"`
}

// BrowseDepotList is the JSON body returned by the depot list endpoint.
type BrowseDepotList struct {
	Items []BrowseDepot `json:"items"`
}

// BrowseResourceDetail is the full drill-down payload for a single resource.
type BrowseResourceDetail struct {
	BrowseResource
	// Versions is the list of known versions for this resource.
	Versions []BrowseVersionSummary `json:"versions,omitempty"`
	// SourceScanFindings are the IaC (module) or go.mod (provider) vulnerability findings.
	SourceScanFindings []opendepotv1alpha1.SecurityFinding `json:"sourceScanFindings,omitempty"`
	// BinaryScanFindings are per-artifact (os/arch) provider binary vulnerability findings.
	// Keys are in the form "os/arch".
	BinaryScanFindings  map[string][]opendepotv1alpha1.SecurityFinding `json:"binaryScanFindings,omitempty"`
	StorageConfig       *BrowseStorageConfig                           `json:"storageConfig,omitempty"`
	GithubConfig        *BrowseGithubConfig                            `json:"githubConfig,omitempty"`
	DepotRef            *BrowseDepotRef                                `json:"depotRef,omitempty"`
	RepoOwner           string                                         `json:"repoOwner,omitempty"`
	VersionHistoryLimit *int                                           `json:"versionHistoryLimit,omitempty"`
	VersionConstraints  string                                         `json:"versionConstraints,omitempty"`
	SourceRepository    string                                         `json:"sourceRepository,omitempty"`
	// ReadmeContent is the decoded (plain markdown) README.md content for the module's
	// latest version, if one could be resolved. Only populated for module resources.
	ReadmeContent *string `json:"readmeContent,omitempty"`
}

// BrowseScanFindings is the JSON body returned by the scan-findings endpoint.
type BrowseScanFindings struct {
	// SourceScanFindings are the IaC (module) or go.mod (provider) vulnerability findings.
	SourceScanFindings []opendepotv1alpha1.SecurityFinding `json:"sourceScanFindings,omitempty"`
	// BinaryScanFindings are per-artifact (os/arch) provider binary vulnerability findings.
	// Keys are in the form "os/arch".
	BinaryScanFindings map[string][]opendepotv1alpha1.SecurityFinding `json:"binaryScanFindings,omitempty"`
	// SelectedVersion is the provider/module version whose source scan findings are returned.
	SelectedVersion string `json:"selectedVersion,omitempty"`
	// ScannedVersions is the list of provider versions that have source scan results available,
	// sorted descending by semver. Used by the UI to populate the version selector dropdown.
	ScannedVersions []string `json:"scannedVersions,omitempty"`
	// BinaryVersions is the list of semver values that have binary scan results available,
	// sorted descending. Used by the UI to populate the binary version selector dropdown.
	BinaryVersions []string `json:"binaryVersions,omitempty"`
}

// BrowseGraphDepot is a depot node in the depots relationship graph.
type BrowseGraphDepot struct {
	ID                     string   `json:"id"`
	Namespace              string   `json:"namespace"`
	Name                   string   `json:"name"`
	StorageBackend         string   `json:"storageBackend,omitempty"`
	PollingIntervalMinutes *int     `json:"pollingIntervalMinutes,omitempty"`
	ManagedModuleNames     []string `json:"managedModuleNames,omitempty"`
	ManagedProviderNames   []string `json:"managedProviderNames,omitempty"`
}

// BrowseGraphModule is a module node in the depots relationship graph.
type BrowseGraphModule struct {
	ID            string            `json:"id"`
	Namespace     string            `json:"namespace"`
	Name          string            `json:"name"`
	Provider      string            `json:"provider,omitempty"`
	Synced        bool              `json:"synced"`
	SyncStatus    string            `json:"syncStatus,omitempty"`
	RepoURL       string            `json:"repoURL,omitempty"`
	LatestVersion string            `json:"latestVersion,omitempty"`
	DepotID       string            `json:"depotID,omitempty"`
	ScanCounts    *BrowseScanCounts `json:"scanCounts,omitempty"`
}

// BrowseGraphProvider is a provider node in the depots relationship graph.
type BrowseGraphProvider struct {
	ID                string `json:"id"`
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	ProviderNamespace string `json:"providerNamespace,omitempty"`
	UpstreamRegistry  string `json:"upstreamRegistry,omitempty"`
	Synced            bool   `json:"synced"`
}

// BrowseGraphEdge is a directed edge in the depots relationship graph.
// Source and Target are node IDs.
type BrowseGraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// BrowseGraphSummary contains aggregate counts for the graph.
type BrowseGraphSummary struct {
	TotalDepots    int `json:"totalDepots"`
	TotalModules   int `json:"totalModules"`
	TotalProviders int `json:"totalProviders"`
}

// BrowseDepotGraph is the full graph payload for the depot relationship view.
type BrowseDepotGraph struct {
	Depots      []BrowseGraphDepot    `json:"depots"`
	Modules     []BrowseGraphModule   `json:"modules"`
	Providers   []BrowseGraphProvider `json:"providers"`
	Edges       []BrowseGraphEdge     `json:"edges"`
	Summary     BrowseGraphSummary    `json:"summary"`
	GeneratedAt string                `json:"generatedAt"`
}

// BrowseStats is the aggregate stats payload returned by the stats endpoint.
type BrowseStats struct {
	TotalModules        int                  `json:"totalModules"`
	TotalProviders      int                  `json:"totalProviders"`
	TotalVersions       int                  `json:"totalVersions"`
	TotalStorageBytes   int64                `json:"totalStorageBytes"`
	TotalDownloads      int64                `json:"totalDownloads"`
	SyncHealth          SyncHealthStats      `json:"syncHealth"`
	SecurityPosture     SecurityPostureStats `json:"securityPosture"`
	StorageDistribution []StorageBackendStat `json:"storageDistribution"`
	MostDownloaded      []PopularResource    `json:"mostDownloaded"`
}

// SyncHealthStats breaks down version sync health.
type SyncHealthStats struct {
	SyncedVersions   int `json:"syncedVersions"`
	UnsyncedVersions int `json:"unsyncedVersions"`
	FailedVersions   int `json:"failedVersions"`
}

// SecurityPostureStats aggregates severity counts across all scan findings.
type SecurityPostureStats struct {
	Critical               int `json:"critical"`
	High                   int `json:"high"`
	Medium                 int `json:"medium"`
	Low                    int `json:"low"`
	Unknown                int `json:"unknown"`
	TotalAffectedResources int `json:"totalAffectedResources"`
}

// StorageBackendStat counts how many resources use a given storage backend.
type StorageBackendStat struct {
	Backend string `json:"backend"`
	Count   int    `json:"count"`
}

// PopularResource represents a resource version ranked by download count.
type PopularResource struct {
	Namespace        string `json:"namespace"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	DownloadCount    int64  `json:"downloadCount"`
	LastDownloadedAt string `json:"lastDownloadedAt,omitempty"`
}
