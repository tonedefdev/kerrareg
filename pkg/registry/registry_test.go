package registry

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestProviderRegistryAPI(t *testing.T) {
	tests := []struct {
		name             string
		upstreamRegistry string
		want             string
		wantError        bool
	}{
		{name: "defaults blank to OpenTofu", want: OpenTofuRegistryAPI},
		{name: "supports OpenTofu", upstreamRegistry: opendepotv1alpha1.OpenTofuRegistryHost, want: OpenTofuRegistryAPI},
		{name: "supports Terraform", upstreamRegistry: opendepotv1alpha1.TerraformRegistryHost, want: "https://registry.terraform.io"},
		{name: "rejects unsupported registry", upstreamRegistry: "registry.example.com", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := providerRegistryAPI(test.upstreamRegistry)
			if test.wantError {
				if err == nil {
					t.Fatalf("providerRegistryAPI(%q) returned no error", test.upstreamRegistry)
				}

				return
			}

			if err != nil {
				t.Fatalf("providerRegistryAPI(%q) returned error: %v", test.upstreamRegistry, err)
			}
			if got != test.want {
				t.Fatalf("providerRegistryAPI(%q) = %q, want %q", test.upstreamRegistry, got, test.want)
			}
		})
	}
}
