/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	// OpenTofuRegistryAPI is the base URL for the OpenTofu provider registry.
	OpenTofuRegistryAPI = "https://registry.opentofu.org"
	// OpenTofuDocsAPI is the base URL for the OpenTofu registry documentation API.
	OpenTofuDocsAPI = "https://api.opentofu.org"
	// DefaultNamespace is the default provider namespace when none is specified.
	DefaultNamespace = "hashicorp"
)

func providerRegistryAPI(upstreamRegistry string) (string, error) {
	switch strings.TrimSpace(upstreamRegistry) {
	case "", opendepotv1alpha1.OpenTofuRegistryHost:
		return "https://" + opendepotv1alpha1.OpenTofuRegistryHost, nil
	case opendepotv1alpha1.TerraformRegistryHost:
		return "https://" + opendepotv1alpha1.TerraformRegistryHost, nil
	default:
		return "", fmt.Errorf("unsupported provider upstream registry %q", upstreamRegistry)
	}
}

// ProviderVersionsResponse is the shape returned by the OpenTofu registry versions endpoint.
type ProviderVersionsResponse struct {
	Versions []ProviderVersion `json:"versions"`
}

// ProviderVersion holds the version string from the registry versions response.
type ProviderVersion struct {
	Version string `json:"version"`
}

// ProviderDownload holds download metadata for a specific provider version/os/arch.
type ProviderDownload struct {
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	Shasum      string `json:"shasum"`
}

// ProviderDocsResponse is the subset of the OpenTofu docs API provider response used here.
type ProviderDocsResponse struct {
	Link string `json:"link"`
}

// ListProviderVersions returns all version strings available for a provider from the configured registry.
// The caller is responsible for applying any version constraint filtering.
func ListProviderVersions(ctx context.Context, upstreamRegistry, namespace, name string) ([]string, error) {
	if strings.TrimSpace(namespace) == "" {
		namespace = DefaultNamespace
	}

	registryAPI, err := providerRegistryAPI(upstreamRegistry)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1/providers/%s/%s/versions",
		registryAPI,
		strings.ToLower(strings.TrimSpace(namespace)),
		strings.ToLower(strings.TrimSpace(name)),
	)

	var resp ProviderVersionsResponse
	if err := HTTPGetJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("failed to list provider versions for %s/%s: %w", namespace, name, err)
	}

	versions := make([]string, 0, len(resp.Versions))
	for _, v := range resp.Versions {
		if s := strings.TrimSpace(v.Version); s != "" {
			versions = append(versions, s)
		}
	}

	return versions, nil
}

// LookupProviderDownload returns download metadata for a specific provider version/os/arch
// from the configured registry.
func LookupProviderDownload(ctx context.Context, upstreamRegistry, namespace, name, version, os, arch string) (*ProviderDownload, error) {
	registryAPI, err := providerRegistryAPI(upstreamRegistry)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1/providers/%s/%s/%s/download/%s/%s",
		registryAPI,
		strings.ToLower(strings.TrimSpace(namespace)),
		strings.ToLower(strings.TrimSpace(name)),
		strings.TrimPrefix(strings.TrimSpace(version), "v"),
		strings.ToLower(strings.TrimSpace(os)),
		strings.ToLower(strings.TrimSpace(arch)),
	)

	var resp ProviderDownload
	if err := HTTPGetJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("provider download lookup for %s/%s@%s (%s/%s) failed: %w",
			namespace, name, version, os, arch, err)
	}

	if strings.TrimSpace(resp.DownloadURL) == "" {
		return nil, fmt.Errorf("registry returned empty download_url for %s/%s@%s (%s/%s)",
			namespace, name, version, os, arch)
	}

	return &resp, nil
}

// LookupProviderRepo queries the OpenTofu docs API for a provider's VCS source URL.
// On failure it returns an error; the caller should fall back to a heuristic URL.
func LookupProviderRepo(ctx context.Context, namespace, name string) (string, error) {
	endpoint := fmt.Sprintf("%s/registry/docs/providers/%s/%s/index.json",
		OpenTofuDocsAPI,
		strings.ToLower(strings.TrimSpace(namespace)),
		strings.ToLower(strings.TrimSpace(name)),
	)

	var resp ProviderDocsResponse
	if err := HTTPGetJSON(ctx, endpoint, &resp); err != nil {
		return "", fmt.Errorf("opentofu registry lookup for %s/%s failed: %w", namespace, name, err)
	}

	if strings.TrimSpace(resp.Link) == "" {
		return "", fmt.Errorf("opentofu registry returned empty link for %s/%s", namespace, name)
	}

	return strings.TrimSpace(resp.Link), nil
}

// HTTPGetJSON performs a GET request and unmarshals the JSON response body into out.
func HTTPGetJSON(ctx context.Context, requestURL string, out any) error {
	b, err := httpGetBytes(ctx, requestURL)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("unable to parse JSON from '%s': %w", requestURL, err)
	}

	return nil
}

func httpGetBytes(ctx context.Context, requestURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	c := &http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed for '%s': %w", requestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to '%s' failed with status %d", requestURL, resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body for '%s': %w", requestURL, err)
	}

	return b, nil
}
