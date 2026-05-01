package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/openpgp"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
	"github.com/tonedefdev/kerrareg/pkg/storage"
	storageTypes "github.com/tonedefdev/kerrareg/pkg/storage/types"
)

var (
	logger                 *slog.Logger
	kerraregAnonymousAuth  *bool
	kerraregUseBearerToken *bool
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
}

func main() {
	kerraregAnonymousAuth = flag.Bool("anonymous-auth", false, "when true use the server's service account to serve modules and versions without requiring client authentication")
	kerraregUseBearerToken = flag.Bool("use-bearer-token", false, "when true use a bearer token instead of a base64 encoded kubeconfig to authenticate with the kubernetes API server")
	kerraregCertPath := flag.String("tls-cert-path", "", "path to TLS certificate file for HTTPS server")
	kerraregCertKey := flag.String("tls-cert-key", "", "path to TLS certificate key file for HTTPS server")
	flag.Parse()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/.well-known/terraform.json", serviceDiscoveryHandler)
	r.Get("/kerrareg/modules/v1/{namespace}/{name}/{system}/versions", getModuleVersions)
	r.Get("/kerrareg/modules/v1/{namespace}/{name}/{system}/{version}/download", getDownloadModuleUrl)
	r.Get("/kerrareg/providers/v1/{namespace}/{type}/versions", getProviderVersions)
	r.Get("/kerrareg/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}", getProviderPackageMetadata)
	r.Get("/kerrareg/providers/v1/download/{namespace}/{type}/{version}", serveProviderPackageDownload)
	r.Get("/kerrareg/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}", getProviderPackageSHA256SUMS)
	r.Get("/kerrareg/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}", getProviderPackageSHA256SUMSSignature)

	r.Get("/kerrareg/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}", serveModuleFromAzureBlob)
	r.Get("/kerrareg/modules/v1/download/fileSystem/{directory}/{name}/{fileName}", serveModuleFromFileSystem)
	r.Get("/kerrareg/modules/v1/download/gcs/{bucket}/{name}/{fileName}", serveModuleFromGCS)
	r.Get("/kerrareg/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}", serveModuleFromS3)

	if *kerraregCertPath != "" && *kerraregCertKey != "" {
		http.ListenAndServeTLS("", *kerraregCertPath, *kerraregCertKey, r)
	} else {
		logger.Info("Server started and listening on default port: 8080 without TLS. For secure communication, provide paths to TLS certificate and key using --tls-cert-path and --tls-cert-key flags.")
		if err := http.ListenAndServe(":8080", r); err != nil {
			logger.Error("Failed to start server", "error", err)
		}
	}

}

type ServiceDiscoveryResponse struct {
	ModulesURL   string `json:"modules.v1"`
	ProvidersURL string `json:"providers.v1"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

type ModuleVersions struct {
	Versions []kerraregv1alpha1.ModuleVersion `json:"versions"`
}

type ProviderVersionsResponse struct {
	Versions []ProviderVersionDetails `json:"versions"`
}

type ProviderVersionDetails struct {
	Version   string             `json:"version"`
	Protocols []string           `json:"protocols,omitempty"`
	Platforms []ProviderPlatform `json:"platforms,omitempty"`
}

type ProviderPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

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

type ProviderSigningKeys struct {
	GPGPublicKeys []ProviderSigningKey `json:"gpg_public_keys"`
}

type ProviderSigningKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
	SourceURL  string `json:"source_url,omitempty"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL:   "/kerrareg/modules/v1/",
		ProvidersURL: "/kerrareg/providers/v1/",
	}
	json.NewEncoder(w).Encode(response)
}

func normalizeVersion(versionString string) string {
	return strings.TrimPrefix(strings.TrimSpace(versionString), "v")
}

func getProviderVersionResource(clientset *kubernetes.Clientset, namespace, providerType, requestedVersion string, ctxName string, ctxReq *http.Request) (*kerraregv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/kerrareg.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(ctxReq.Context())
	if err != nil {
		return nil, err
	}

	var versionList kerraregv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		return nil, fmt.Errorf("unable to unmarshal versions list for %s: %w", ctxName, err)
	}

	normalizedRequestedVersion := normalizeVersion(requestedVersion)
	for _, item := range versionList.Items {
		if item.Spec.ProviderConfigRef == nil || item.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *item.Spec.ProviderConfigRef.Name != providerType {
			continue
		}

		if normalizeVersion(item.Spec.Version) != normalizedRequestedVersion {
			continue
		}

		return &item, nil
	}

	return nil, nil
}

