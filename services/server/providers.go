package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/openpgp"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

const openTofuRegistryHost = "registry.opentofu.org"

func providerConfigIdentity(providerConfig *opendepotv1alpha1.ProviderConfig, fallbackName string) (string, string) {
	providerNamespace := "hashicorp"
	providerName := fallbackName
	if providerConfig == nil {
		return providerNamespace, providerName
	}

	if providerConfig.Namespace != nil && strings.TrimSpace(*providerConfig.Namespace) != "" {
		providerNamespace = strings.TrimSpace(*providerConfig.Namespace)
	}

	if providerConfig.Name != nil && strings.TrimSpace(*providerConfig.Name) != "" {
		providerName = strings.TrimSpace(*providerConfig.Name)
	}

	return providerNamespace, providerName
}

func getMirrorProvider(clientset *kubernetes.Clientset, namespace, providerNamespace, providerType string, r *http.Request) (*opendepotv1alpha1.Provider, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("providers").
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var providerList opendepotv1alpha1.ProviderList
	if err := json.Unmarshal(result, &providerList); err != nil {
		return nil, fmt.Errorf("unmarshal providers list for network mirror: %w", err)
	}

	for index := range providerList.Items {
		provider := &providerList.Items[index]
		configuredNamespace, configuredName := providerConfigIdentity(&provider.Spec.ProviderConfig, provider.Name)
		if configuredNamespace == providerNamespace && configuredName == providerType {
			return provider, nil
		}
	}

	return nil, nil
}

func listMirrorProviderVersions(clientset *kubernetes.Clientset, namespace, providerNamespace, providerType string, r *http.Request) ([]opendepotv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var versionList opendepotv1alpha1.VersionList
	if err := json.Unmarshal(result, &versionList); err != nil {
		return nil, fmt.Errorf("unmarshal versions list for network mirror: %w", err)
	}

	versions := make([]opendepotv1alpha1.Version, 0)
	for index := range versionList.Items {
		version := versionList.Items[index]
		configuredNamespace, configuredName := providerConfigIdentity(version.Spec.ProviderConfigRef, "")
		if configuredNamespace != providerNamespace || configuredName != providerType {
			continue
		}

		if !version.Status.Synced || version.Spec.FileName == nil || version.Status.Checksum == nil ||
			version.Spec.OperatingSystem == "" || version.Spec.Architecture == "" {
			continue
		}

		versions = append(versions, version)
	}

	return versions, nil
}

func findMirrorProviderVersion(versions []opendepotv1alpha1.Version, requestedVersion, osName, arch string) *opendepotv1alpha1.Version {
	normalizedRequestedVersion := opendepotUtils.SanitizeVersion(requestedVersion)
	for index := range versions {
		version := &versions[index]
		if opendepotUtils.SanitizeVersion(version.Spec.Version) == normalizedRequestedVersion &&
			version.Spec.OperatingSystem == osName && version.Spec.Architecture == arch {
			return version
		}
	}

	return nil
}

func buildProviderMirrorVersionsResponse(versions []opendepotv1alpha1.Version) ProviderMirrorVersionsResponse {
	response := ProviderMirrorVersionsResponse{Versions: make(map[string]struct{})}
	for index := range versions {
		version := opendepotUtils.SanitizeVersion(versions[index].Spec.Version)
		if version != "" {
			response.Versions[version] = struct{}{}
		}
	}

	return response
}

func buildProviderMirrorArchivesResponse(versions []opendepotv1alpha1.Version, requestedVersion string) ProviderMirrorArchivesResponse {
	normalizedRequestedVersion := opendepotUtils.SanitizeVersion(requestedVersion)
	response := ProviderMirrorArchivesResponse{Archives: make(map[string]ProviderMirrorArchive)}
	for index := range versions {
		version := &versions[index]
		if opendepotUtils.SanitizeVersion(version.Spec.Version) != normalizedRequestedVersion {
			continue
		}

		platform := version.Spec.OperatingSystem + "_" + version.Spec.Architecture
		checksumHex, err := decodeSHA256Checksum(*version.Status.Checksum)
		if err != nil {
			continue
		}
		response.Archives[platform] = ProviderMirrorArchive{
			URL:    fmt.Sprintf("%s/%s/%s/%s", normalizedRequestedVersion, version.Spec.OperatingSystem, version.Spec.Architecture, path.Base(*version.Spec.FileName)),
			Hashes: []string{"zh:" + checksumHex},
		}
	}

	return response
}

