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

	"kerrareg/api/types"
	"kerrareg/pkg/storage"
	storageTypes "kerrareg/pkg/storage/types"
	modulev1alpha1 "kerrareg/services/module/api/v1alpha1"
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

var (
	logger                 *slog.Logger
	kerraregUseBearerToken *bool
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
}

func main() {
	kerraregUseBearerToken = flag.Bool("use-bearer-token", false, "when true use a bearer token instead of a base64 encoded kubeconfig to authenticate with the kubernetes API server")
	flag.Parse()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/.well-known/terraform.json", serviceDiscoveryHandler)
	r.Get("/kerrareg/modules/v1/{namespace}/{name}/{system}/versions", getModuleVersions)
	r.Get("/kerrareg/modules/v1/{namespace}/{name}/{system}/{version}/download", getDownloadModuleUrl)

	r.Get("/kerrareg/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}", serveModuleFromAzureBlob)
	r.Get("/kerrareg/modules/v1/download/fileSystem/{directory}/{name}/{fileName}", serveModuleFromFileSystem)
	r.Get("/kerrareg/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}", serveModuleFromS3)

	http.ListenAndServeTLS("", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/certificate.crt", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/private.key", r)
}

type ServiceDiscoveryResponse struct {
	ModulesURL string `json:"modules.v1"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

type ModuleVersions struct {
	Versions []types.ModuleVersion `json:"versions"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL: "/kerrareg/modules/v1/",
	}
	json.NewEncoder(w).Encode(response)
}

func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*versionv1alpha1.Version, error) {
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

	var moduleVersion versionv1alpha1.Version
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var kubeconfig []byte
	var bearerToken string

	if *kerraregUseBearerToken {
		bearerToken = r.Header.Get("Authorization")
	} else {
		config, err := extractKubeconfig(w, r)
		if err != nil {
			logger.Error("unable to extract kubeconfig", "error", err)
			return
		}

		kubeconfig = config
	}

	clientset, err := generateKubeClient(kubeconfig, &bearerToken, *kerraregUseBearerToken)
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
			url.PathEscape(*moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		downloadPath = fmt.Sprintf("s3/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Region,
			url.PathEscape(*moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Key),
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

	storage := &storage.AzureBlobStorage{}
	if err := storage.NewClients(subID, accountUrl); err != nil {
		logger.Error("failed to init azure clients", "error", err, "storageAccountName", accountName)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	version := &versionv1alpha1.Version{
		Spec: versionv1alpha1.VersionSpec{
			ModuleConfigRef: types.ModuleConfig{
				Name: &name,
				StorageConfig: types.StorageConfig{
					AzureStorage: &types.AzureStorageConfig{
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
	dir := chi.URLParam(r, "directory")
	moduleName := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

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

	version := versionv1alpha1.Version{
		Spec: versionv1alpha1.VersionSpec{
			ModuleConfigRef: types.ModuleConfig{
				StorageConfig: types.StorageConfig{
					S3: &types.AmazonS3Config{
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
	if useBearerToken {
		clientConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		clientConfig.BearerToken = *bearerToken
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
	var kubeconfig []byte
	var bearerToken string

	if *kerraregUseBearerToken {
		bearerToken = r.Header.Get("Authorization")
	} else {
		config, err := extractKubeconfig(w, r)
		if err != nil {
			logger.Error("unable to extract kubeconfig", "error", err)
			return
		}

		kubeconfig = config
	}

	clientset, err := generateKubeClient(kubeconfig, &bearerToken, *kerraregUseBearerToken)
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
		logger.Error("unable to get modules", "error", err)
	}

	var module modulev1alpha1.Module
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
