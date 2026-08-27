package main

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestProviderConfigIdentity(t *testing.T) {
	name := "aws"
	namespace := "integrations"
	terraformRegistry := opendepotv1alpha1.TerraformRegistryHost

	tests := []struct {
		name             string
		config           *opendepotv1alpha1.ProviderConfig
		fallback         string
		wantRegistry     string
		wantNamespace    string
		wantProviderName string
	}{
		{name: "defaults", config: &opendepotv1alpha1.ProviderConfig{}, fallback: "null", wantRegistry: opendepotv1alpha1.OpenTofuRegistryHost, wantNamespace: "hashicorp", wantProviderName: "null"},
		{name: "configured", config: &opendepotv1alpha1.ProviderConfig{Name: &name, Namespace: &namespace, UpstreamRegistry: &terraformRegistry}, fallback: "ignored", wantRegistry: opendepotv1alpha1.TerraformRegistryHost, wantNamespace: "integrations", wantProviderName: "aws"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRegistry, gotNamespace, gotName := providerConfigIdentity(test.config, test.fallback)
			if gotRegistry != test.wantRegistry || gotNamespace != test.wantNamespace || gotName != test.wantProviderName {
				t.Fatalf("providerConfigIdentity() = %q/%q/%q, want %q/%q/%q", gotRegistry, gotNamespace, gotName, test.wantRegistry, test.wantNamespace, test.wantProviderName)
			}
		})
	}
}

func TestSupportedProviderRegistry(t *testing.T) {
	for _, hostname := range []string{opendepotv1alpha1.OpenTofuRegistryHost, opendepotv1alpha1.TerraformRegistryHost} {
		if !supportedProviderRegistry(hostname) {
			t.Fatalf("supportedProviderRegistry(%q) = false, want true", hostname)
		}
	}

	if supportedProviderRegistry("registry.example.com") {
		t.Fatal("supportedProviderRegistry() accepted unsupported registry")
	}
}

func TestBuildProviderMirrorResponses(t *testing.T) {
	fileLinux := "terraform-provider-null_3.2.3_linux_amd64.zip"
	fileDarwin := "terraform-provider-null_3.2.3_darwin_arm64.zip"
	checksumLinux := "bGludXg="
	checksumDarwin := "ZGFyd2lu"
	versions := []opendepotv1alpha1.Version{
		{
			Spec: opendepotv1alpha1.VersionSpec{
				Version: "v3.2.3", OperatingSystem: "linux", Architecture: "amd64", FileName: &fileLinux,
			},
			Status: opendepotv1alpha1.VersionStatus{Checksum: &checksumLinux, Synced: true},
		},
		{
			Spec: opendepotv1alpha1.VersionSpec{
				Version: "3.2.3", OperatingSystem: "darwin", Architecture: "arm64", FileName: &fileDarwin,
			},
			Status: opendepotv1alpha1.VersionStatus{Checksum: &checksumDarwin, Synced: true},
		},
	}

	index := buildProviderMirrorVersionsResponse(versions)
	if len(index.Versions) != 1 {
		t.Fatalf("version count = %d, want 1", len(index.Versions))
	}
	if _, ok := index.Versions["3.2.3"]; !ok {
		t.Fatalf("versions = %#v, want normalized 3.2.3", index.Versions)
	}

	archives := buildProviderMirrorArchivesResponse(versions, "v3.2.3")
	linux, ok := archives.Archives["linux_amd64"]
	if !ok {
		t.Fatalf("archives = %#v, want linux_amd64", archives.Archives)
	}
	if linux.URL != "3.2.3/linux/amd64/terraform-provider-null_3.2.3_linux_amd64.zip" {
		t.Fatalf("linux URL = %q", linux.URL)
	}
	if len(linux.Hashes) != 1 || linux.Hashes[0] != "zh:6c696e7578" {
		t.Fatalf("linux hashes = %#v", linux.Hashes)
	}

	if selected := findMirrorProviderVersion(versions, "3.2.3", "darwin", "arm64"); selected == nil || selected.Spec.FileName != &fileDarwin {
		t.Fatalf("findMirrorProviderVersion() = %#v, want darwin_arm64", selected)
	}
}
