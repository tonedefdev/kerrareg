package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	kerrareg "defdev.io/kerrareg/services/controller/api/v1alpha1"
)

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/.well-known/terraform.json", serviceDiscoveryHandler)
	r.Get("/terraform/modules/v1/{namespace}/{name}/{system}/versions", getModuleVersions)
	r.Get("/terraform/modules/v1/{namespace}/{name}/{system}/{version}/download", getDownloadModuleUrl)
	r.Get("/terraform/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}", serveModuleFromS3)
	http.ListenAndServeTLS("", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/certificate.crt", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/private.key", r)
}

type ServiceDiscoveryResponse struct {
	ModulesURL string `json:"modules.v1"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersion `json:"modules"`
}

type ModuleVersion struct {
	Versions []kerrareg.ModuleVersionSpec `json:"versions"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL: "/terraform/modules/v1/",
	}
	json.NewEncoder(w).Encode(response)
}

func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*kerrareg.ModuleVersion, error) {
	name := chi.URLParam(r, "name")
	namespace := chi.URLParam(r, "namespace")
	version := chi.URLParam(r, "version")
	moduleName := fmt.Sprintf("%s-%s", name, version)

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/kerrareg.io/v1alpha1").
		Namespace(namespace).
		Resource("moduleversions").
		Name(moduleName).
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var moduleVersion kerrareg.ModuleVersion
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	kubeconfig, err := extractKubeconfig(w, r)
	if err != nil {
		log.Fatalf("extract kubeconfig error: %v", err)
		return
	}

	clientset, err := generateKubeClient(kubeconfig)
	if err != nil {
		log.Fatalf("generate kube client error: %v", err)
		return
	}

	moduleVersion, err := getModuleVersion(clientset, w, r)
	if moduleVersion == nil || err != nil {
		log.Fatalf("get module version error: %v", err)
		return
	}

	checksumQuery := url.QueryEscape(moduleVersion.Spec.Checksum)

	var downloadPath string
	if moduleVersion.Spec.Storage.S3 != nil {
		downloadPath = fmt.Sprintf("s3/%s/%s/%s?fileChecksum=%s",
			moduleVersion.Spec.Storage.S3.Config.Bucket,
			moduleVersion.Spec.Storage.S3.Config.Region,
			moduleVersion.Spec.Storage.S3.Key,
			checksumQuery,
		)
	}

	w.Header().Set("X-Terraform-Get", fmt.Sprintf("/terraform/modules/v1/download/%s", downloadPath))
	w.WriteHeader(http.StatusNoContent)
}

func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	region := chi.URLParam(r, "region")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	cfg, err := config.LoadDefaultConfig(r.Context(), config.WithRegion(region))
	if err != nil {
		log.Printf("unable to load SDK config, %v", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	result, err := s3Client.GetObject(r.Context(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    aws.String(fmt.Sprintf("%s/%s", name, fileName)),
	})
	if err != nil {
		log.Printf("failed to get module from S3: %v", err)
	}
	defer result.Body.Close()

	if result.ChecksumSHA256 == nil {
		http.Error(w, "s3 checksumSHA256 was nil", http.StatusInternalServerError)
		return
	}

	if *result.ChecksumSHA256 != checksum {
		log.Printf("checksum mismatch: got '%s' want '%s'", checksum, *result.ChecksumSHA256)
		http.Error(w, "s3 checksumSHA256 was nil", http.StatusInternalServerError)
		return
	}

	if result.ContentType != nil {
		w.Header().Set("Content-Type", *result.ContentType)
	}

	if _, err := io.Copy(w, result.Body); err != nil {
		http.Error(w, fmt.Sprintf("failed to stream file: %v", err), http.StatusInternalServerError)
		return
	}
}

func generateKubeClient(kubeconfig []byte) (*kubernetes.Clientset, error) {
	config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, err
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
		log.Fatalf("base64 decode error: %v", err)
	}

	return kubeconfig, nil
}

func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	kubeconfig, err := extractKubeconfig(w, r)
	if err != nil {
		log.Fatalf("extract kubeconfig error: %v", err)
		return
	}

	clientset, err := generateKubeClient(kubeconfig)
	if err != nil {
		log.Fatalf("generate kube client error: %v", err)
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
		log.Fatalf("get modules error: %v", err)
	}

	var module kerrareg.Module
	if err = json.Unmarshal(result, &module); err != nil {
		log.Fatalf("unmarshal module error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	response := ModuleVersionsResponse{
		Modules: []ModuleVersion{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}