func buildDownloadPathFromVersion(versionResource *kerraregv1alpha1.Version) (string, error) {
	var storageConfig *kerraregv1alpha1.StorageConfig
	var name *string

	if versionResource.Spec.ModuleConfigRef != nil && versionResource.Spec.ModuleConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ModuleConfigRef.StorageConfig
		name = versionResource.Spec.ModuleConfigRef.Name
	} else if versionResource.Spec.ProviderConfigRef != nil && versionResource.Spec.ProviderConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ProviderConfigRef.StorageConfig
		name = versionResource.Spec.ProviderConfigRef.Name
	}

	if storageConfig == nil || name == nil || versionResource.Spec.FileName == nil {
		return "", fmt.Errorf("storage configuration not available for version '%s'", versionResource.Name)
	}

	if storageConfig.AzureStorage != nil {
		return fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			storageConfig.AzureStorage.SubscriptionID,
			storageConfig.AzureStorage.ResourceGroup,
			storageConfig.AzureStorage.AccountName,
			url.PathEscape(storageConfig.AzureStorage.AccountUrl),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		return fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*storageConfig.FileSystem.DirectoryPath)),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.GCS != nil {
		return fmt.Sprintf("gcs/%s/%s/%s",
			storageConfig.GCS.Bucket,
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.S3 != nil {
		return fmt.Sprintf("s3/%s/%s/%s",
			storageConfig.S3.Bucket,
			storageConfig.S3.Region,
			*storageConfig.S3.Key,
		), nil
	}

	return "", fmt.Errorf("unsupported storage configuration for version '%s'", versionResource.Name)
}

func requestBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
			scheme = fwdProto
		} else {
			scheme = "http"
		}
	}

	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func getProviderSigningKeysFromEnv() (*ProviderSigningKeys, error) {
	keyID := strings.TrimSpace(os.Getenv("KERRAREG_PROVIDER_GPG_KEY_ID"))
	asciiArmor := os.Getenv("KERRAREG_PROVIDER_GPG_ASCII_ARMOR")
	if keyID == "" || asciiArmor == "" {
		return nil, fmt.Errorf("missing provider signing key env vars: KERRAREG_PROVIDER_GPG_KEY_ID and KERRAREG_PROVIDER_GPG_ASCII_ARMOR")
	}

	keys := &ProviderSigningKeys{
		GPGPublicKeys: []ProviderSigningKey{
			{
				KeyID:      strings.ToUpper(keyID),
				ASCIIArmor: asciiArmor,
				SourceURL:  strings.TrimSpace(os.Getenv("KERRAREG_PROVIDER_GPG_SOURCE_URL")),
			},
		},
	}

	return keys, nil
}

func decodeSHA256Checksum(base64Checksum string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(base64Checksum)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(decoded), nil
}

