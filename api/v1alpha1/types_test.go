package v1alpha1

import "testing"

func TestProviderUpstreamRegistry(t *testing.T) {
	terraformRegistry := TerraformRegistryHost
	blankRegistry := " "
	tests := []struct {
		name   string
		config *ProviderConfig
		want   string
	}{
		{name: "nil config", want: OpenTofuRegistryHost},
		{name: "omitted registry", config: &ProviderConfig{}, want: OpenTofuRegistryHost},
		{name: "blank registry", config: &ProviderConfig{UpstreamRegistry: &blankRegistry}, want: OpenTofuRegistryHost},
		{name: "Terraform registry", config: &ProviderConfig{UpstreamRegistry: &terraformRegistry}, want: TerraformRegistryHost},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProviderUpstreamRegistry(test.config); got != test.want {
				t.Fatalf("ProviderUpstreamRegistry() = %q, want %q", got, test.want)
			}
		})
	}
}
