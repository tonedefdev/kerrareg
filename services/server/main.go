package main

import (
	"encoding/base64"
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
	ModulesURL string `json:"modules.v1"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

type ModuleVersions struct {
	Versions []kerraregv1alpha1.ModuleVersion `json:"versions"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL: "/kerrareg/modules/v1/",
	}
	json.NewEncoder(w).Encode(response)
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