func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*kerraregv1alpha1.Version, error) {
	name := chi.URLParam(r, "name")
	namespace := chi.URLParam(r, "namespace")
	version := chi.URLParam(r, "version")
	moduleName := fmt.Sprintf("%s-%s", name, version)

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/kerrareg.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		Name(moduleName).
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var moduleVersion kerraregv1alpha1.Version
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	moduleVersion, err := getModuleVersion(clientset, w, r)
	if err != nil {
		logger.Error("unable to get module version", "error", err)
		return
	}

	var downloadPath string
	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage != nil {
		downloadPath = fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.SubscriptionID,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.ResourceGroup,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountName,
			url.PathEscape(moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountUrl),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem != nil {
		downloadPath = fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath)),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS != nil {
		downloadPath = fmt.Sprintf("gcs/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket,
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		downloadPath = fmt.Sprintf("s3/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Region,
			*moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Key,
		)
	}

	checksumQuery := url.QueryEscape(*moduleVersion.Status.Checksum)
	w.Header().Set("X-Terraform-Get", fmt.Sprintf("/kerrareg/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery))
	w.WriteHeader(http.StatusNoContent)
}

func serveModuleFromAzureBlob(w http.ResponseWriter, r *http.Request) {
	accountName := chi.URLParam(r, "account")
	accountUrl := chi.URLParam(r, "accountUrl")
	rg := chi.URLParam(r, "rg")
	subID := chi.URLParam(r, "subID")

	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	accountUrl, err := url.PathUnescape(accountUrl)
	if err != nil {
		logger.Error("failed to unescape account url", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	storage := &storage.AzureBlobStorage{}
	if err := storage.NewClients(subID, accountUrl); err != nil {
		logger.Error("failed to init azure clients", "error", err, "storageAccountName", accountName)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	version := &kerraregv1alpha1.Version{
		Spec: kerraregv1alpha1.VersionSpec{
			ModuleConfigRef: &kerraregv1alpha1.ModuleConfig{
				Name: &name,
				StorageConfig: &kerraregv1alpha1.StorageConfig{
					AzureStorage: &kerraregv1alpha1.AzureStorageConfig{
						AccountName:    accountName,
						AccountUrl:     accountUrl,
						ResourceGroup:  rg,
						SubscriptionID: subID,
					},
				},
			},
		},
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: &fileName,
		Method:   storageTypes.Get,
		Version:  version,
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

func serveModuleFromFileSystem(w http.ResponseWriter, r *http.Request) {
	encodedDir := chi.URLParam(r, "directory")
	moduleName := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	// go-getter sends ?terraform-get=1 to detect source URLs via HTML meta tags.
	// We intercept this and return the X-Terraform-Get header pointing to the same
	// download URL. go-getter reads the header before parsing the body, then processes
	// the source URL through its full pipeline which detects the archive extension
	// and uses direct file download (no further terraform-get detection).
	if r.URL.Query().Get("terraform-get") == "1" {
		scheme := "https"
		if r.TLS == nil {
			if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
				scheme = fwdProto
			} else {
				scheme = "http"
			}
		}

		q := url.Values{}
		q.Set("fileChecksum", checksum)

		// GitHub tarballs are gzip-compressed despite having .tar extension.
		// go-getter uses the archive param to select the decompressor, so we
		// must specify tar.gz for gzipped tarballs.
		ext := path.Ext(fileName)
		archiveType := strings.TrimPrefix(ext, ".")
		if archiveType == "tar" {
			archiveType = "tar.gz"
		}

		q.Set("archive", archiveType)
		sourceURL := fmt.Sprintf("%s://%s/kerrareg/modules/v1/download/fileSystem/%s/%s/%s?%s",
			scheme, r.Host, encodedDir, moduleName, fileName, q.Encode())

		w.Header().Set("X-Terraform-Get", sourceURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	dirBytes, err := base64.RawURLEncoding.DecodeString(encodedDir)
	if err != nil {
		logger.Error("failed to decode directory path", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}
	dir := string(dirBytes)

	logger.Info("filesystem download", "dir", dir, "module", moduleName, "file", fileName)

	filePath := path.Join(
		dir,
		moduleName,
		fileName,
	)

	storage := &storage.FileSystem{}
	soi := &storageTypes.StorageObjectInput{
		FilePath: &filePath,
		Method:   storageTypes.Get,
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

func serveModuleFromGCS(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	gcsStorage := &storage.GoogleCloudStorage{}
	if err := gcsStorage.NewClient(r.Context()); err != nil {
		logger.Error("failed to init gcs client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	version := &kerraregv1alpha1.Version{
		Spec: kerraregv1alpha1.VersionSpec{
			ModuleConfigRef: &kerraregv1alpha1.ModuleConfig{
				StorageConfig: &kerraregv1alpha1.StorageConfig{
					GCS: &kerraregv1alpha1.GoogleCloudStorageConfig{
						Bucket: bucket,
					},
				},
			},
		},
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		Version:  version,
	}

	getObjectFromStorageSystem(w, r, gcsStorage, soi, checksum)
}

func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	region := chi.URLParam(r, "region")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	storage := &storage.AmazonS3Storage{}
	if err := storage.NewClient(r.Context(), region); err != nil {
		logger.Error("failed to init s3 client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	version := kerraregv1alpha1.Version{
		Spec: kerraregv1alpha1.VersionSpec{
			ModuleConfigRef: &kerraregv1alpha1.ModuleConfig{
				StorageConfig: &kerraregv1alpha1.StorageConfig{
					S3: &kerraregv1alpha1.AmazonS3Config{
						Bucket: bucket,
					},
				},
			},
		},
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		Version:  &version,
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

// getObjectFromStorage validates the object's sha256 checksum and when valid copies from the storage system src to the
// download stream dst provided by http.ResponseWriter
func getObjectFromStorageSystem(w http.ResponseWriter, r *http.Request, storage storage.Storage, soi *storageTypes.StorageObjectInput, checksum string) {
	if err := storage.GetObjectChecksum(r.Context(), soi); err != nil {
		logger.Error("failed to get checksum from storage system", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if soi.ObjectChecksum != nil && *soi.ObjectChecksum != checksum {
		logger.Error("checksum mismatch from storage system", "want", checksum, "received", *soi.ObjectChecksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	reader, err := storage.GetObject(r.Context(), soi)
	if err != nil {
		logger.Error("failed to get module from storage system", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	if strings.HasSuffix(*soi.FilePath, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "application/x-tar")
	}

	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, fmt.Sprintf("failed to stream file: %v", err), http.StatusInternalServerError)
		return
	}
}

// generateKubeClient creates a new kubernetes client from either a kubeconfig as a byte slice
// or from a bearerToken. When using a bearerToken this function will use the in-cluster config
// to generate the necessary rest.Config settings for TLS connections.
func generateKubeClient(kubeconfig []byte, bearerToken *string, useBearerToken bool) (*kubernetes.Clientset, error) {
	var clientConfig *rest.Config
	var err error

	if bearerToken == nil && kubeconfig == nil {
		// Anonymous auth: use in-cluster config with the server's own service account
		clientConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else if useBearerToken {
		clientConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		clientConfig.BearerToken = *bearerToken
		clientConfig.BearerTokenFile = ""
	} else {
		config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
		if err != nil {
			return nil, err
		}

		clientConfig, err = config.ClientConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

// getKubeClientFromRequest creates a Kubernetes clientset based on the configured auth mode.
// When anonymous auth is enabled, uses the server's in-cluster service account.
// When bearer token mode is enabled, extracts the token from the Authorization header.
// Otherwise, extracts a base64-encoded kubeconfig from the Authorization header.
func getKubeClientFromRequest(w http.ResponseWriter, r *http.Request) (*kubernetes.Clientset, error) {
	if *kerraregAnonymousAuth {
		return generateKubeClient(nil, nil, false)
	}

	var kubeconfig []byte
	var bearerToken string

	if *kerraregUseBearerToken {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return nil, fmt.Errorf("missing Authorization header")
		}
		bearerToken = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		config, err := extractKubeconfig(w, r)
		if err != nil {
			return nil, err
		}
		kubeconfig = config
	}

	return generateKubeClient(kubeconfig, &bearerToken, *kerraregUseBearerToken)
}

func extractKubeconfig(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)
		return nil, fmt.Errorf("missing Authorization header")
	}

	kubeconfigBase64 := strings.ReplaceAll(authHeader, "Bearer ", "")
	kubeconfig, err := base64.StdEncoding.DecodeString(kubeconfigBase64)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return kubeconfig, nil
}

func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/kerrareg.io/v1alpha1").
		Namespace(namespace).
		Resource("modules").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to get modules", "error", err, "namespace", namespace, "name", name, "responseBody", string(result))
	}

	var module kerraregv1alpha1.Module
	if err = json.Unmarshal(result, &module); err != nil {
		logger.Error("unable to unmarshal module", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	response := ModuleVersionsResponse{
		Modules: []ModuleVersions{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

func getProviderVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")

	_, err = clientset.RESTClient().
		Get().
		AbsPath("/apis/kerrareg.io/v1alpha1").
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
		AbsPath("/apis/kerrareg.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to list versions", "error", err, "namespace", namespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var versionList kerraregv1alpha1.VersionList
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

		normalized := normalizeVersion(item.Spec.Version)
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

func getProviderPackageMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")
	osName := chi.URLParam(r, "os")
	arch := chi.URLParam(r, "arch")

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
	versionString := normalizeVersion(versionResource.Spec.Version)

	response := ProviderPackageMetadataResponse{
		Protocols:           []string{"5.0"},
		OS:                  osName,
		Arch:                arch,
		Filename:            *versionResource.Spec.FileName,
		DownloadURL:         fmt.Sprintf("%s/kerrareg/providers/v1/download/%s/%s/%s", baseURL, namespace, providerType, versionString),
		SHASumsURL:          fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/%s/SHA256SUMS/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASumsSignatureURL: fmt.Sprintf("%s/kerrareg/providers/v1/%s/%s/%s/SHA256SUMS.sig/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASum:              checksumHex,
		SigningKeys:         *signingKeys,
	}

	json.NewEncoder(w).Encode(response)
}

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

	downloadPath, err := buildDownloadPathFromVersion(versionResource)
	if err != nil {
		logger.Error("unable to build download path for provider package", "error", err, "version", versionResource.Name)
		http.Error(w, "provider package download backend not implemented", http.StatusNotImplemented)
		return
	}

	if versionResource.Status.Checksum == nil {
		http.Error(w, "provider package checksum unavailable", http.StatusNotImplemented)
		return
	}

	checksumQuery := url.QueryEscape(*versionResource.Status.Checksum)
	http.Redirect(w, r, fmt.Sprintf("/kerrareg/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery), http.StatusFound)
}

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

	_, _ = w.Write([]byte(fmt.Sprintf("%s  %s\n", checksumHex, *versionResource.Spec.FileName)))
}

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

	privateKeyBase64 := strings.TrimSpace(os.Getenv("KERRAREG_PROVIDER_GPG_PRIVATE_KEY_BASE64"))
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
