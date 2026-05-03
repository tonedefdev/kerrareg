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

package controller

import (
	"context"
	"fmt"
	"strings"
)

const (
	openTofuDocsAPI     = "https://api.opentofu.org"
	openTofuRegistryAPI = "https://registry.opentofu.org"
)

// openTofuProviderResponse is the subset of the OpenTofu registry docs API provider response used here.
type openTofuProviderResponse struct {
	Link string `json:"link"`
}

// lookupProviderRepo queries the OpenTofu registry docs API for a provider's VCS source URL.
// On failure it returns an error; the caller should fall back to a heuristic URL.
func lookupProviderRepo(ctx context.Context, namespace, name string) (string, error) {
	endpoint := fmt.Sprintf("%s/registry/docs/providers/%s/%s/index.json",
		openTofuDocsAPI,
		strings.ToLower(strings.TrimSpace(namespace)),
		strings.ToLower(strings.TrimSpace(name)),
	)

	var resp openTofuProviderResponse
	if err := httpGetJSON(ctx, endpoint, &resp); err != nil {
		return "", fmt.Errorf("opentofu registry lookup for %s/%s failed: %w", namespace, name, err)
	}

	if strings.TrimSpace(resp.Link) == "" {
		return "", fmt.Errorf("opentofu registry returned empty link for %s/%s", namespace, name)
	}

	return strings.TrimSpace(resp.Link), nil
}

// openTofuRegistryDownload holds the fields from an OpenTofu registry provider download response.
// See: https://opentofu.org/docs/internals/provider-registry-protocol/#find-a-provider-package
type openTofuRegistryDownload struct {
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	Shasum      string `json:"shasum"`
}

// lookupProviderDownload queries the OpenTofu registry for a specific provider version's download metadata.
func lookupProviderDownload(ctx context.Context, namespace, name, version, os, arch string) (*openTofuRegistryDownload, error) {
	endpoint := fmt.Sprintf("%s/v1/providers/%s/%s/%s/download/%s/%s",
		openTofuRegistryAPI,
		strings.ToLower(strings.TrimSpace(namespace)),
		strings.ToLower(strings.TrimSpace(name)),
		strings.TrimPrefix(strings.TrimSpace(version), "v"),
		strings.ToLower(strings.TrimSpace(os)),
		strings.ToLower(strings.TrimSpace(arch)),
	)

	var resp openTofuRegistryDownload
	if err := httpGetJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("opentofu registry download lookup for %s/%s@%s (%s/%s) failed: %w",
			namespace, name, version, os, arch, err)
	}

	if strings.TrimSpace(resp.DownloadURL) == "" {
		return nil, fmt.Errorf("opentofu registry returned empty download_url for %s/%s@%s (%s/%s)",
			namespace, name, version, os, arch)
	}

	return &resp, nil
}
