package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	r.Get("/terraform/modules/v1/download/s3/{fileName}", serveModuleFromS3)
	http.ListenAndServeTLS("", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/certificate.crt", "/Users/tonedefdev/Desktop/kerrareg.defdev.io/private.key", r)
}

type ServiceDiscoveryResponse struct {
	ModulesURL string `json:"modules.v1"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersion `json:"modules"`
}

type ModuleVersion struct {
	Versions []kerrareg.ModuleVersion `json:"versions"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL: "/terraform/modules/v1/",
	}
	json.NewEncoder(w).Encode(response)
}

func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	w.Header().Set("X-Terraform-Get", fmt.Sprintf("/terraform/modules/v1/download/s3/%s-%s.zip", name, version))
	w.WriteHeader(http.StatusNoContent)
}

func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")

	cfg, err := config.LoadDefaultConfig(r.Context(), config.WithRegion("us-west-2"))
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	result, err := s3Client.GetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String("kerrareg-module-store"),
		Key:    aws.String(fileName),
	})
	if err != nil {
		log.Fatalf("failed to get module from S3: %v", err)
	}
	defer result.Body.Close()

	if result.ContentType != nil {
		w.Header().Set("Content-Type", *result.ContentType)
	}

	if _, err := io.Copy(w, result.Body); err != nil {
		http.Error(w, fmt.Sprintf("failed to stream file: %v", err), http.StatusInternalServerError)
		return
	}
}

func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)
		return
	}

	kubeconfigBase64 := strings.ReplaceAll(authHeader, "Bearer ", "")
	kubeconfig, err := base64.StdEncoding.DecodeString(kubeconfigBase64)
	if err != nil {
		log.Fatalf("base64 decode error: %v", err)
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		log.Fatal(err.Error())
	}

	clientConfig, err := config.ClientConfig()
	if err != nil {
		log.Fatal(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		log.Fatal(err.Error())
	}

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
	json.Unmarshal(result, &module)

	response := ModuleVersionsResponse{
		Modules: []ModuleVersion{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}