func authorizeMirrorProvider(w http.ResponseWriter, r *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.Provider, bool) {
	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider mirror", "error", err)

		return nil, nil, false
	}

	namespace := chi.URLParam(r, "namespace")
	providerNamespace := chi.URLParam(r, "providerNamespace")
	providerType := chi.URLParam(r, "type")
	if chi.URLParam(r, "hostname") != openTofuRegistryHost {
		http.Error(w, "provider not found", http.StatusNotFound)

		return nil, nil, false
	}

	provider, err := getMirrorProvider(clientset, namespace, providerNamespace, providerType, r)
	if err != nil {
		logger.Error("unable to locate provider for network mirror", "error", err, "namespace", namespace, "providerNamespace", providerNamespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return nil, nil, false
	}

	if provider == nil {
		http.Error(w, "provider not found", http.StatusNotFound)

		return nil, nil, false
	}

	if binding != nil && !isResourceAllowed(binding, "provider", provider.Name) {
		logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", provider.Name, "namespace", namespace)
		http.Error(w, "forbidden", http.StatusForbidden)

		return nil, nil, false
	}

	return clientset, provider, true
}

func getProviderMirrorVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, _, ok := authorizeMirrorProvider(w, r)
	if !ok {
		return
	}

	versions, err := listMirrorProviderVersions(clientset, chi.URLParam(r, "namespace"), chi.URLParam(r, "providerNamespace"), chi.URLParam(r, "type"), r)
	if err != nil {
		logger.Error("unable to list provider versions for network mirror", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	response := buildProviderMirrorVersionsResponse(versions)

	if len(response.Versions) == 0 {
		http.Error(w, "provider not found", http.StatusNotFound)

		return
	}

	_ = json.NewEncoder(w).Encode(response)
}

func getProviderMirrorArchives(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	requestedVersion, ok := strings.CutSuffix(chi.URLParam(r, "versionFile"), ".json")
	if !ok || requestedVersion == "" {
		http.Error(w, "provider version not found", http.StatusNotFound)

		return
	}

	clientset, _, ok := authorizeMirrorProvider(w, r)
	if !ok {
		return
	}

	versions, err := listMirrorProviderVersions(clientset, chi.URLParam(r, "namespace"), chi.URLParam(r, "providerNamespace"), chi.URLParam(r, "type"), r)
	if err != nil {
		logger.Error("unable to list provider archives for network mirror", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	response := buildProviderMirrorArchivesResponse(versions, requestedVersion)

	if len(response.Archives) == 0 {
		http.Error(w, "provider version not found", http.StatusNotFound)

		return
	}

	_ = json.NewEncoder(w).Encode(response)
}

func serveProviderMirrorArchive(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "hostname") != openTofuRegistryHost {
		http.Error(w, "provider package not found", http.StatusNotFound)

		return
	}

	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider mirror download", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerNamespace := chi.URLParam(r, "providerNamespace")
	providerType := chi.URLParam(r, "type")
	versions, err := listMirrorProviderVersions(clientset, namespace, providerNamespace, providerType, r)
	if err != nil {
		logger.Error("unable to list provider archives for network mirror download", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	versionResource := findMirrorProviderVersion(versions, chi.URLParam(r, "version"), chi.URLParam(r, "os"), chi.URLParam(r, "arch"))
	if versionResource == nil || versionResource.Spec.FileName == nil || path.Base(*versionResource.Spec.FileName) != chi.URLParam(r, "filename") {
		http.Error(w, "provider package not found", http.StatusNotFound)

		return
	}

	serveProviderVersionDownload(w, r, namespace, providerType, chi.URLParam(r, "version"), versionResource)
}

// getProviderVersionResource scans all Version resources in the given namespace and
// returns the first one whose ProviderConfigRef matches providerType and whose
// normalized version matches requestedVersion. Returns nil, nil when no match is found.
// ctxName is included in error messages for caller-specific context.
func getProviderVersionResource(clientset *kubernetes.Clientset, namespace, providerType, requestedVersion string, ctxName string, ctxReq *http.Request) (*opendepotv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(ctxReq.Context())
	if err != nil {
		return nil, err
	}

	var versionList opendepotv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		return nil, fmt.Errorf("unable to unmarshal versions list for %s: %w", ctxName, err)
	}

	normalizedRequestedVersion := opendepotUtils.SanitizeVersion(requestedVersion)
	for _, item := range versionList.Items {
		if item.Spec.ProviderConfigRef == nil || item.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *item.Spec.ProviderConfigRef.Name != providerType {
			continue
		}

		if opendepotUtils.SanitizeVersion(item.Spec.Version) != normalizedRequestedVersion {
			continue
		}

		return &item, nil
	}

	return nil, nil
}

// getProviderSigningKeysFromEnv reads the provider GPG signing key material from
// the OPENDEPOT_PROVIDER_GPG_KEY_ID, OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR, and
// OPENDEPOT_PROVIDER_GPG_SOURCE_URL environment variables and returns them as a
// ProviderSigningKeys value for inclusion in package metadata responses.
func getProviderSigningKeysFromEnv() (*ProviderSigningKeys, error) {
	keyID := strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_KEY_ID"))
	asciiArmor := os.Getenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR")
	if keyID == "" || asciiArmor == "" {
		return nil, fmt.Errorf("missing provider signing key env vars: OPENDEPOT_PROVIDER_GPG_KEY_ID and OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR")
	}

	keys := &ProviderSigningKeys{
		GPGPublicKeys: []ProviderSigningKey{
			{
				KeyID:      strings.ToUpper(keyID),
				ASCIIArmor: asciiArmor,
				SourceURL:  strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_SOURCE_URL")),
			},
		},
	}

	return keys, nil
}

// decodeSHA256Checksum converts a base64-encoded SHA-256 digest (as stored in a
// Version resource's status) to its lowercase hex representation expected by the
// Terraform provider registry protocol.
func decodeSHA256Checksum(base64Checksum string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(base64Checksum)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(decoded), nil
}

// getProviderVersions returns the list of available versions for a provider type,
// as required by the Terraform provider registry protocol.
func getProviderVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")

	if binding != nil {
		if !isResourceAllowed(binding, "provider", providerType) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
	}

	_, err = clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("providers").
		Name(providerType).
		DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}

		logger.Error("unable to get provider resource", "error", err, "namespace", namespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to list versions", "error", err, "namespace", namespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var versionList opendepotv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		logger.Error("unable to unmarshal versions list", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	versionSet := make(map[string]struct{})
	providerVersions := make([]ProviderVersionDetails, 0)
	for _, item := range versionList.Items {
		if item.Spec.ProviderConfigRef == nil || item.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *item.Spec.ProviderConfigRef.Name != providerType {
			continue
		}

		normalized := opendepotUtils.SanitizeVersion(item.Spec.Version)
		if normalized == "" {
			continue
		}

		if _, exists := versionSet[normalized]; exists {
			continue
		}

		versionSet[normalized] = struct{}{}
		providerVersions = append(providerVersions, ProviderVersionDetails{
			Version: normalized,
		})
	}

	response := ProviderVersionsResponse{Versions: providerVersions}
	json.NewEncoder(w).Encode(response)
}

// getProviderPackageMetadata returns the download URL, checksum, and signing-key
// metadata for a specific provider version/OS/arch tuple, as required by the
// Terraform provider registry protocol.
func getProviderPackageMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")
	osName := chi.URLParam(r, "os")
	arch := chi.URLParam(r, "arch")

	if binding != nil {
		if !isResourceAllowed(binding, "provider", providerType) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
	}

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider package metadata", r)
	if err != nil {
		logger.Error("unable to locate provider version", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	if versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package metadata incomplete", http.StatusNotImplemented)
		return
	}

	signingKeys, err := getProviderSigningKeysFromEnv()
	if err != nil {
		logger.Error("provider signing keys are not configured", "error", err)
		http.Error(w, "provider signing metadata not configured", http.StatusNotImplemented)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	baseURL := requestBaseURL(r)
	versionString := opendepotUtils.SanitizeVersion(versionResource.Spec.Version)

	response := ProviderPackageMetadataResponse{
		Protocols:           []string{"5.0"},
		OS:                  osName,
		Arch:                arch,
		Filename:            *versionResource.Spec.FileName,
		DownloadURL:         fmt.Sprintf("%s/opendepot/providers/v1/download/%s/%s/%s", baseURL, namespace, providerType, versionString),
		SHASumsURL:          fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASumsSignatureURL: fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS.sig/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASum:              checksumHex,
		SigningKeys:         *signingKeys,
	}

	json.NewEncoder(w).Encode(response)
}

// serveProviderPackageDownload handles provider binary downloads. When the Version
// resource has presigning enabled and a compatible storage backend, it issues a
// temporary redirect to a presigned URL so the client downloads directly from the
// storage backend. Otherwise it falls back to proxying the binary through the server.
// This endpoint is accessed by OpenTofu without credentials per the provider registry
// protocol spec; the server's own service account is used for Kubernetes API calls.
func serveProviderPackageDownload(w http.ResponseWriter, r *http.Request) {
	// Provider artifact download endpoints are accessed by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Use the
	// server's own service account for k8s access.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider download", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider package download", r)
	if err != nil {
		logger.Error("unable to locate provider version for download", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	serveProviderVersionDownload(w, r, namespace, providerType, requestedVersion, versionResource)
}

func serveProviderVersionDownload(w http.ResponseWriter, r *http.Request, namespace, providerType, requestedVersion string, versionResource *opendepotv1alpha1.Version) {
	if versionResource.Status.Checksum == nil {
		http.Error(w, "provider package checksum unavailable", http.StatusNotImplemented)
		return
	}

	// Attempt a presigned URL redirect so Terraform downloads directly from the storage backend.
	if versionResource.Spec.ProviderConfigRef != nil &&
		versionResource.Spec.ProviderConfigRef.StorageConfig != nil &&
		versionResource.Spec.ProviderConfigRef.Name != nil &&
		versionResource.Spec.FileName != nil {

		storageConfig := versionResource.Spec.ProviderConfigRef.StorageConfig
		presignCfg := storageConfig.Presign

		if presignCfg != nil && presignCfg.Enabled != nil && *presignCfg.Enabled {
			name := versionResource.Spec.ProviderConfigRef.Name

			objectKey := fmt.Sprintf("%s/%s", *name, *versionResource.Spec.FileName)

			ttl := 15 * time.Minute
			if presignCfg.TTL != nil {
				ttl = presignCfg.TTL.Duration
			}

			soi := &storageTypes.StorageObjectInput{
				FilePath:      &objectKey,
				ContainerName: name,
				StorageConfig: storageConfig,
				PresignTTL:    ttl,
			}

			fallback := presignCfg.FallbackToProxy == nil || *presignCfg.FallbackToProxy

			storageBackend, initErr := initStorageBackend(r.Context(), storageConfig)
			if initErr != nil {
				if !fallback {
					logger.Error("failed to init storage backend for presign", "error", initErr)
					http.Error(w, "failed to initialize storage backend for pre-signed URL", http.StatusBadGateway)
					return
				}
				logger.Info("failed to init storage backend for presign, falling back to proxy", "error", initErr)
			} else if presignErr := storageBackend.PresignObject(r.Context(), soi); presignErr == nil {
				_ = recordDownload(r.Context(), statsClient, namespace, "provider", providerType, requestedVersion)
				http.Redirect(w, r, *soi.PresignedURL, http.StatusTemporaryRedirect)
				return
			} else {
				if !fallback {
					logger.Error("presign failed", "error", presignErr)
					http.Error(w, "failed to generate pre-signed URL", http.StatusBadGateway)
					return
				}
				logger.Info("presign not supported or failed, falling back to proxy", "error", presignErr)
			}
		}
	}

	// Fallback: proxy the binary through the server.
	downloadPath, err := buildDownloadPathFromVersion(versionResource)
	if err != nil {
		logger.Error("unable to build download path for provider package", "error", err, "version", versionResource.Name)
		http.Error(w, "provider package download backend not implemented", http.StatusNotImplemented)
		return
	}

	checksumQuery := url.QueryEscape(*versionResource.Status.Checksum)
	_ = recordDownload(r.Context(), statsClient, namespace, "provider", providerType, requestedVersion)
	http.Redirect(w, r, fmt.Sprintf("/opendepot/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery), http.StatusFound)
}

// getProviderPackageSHA256SUMS serves the SHA256SUMS file for a provider package.
// The URL for this endpoint is returned in the package metadata response and is
// fetched by OpenTofu without credentials per the provider registry protocol spec.
func getProviderPackageSHA256SUMS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// SHA256SUMS is a provider artifact endpoint fetched by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Access to
	// this URL is gated by the authenticated metadata endpoint that returns it.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider shasums", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider shasums", r)
	if err != nil {
		logger.Error("unable to locate provider version for shasums", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil || versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(fmt.Appendf(nil, "%s  %s\n", checksumHex, *versionResource.Spec.FileName))
}

// getProviderPackageSHA256SUMSSignature serves the detached GPG signature of the
// SHA256SUMS file for a provider package. The private key is read from the
// OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64 environment variable at request time.
func getProviderPackageSHA256SUMSSignature(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")

	// SHA256SUMS.sig is a provider artifact endpoint fetched by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Access to
	// this URL is gated by the authenticated metadata endpoint that returns it.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider shasums signature", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider shasums signature", r)
	if err != nil {
		logger.Error("unable to locate provider version for shasums signature", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil || versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	shasumsContent := fmt.Sprintf("%s  %s\n", checksumHex, *versionResource.Spec.FileName)

	privateKeyBase64 := strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"))
	if privateKeyBase64 == "" {
		http.Error(w, "provider gpg private key not configured", http.StatusNotImplemented)
		return
	}

	privateKeyArmor, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		logger.Error("unable to decode gpg private key", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateKeyArmor))
	if err != nil {
		logger.Error("unable to parse gpg private key", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if len(entityList) == 0 {
		logger.Error("no gpg entities found in private key")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var sigBuf bytes.Buffer
	if err = openpgp.DetachSign(&sigBuf, entityList[0], strings.NewReader(shasumsContent), nil); err != nil {
		logger.Error("unable to sign provider shasums", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(sigBuf.Bytes())
}
