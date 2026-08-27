package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

const (
	// labelPublic is the label key used to mark namespaces and resources as publicly browseable.
	labelPublic = "opendepot.defdev.io/public"
)

// k8sNamespace is a minimal struct for deserializing namespace objects.
type k8sNamespace struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

// k8sNamespaceList is a minimal struct for deserializing the namespace list API response.
type k8sNamespaceList struct {
	Items []k8sNamespace `json:"items"`
}

// k8sConfigMap is a minimal struct for deserializing ConfigMap objects.
type k8sConfigMap struct {
	Data map[string]string `json:"data"`
}

// isPublicNamespace reports whether the given label set marks a namespace as public.
func isPublicNamespace(labels map[string]string) bool {
	return labels[labelPublic] == "true"
}

// isPublicResource reports whether the given label set marks a resource as public.
func isPublicResource(labels map[string]string) bool {
	return labels[labelPublic] == "true"
}

// storageConfigToBrowse converts a StorageConfig to a BrowseStorageConfig for display.
func storageConfigToBrowse(sc *opendepotv1alpha1.StorageConfig) *BrowseStorageConfig {
	if sc == nil {
		return nil
	}

	result := &BrowseStorageConfig{}

	switch {
	case sc.S3 != nil:
		result.Backend = "s3"
		result.Bucket = sc.S3.Bucket
		result.Region = sc.S3.Region
		result.Key = sc.S3.Key
	case sc.AzureStorage != nil:
		result.Backend = "azureStorage"
		result.AccountName = sc.AzureStorage.AccountName
		result.AccountUrl = sc.AzureStorage.AccountUrl
		result.SubscriptionID = sc.AzureStorage.SubscriptionID
		result.ResourceGroup = sc.AzureStorage.ResourceGroup
	case sc.GCS != nil:
		result.Backend = "gcs"
		result.Bucket = sc.GCS.Bucket
	case sc.FileSystem != nil:
		result.Backend = "fileSystem"
		result.DirectoryPath = sc.FileSystem.DirectoryPath
	}

	if sc.Presign != nil {
		result.PresignEnabled = sc.Presign.Enabled
		if sc.Presign.TTL != nil {
			result.PresignTTL = sc.Presign.TTL.Duration.String()
		}
	}

	return result
}

// browseDepotForModule finds the first Depot in the given namespace whose status lists the module.
func browseDepotForModule(cs *kubernetes.Clientset, r *http.Request, namespace, moduleName string) *BrowseDepotRef {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("depots").
		DoRaw(r.Context())
	if err != nil {
		return nil
	}

	var list opendepotv1alpha1.DepotList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}

	for _, d := range list.Items {
		for _, m := range d.Status.Modules {
			if m == moduleName {
				return &BrowseDepotRef{Namespace: d.Namespace, Name: d.Name}
			}
		}
	}

	return nil
}

// browseDepotForProvider finds the first Depot in the given namespace whose status lists the provider.
func browseDepotForProvider(cs *kubernetes.Clientset, r *http.Request, namespace, providerName string) *BrowseDepotRef {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("depots").
		DoRaw(r.Context())
	if err != nil {
		return nil
	}

	var list opendepotv1alpha1.DepotList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}

	for _, d := range list.Items {
		for _, p := range d.Status.Providers {
			if p == providerName {
				return &BrowseDepotRef{Namespace: d.Namespace, Name: d.Name}
			}
		}
	}

	return nil
}

// browseScanCounts tallies SecurityFindings by severity into a BrowseScanCounts.
func browseScanCounts(findings []opendepotv1alpha1.SecurityFinding) *BrowseScanCounts {
	if len(findings) == 0 {
		return nil
	}

	counts := &BrowseScanCounts{}
	for _, f := range findings {
		switch strings.ToUpper(f.Severity) {
		case "CRITICAL":
			counts.Critical++
		case "HIGH":
			counts.High++
		case "MEDIUM":
			counts.Medium++
		case "LOW":
			counts.Low++
		default:
			counts.Unknown++
		}
	}

	return counts
}

// latestOf returns the lexicographically latest non-empty string from the arguments.
func latestOf(times ...string) string {
	var latest string
	for _, t := range times {
		if t > latest {
			latest = t
		}
	}
	return latest
}

// anyVersionUnsynced returns true when any Version in vs has Synced == false
// or a SyncStatus containing "failed" or "error" (case-insensitive).
func anyVersionUnsynced(vs []opendepotv1alpha1.Version) bool {
	for _, v := range vs {
		ss := strings.ToLower(v.Status.SyncStatus)
		if !v.Status.Synced || strings.Contains(ss, "failed") || strings.Contains(ss, "error") {
			return true
		}
	}
	return false
}

// severityRank returns a numeric rank for a severity string (lower = more severe).
func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return 0
	case "HIGH":
		return 1
	case "MEDIUM":
		return 2
	case "LOW":
		return 3
	default:
		return 4
	}
}

// highestSeverity returns the most severe level present in a BrowseScanCounts.
// Returns "" when counts is nil or all-zero.
func highestSeverity(c *BrowseScanCounts) string {
	if c == nil {
		return ""
	}

	switch {
	case c.Critical > 0:
		return "CRITICAL"
	case c.High > 0:
		return "HIGH"
	case c.Medium > 0:
		return "MEDIUM"
	case c.Low > 0:
		return "LOW"
	case c.Unknown > 0:
		return "UNKNOWN"
	default:
		return ""
	}
}

// browseSAClient creates a Kubernetes clientset using the server's own service account.
// All browse endpoint Kubernetes reads go through this client so that visibility
// filtering is applied in-process rather than relying on per-user Kubernetes RBAC.
func browseSAClient() (*kubernetes.Clientset, error) {
	return generateKubeClient(nil, nil, false)
}

// browseAuthClientCredentials attempts to authenticate via OAuth2 client credentials.
// Returns the GroupBinding (may be nil when no GroupBinding matches) and verified=true
// when the token was successfully verified as a client-credentials token.
// Returns nil and false on any auth failure.
func browseAuthClientCredentials(ctx context.Context, rawToken string) (*opendepotv1alpha1.GroupBinding, bool) {
	if !*opendepotOIDCAllowClientCredentials || oidcCCVerifier == nil {
		return nil, false
	}

	iss, _ := parseUnsignedJWTIssuer(rawToken)
	if iss != *opendepotOIDCIssuerURL {
		return nil, false
	}

	ccToken, err := oidcCCVerifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, false
	}

	var ccClaims map[string]any
	if err := ccToken.Claims(&ccClaims); err != nil {
		return nil, false
	}

	sub, _ := ccClaims["sub"].(string)
	if sub == "" {
		return nil, false
	}

	cs, err := browseSAClient()
	if err != nil {
		return nil, false
	}

	b, _ := findGroupBinding(ctx, cs, []string{"client:" + sub})
	// b may be nil when no GroupBinding matches — the CC client is still authenticated.
	return b, true
}

// browseAuthCheck gates browse access when the server is in OIDC mode with anonymous-auth
// disabled. It writes an appropriate HTTP error and returns ok=false when the request must
// be rejected. On success it returns the resolved GroupBinding (nil when none matches) and
// the allAccess flag, following the same visibility semantics as the former browseAuthState.
//
// Gating rules (when oidcVerifier != nil && !anonymousAuth):
//   - No Authorization header, or not a Bearer token → 401
//   - Token fails all verification paths (OIDC, UI client, CC) → 401
//   - Token valid, no GroupBinding match → (nil, false, true)  public-only visibility
//   - Token valid, GroupBinding found   → (binding, false, true)
//
// When anonymous-auth is enabled or OIDC is not configured the function never rejects.
func browseAuthCheck(w http.ResponseWriter, r *http.Request) (binding *opendepotv1alpha1.GroupBinding, allAccess bool, ok bool) {
	if *opendepotAnonymousAuth {
		return nil, true, true
	}

	if oidcVerifier == nil {
		// Non-OIDC mode: no gating; public-only visibility.
		return nil, false, true
	}

	// OIDC mode is active — require a valid Bearer token.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false, false
	}

	rawToken := strings.TrimPrefix(authHeader, "Bearer ")
	idToken, err := oidcVerifier.Verify(r.Context(), rawToken)
	if err != nil {
		// If a separate UI client ID is configured, try verifying as a UI token.
		if oidcUIVerifier != nil {
			if uiToken, uiErr := oidcUIVerifier.Verify(r.Context(), rawToken); uiErr == nil {
				idToken = uiToken
				err = nil
			}
		}
		if err != nil {
			// Try the client-credentials path.
			if b, verified := browseAuthClientCredentials(r.Context(), rawToken); verified {
				return b, false, true
			}
			// All verification paths exhausted — the token is invalid.
			logger.Warn("browse: token verification failed", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, false, false
		}
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false, false
	}

	groups, _ := extractGroupsClaim(claims, *opendepotOIDCGroupsClaim)
	if len(groups) == 0 {
		// Valid token but no groups claim — authenticated without a GroupBinding.
		return nil, false, true
	}

	cs, err := browseSAClient()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false, false
	}

	b, err := findGroupBinding(r.Context(), cs, groups)
	if err != nil {
		// Authenticated but no matching GroupBinding → public-only visibility.
		return nil, false, true
	}

	return b, false, true
}

// browseListNamespaces returns only namespaces labelled opendepot.defdev.io/public=true
// via the server SA. The label selector is applied server-side so that system namespaces
// (kube-system, default, etc.) never enter the browse layer.
func browseListNamespaces(cs *kubernetes.Clientset, r *http.Request) ([]k8sNamespace, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1/namespaces").
		Param("labelSelector", labelPublic+"=true").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var list k8sNamespaceList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal namespace list: %w", err)
	}

	return list.Items, nil
}

// browseGetNamespaceLabels returns labels for a single namespace using the raw REST client.
func browseGetNamespaceLabels(cs *kubernetes.Clientset, r *http.Request, namespace string) (map[string]string, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1/namespaces/" + namespace).
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	var ns k8sNamespace
	if err := json.Unmarshal(raw, &ns); err != nil {
		return nil, fmt.Errorf("failed to unmarshal namespace %s: %w", namespace, err)
	}

	return ns.Metadata.Labels, nil
}

// isBrowseVisible reports whether a resource should be included in browse results.
//
// Visibility rules:
//   - allAccess (anonymous-auth mode): all resources visible; publicOnly still applies.
//   - pub true: public resource, always visible unless publicOnly with !pub (noop).
//   - pub false, binding nil: hidden (no GroupBinding → public-only).
//   - pub false, binding not nil: visible only if isResourceAllowed grants access.
func isBrowseVisible(pub, publicOnly, allAccess bool, binding *opendepotv1alpha1.GroupBinding, kind, name string) bool {
	if allAccess {
		if publicOnly && !pub {
			return false
		}
		return true
	}

	if !pub {
		// Non-public resource: requires a matching GroupBinding.
		if binding == nil {
			return false
		}

		if !isResourceAllowed(binding, kind, name) {
			return false
		}
	}

	if publicOnly && !pub {
		return false
	}

	return true
}

// browseCollectModules fetches and filters Module resources for the browse endpoint.
func browseCollectModules(cs *kubernetes.Clientset, r *http.Request, nsFilter, nsPublic map[string]bool, binding *opendepotv1alpha1.GroupBinding, allAccess, publicOnly bool) ([]BrowseResource, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("modules").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list modules: %w", err)
	}

	var list opendepotv1alpha1.ModuleList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal module list: %w", err)
	}

	dlStats, _ := batchResourceDownloadStats(r.Context(), statsClient, func() []string {
		keys := make([]string, 0, len(list.Items))
		for _, m := range list.Items {
			keys = append(keys, m.Namespace+"/module/"+m.Name)
		}

		return keys
	}())

	var items []BrowseResource
	for _, m := range list.Items {
		ns := m.Namespace
		if len(nsFilter) > 0 && !nsFilter[ns] {
			continue
		}

		pub := nsPublic[ns] && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, publicOnly, allAccess, binding, "module", m.Name) {
			continue
		}

		resource := moduleToCard(m, pub)
		versionData, _ := browseListModuleVersions(cs, r, ns, m.Name)
		enrichModuleCard(&resource, versionData)
		enrichResourceWithDownloads(&resource, dlStats)
		items = append(items, resource)
	}
	return items, nil
}

// browseCollectProviders fetches and filters Provider resources for the browse endpoint.
func browseCollectProviders(cs *kubernetes.Clientset, r *http.Request, nsFilter, nsPublic map[string]bool, binding *opendepotv1alpha1.GroupBinding, allAccess, publicOnly bool, filterOS, filterArch string) ([]BrowseResource, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("providers").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}

	var list opendepotv1alpha1.ProviderList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider list: %w", err)
	}

	dlStats, _ := batchResourceDownloadStats(r.Context(), statsClient, func() []string {
		keys := make([]string, 0, len(list.Items))
		for _, p := range list.Items {
			keys = append(keys, p.Namespace+"/provider/"+p.Name)
		}

		return keys
	}())

	var items []BrowseResource
	for _, p := range list.Items {
		ns := p.Namespace
		if len(nsFilter) > 0 && !nsFilter[ns] {
			continue
		}

		pub := nsPublic[ns] && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, publicOnly, allAccess, binding, "provider", p.Name) {
			continue
		}

		resource := providerToCard(p, pub)
		versionData, _ := browseListProviderVersions(cs, r, ns)
		enrichProviderCard(&resource, p, providerVersionsFor(versionData, p.Name), filterOS, filterArch)
		enrichResourceWithDownloads(&resource, dlStats)
		items = append(items, resource)
	}
	return items, nil
}

// handleBrowseNamespaces returns the list of namespaces that carry the
// opendepot.defdev.io/public=true label. The label filter is enforced at the
// Kubernetes API level in browseListNamespaces, so system namespaces
// (kube-system, default, etc.) never appear regardless of auth mode.
// GET /opendepot/ui/v1/namespaces
func handleBrowseNamespaces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, _, ok := browseAuthCheck(w, r); !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	result := BrowseNamespaceList{Items: []BrowseNamespace{}}
	for _, ns := range namespaces {
		result.Items = append(result.Items, BrowseNamespace{
			Name:   ns.Metadata.Name,
			Public: true,
		})
	}

	json.NewEncoder(w).Encode(result)
}

// handleBrowseResources returns the paginated, filtered, sorted list of visible resources.
// GET /opendepot/ui/v1/resources
//
// Query parameters:
//
//	namespace   - repeat for multi-namespace filter (default: all visible namespaces)
//	kind        - "module" or "provider" (default: both)
//	q           - search text matched against name, namespace, provider, repoUrl
//	synced      - "true"/"false" filter by sync status
//	os          - filter providers by supported OS
//	arch        - filter providers by supported architecture
//	severity    - minimum severity: CRITICAL/HIGH/MEDIUM/LOW
//	public_only - "true" show only public resources
//	sort_by     - name|namespace|latest_version|synced|severity|last_scanned (default: name)
//	sort_dir    - asc|desc (default: asc)
//	page        - 1-based page number (default: 1)
//	page_size   - items per page (default: 20, max: 100)
func handleBrowseResources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	filterNamespaces := q["namespace"]
	filterKind := strings.ToLower(q.Get("kind"))
	searchText := strings.ToLower(q.Get("q"))
	filterSynced := q.Get("synced")
	filterOS := q.Get("os")
	filterArch := q.Get("arch")
	filterSeverity := strings.ToUpper(q.Get("severity"))
	publicOnly := q.Get("public_only") == "true"
	sortBy := q.Get("sort_by")

	if sortBy == "" {
		sortBy = "name"
	}

	sortDir := strings.ToLower(q.Get("sort_dir"))
	if sortDir == "" {
		sortDir = "asc"
	}

	page := 1
	if p, err2 := strconv.Atoi(q.Get("page")); err2 == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err2 := strconv.Atoi(q.Get("page_size")); err2 == nil && ps > 0 {
		if ps > 100 {
			ps = 100
		}
		pageSize = ps
	}

	allNamespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsFilter := make(map[string]bool, len(filterNamespaces))
	for _, ns := range filterNamespaces {
		nsFilter[ns] = true
	}

	nsPublic := make(map[string]bool, len(allNamespaces))
	for _, ns := range allNamespaces {
		nsPublic[ns.Metadata.Name] = isPublicNamespace(ns.Metadata.Labels)
	}

	var items []BrowseResource

	if filterKind == "" || filterKind == "module" {
		modules, err := browseCollectModules(cs, r, nsFilter, nsPublic, binding, allAccess, publicOnly)
		if err != nil {
			logger.Error("browse: failed to collect modules", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		items = append(items, modules...)
	}

	if filterKind == "" || filterKind == "provider" {
		providers, err := browseCollectProviders(cs, r, nsFilter, nsPublic, binding, allAccess, publicOnly, filterOS, filterArch)
		if err != nil {
			logger.Error("browse: failed to collect providers", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		items = append(items, providers...)
	}

	items = applyBrowseFilters(items, searchText, filterSynced, filterSeverity)
	sortBrowseItems(items, sortBy, sortDir)

	totalCount := len(items)
	start := min((page-1)*pageSize, totalCount)
	end := min(start+pageSize, totalCount)

	resp := BrowseResourceList{
		Items:      items[start:end],
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}

	if resp.Items == nil {
		resp.Items = []BrowseResource{}
	}

	json.NewEncoder(w).Encode(resp)
}

// handleBrowseResourceDetail returns the full drill-down payload for a single resource.
// GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
func handleBrowseResourceDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	kind := strings.ToLower(chi.URLParam(r, "kind"))
	name := chi.URLParam(r, "name")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsLabels, _ := browseGetNamespaceLabels(cs, r, namespace)
	nsPublic := isPublicNamespace(nsLabels)

	switch kind {
	case "module":
		rawModule, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("modules").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			logger.Error("browse: failed to get module", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var m opendepotv1alpha1.Module
		if err := json.Unmarshal(rawModule, &m); err != nil {
			logger.Error("browse: failed to unmarshal module", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		card := moduleToCard(m, pub)
		versions, _ := browseListModuleVersions(cs, r, namespace, name)
		enrichModuleCard(&card, versions)

		detail := BrowseResourceDetail{BrowseResource: card}
		detail.Versions = moduleVersionSummaries(versions)
		detail.SourceScanFindings = collectModuleSourceFindings(versions)
		detail.StorageConfig = storageConfigToBrowse(m.Spec.ModuleConfig.StorageConfig)
		detail.RepoOwner = m.Spec.ModuleConfig.RepoOwner
		detail.VersionHistoryLimit = m.Spec.ModuleConfig.VersionHistoryLimit
		detail.VersionConstraints = m.Spec.ModuleConfig.VersionConstraints
		detail.ReadmeContent = browseModuleReadme(cs, r, namespace, versions, resolveLatestModuleVersion(&card, versions))

		if m.Spec.ModuleConfig.GithubClientConfig != nil {
			detail.GithubConfig = &BrowseGithubConfig{
				UseAuthenticatedClient: m.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient,
			}
		}

		detail.DepotRef = browseDepotForModule(cs, r, namespace, name)
		json.NewEncoder(w).Encode(detail)

	case "provider":
		rawProvider, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("providers").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			logger.Error("browse: failed to get provider", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var p opendepotv1alpha1.Provider
		if err := json.Unmarshal(rawProvider, &p); err != nil {
			logger.Error("browse: failed to unmarshal provider", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "provider", p.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		card := providerToCard(p, pub)
		versions, _ := browseListProviderVersions(cs, r, namespace)
		versions = providerVersionsFor(versions, p.Name)
		enrichProviderCard(&card, p, versions, "", "")

		detail := BrowseResourceDetail{BrowseResource: card}
		detail.Versions = providerVersionSummaries(p, versions)
		if latestScan := findProviderSourceScanFromVersions(versions, ""); latestScan != nil {
			detail.SourceScanFindings = latestScan.Findings
		}

		detail.BinaryScanFindings = collectBinaryFindings(versions)
		detail.StorageConfig = storageConfigToBrowse(p.Spec.ProviderConfig.StorageConfig)
		detail.VersionHistoryLimit = p.Spec.ProviderConfig.VersionHistoryLimit
		detail.VersionConstraints = p.Spec.ProviderConfig.VersionConstraints

		if p.Spec.ProviderConfig.SourceRepository != nil && *p.Spec.ProviderConfig.SourceRepository != "" {
			detail.SourceRepository = *p.Spec.ProviderConfig.SourceRepository
		} else if p.Status.ResolvedSourceRepository != "" {
			detail.SourceRepository = p.Status.ResolvedSourceRepository
		}

		if p.Spec.ProviderConfig.GithubClientConfig != nil {
			detail.GithubConfig = &BrowseGithubConfig{
				UseAuthenticatedClient: p.Spec.ProviderConfig.GithubClientConfig.UseAuthenticatedClient,
			}
		}

		detail.DepotRef = browseDepotForProvider(cs, r, namespace, name)
		json.NewEncoder(w).Encode(detail)

	default:
		http.Error(w, "kind must be 'module' or 'provider'", http.StatusBadRequest)
	}
}

// moduleToCard converts a Module resource to a BrowseResource card.
func moduleToCard(m opendepotv1alpha1.Module, public bool) BrowseResource {
	return BrowseResource{
		Kind:          "module",
		Namespace:     m.Namespace,
		Name:          m.Name,
		Synced:        m.Status.Synced,
		SyncStatus:    m.Status.SyncStatus,
		Public:        public,
		Provider:      m.Spec.ModuleConfig.Provider,
		RepoURL:       derefString(m.Spec.ModuleConfig.RepoUrl),
		LatestVersion: derefString(m.Status.LatestVersion),
	}
}

// providerToCard converts a Provider resource to a BrowseResource card.
func providerToCard(p opendepotv1alpha1.Provider, public bool) BrowseResource {
	return BrowseResource{
		Kind:              "provider",
		Namespace:         p.Namespace,
		Name:              p.Name,
		Synced:            p.Status.Synced,
		SyncStatus:        p.Status.SyncStatus,
		Public:            public,
		LatestVersion:     derefString(p.Status.LatestVersion),
		ProviderNamespace: derefString(p.Spec.ProviderConfig.Namespace),
		UpstreamRegistry:  opendepotv1alpha1.ProviderUpstreamRegistry(&p.Spec.ProviderConfig),
	}
}

// enrichModuleCard sets ScanCounts and LastScanned on a module card using only the latest version.
func enrichModuleCard(card *BrowseResource, versions []opendepotv1alpha1.Version) {
	card.HasUnsyncedVersions = anyVersionUnsynced(versions)

	latestVersionStr := resolveLatestModuleVersion(card, versions)

	for i := range versions {
		v := &versions[i]
		if opendepotUtils.SanitizeVersion(v.Spec.Version) != latestVersionStr || v.Status.SourceScan == nil {
			continue
		}

		card.ScanCounts = browseScanCounts(v.Status.SourceScan.Findings)
		card.LastScanned = v.Status.SourceScan.ScannedAt
		return
	}
}

// resolveLatestModuleVersion returns the sanitized latest version string for a module, preferring
// card.LatestVersion (set from the Module's status) and falling back to the highest semver found
// in versions. Normalize to strip any leading "v" prefix so that "v44.2.0" matches "44.2.0".
func resolveLatestModuleVersion(card *BrowseResource, versions []opendepotv1alpha1.Version) string {
	latestVersionStr := opendepotUtils.SanitizeVersion(card.LatestVersion)
	if latestVersionStr != "" {
		return latestVersionStr
	}

	for _, v := range versions {
		nv := opendepotUtils.SanitizeVersion(v.Spec.Version)
		if latestVersionStr == "" || compareVersionDesc(nv, latestVersionStr) {
			latestVersionStr = nv
		}
	}

	return latestVersionStr
}

// browseModuleReadme resolves the base64-decoded README content for a module's latest version,
// if that Version has a ReadmeConfigMapRef set. Any failure to resolve the ConfigMap is non-fatal
// and results in a nil return so the UI simply omits the README section.
func browseModuleReadme(cs *kubernetes.Clientset, r *http.Request, namespace string, versions []opendepotv1alpha1.Version, latestVersionStr string) *string {
	var ref *opendepotv1alpha1.ReadmeConfigMapRef
	for i := range versions {
		if opendepotUtils.SanitizeVersion(versions[i].Spec.Version) != latestVersionStr {
			continue
		}

		if versions[i].Status.ReadmeConfigMapRef != nil {
			ref = versions[i].Status.ReadmeConfigMapRef
			break
		}
	}

	if ref == nil {
		return nil
	}

	raw, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1").
		Namespace(namespace).
		Resource("configmaps").
		Name(ref.Name).
		DoRaw(r.Context())
	if err != nil {
		return nil
	}

	var cm k8sConfigMap
	if err := json.Unmarshal(raw, &cm); err != nil {
		return nil
	}

	encoded, ok := cm.Data[ref.Key]
	if !ok {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}

	content := string(decoded)
	return &content
}

// enrichProviderCard sets Platforms, ScanCounts, and LastScanned on a provider card.
// ScanCounts reflects only the latest version's binary findings plus the provider-level source scan.
func enrichProviderCard(card *BrowseResource, p opendepotv1alpha1.Provider, versions []opendepotv1alpha1.Version, filterOS, filterArch string) {
	platformSet := make(map[string]ProviderPlatform)

	// Determine the latest version string: prefer provider status, fall back to highest semver.
	// Normalize to strip any leading "v" prefix so that "v3.2.4" matches "3.2.4".
	latestVersionStr := opendepotUtils.SanitizeVersion(derefString(p.Status.LatestVersion))
	if latestVersionStr == "" {
		for _, v := range versions {
			if v.Spec.ProviderConfigRef == nil || v.Spec.ProviderConfigRef.Name == nil {
				continue
			}

			if *v.Spec.ProviderConfigRef.Name != p.Name {
				continue
			}

			nv := opendepotUtils.SanitizeVersion(v.Spec.Version)
			if latestVersionStr == "" || compareVersionDesc(nv, latestVersionStr) {
				latestVersionStr = nv
			}
		}
	}

	var scanFindings []opendepotv1alpha1.SecurityFinding
	var scanTimes []string

	// latestBinaryByPlatform collects one Version per os/arch key at the latest semver.
	// Only the alphabetically-first platform's findings are used for the badge count so
	// that findings shared across platforms are not multiplied by the platform count.
	latestBinaryByPlatform := make(map[string]*opendepotv1alpha1.Version)

	for i := range versions {
		v := &versions[i]
		if v.Spec.ProviderConfigRef == nil || v.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ProviderConfigRef.Name != p.Name {
			continue
		}

		ss := strings.ToLower(v.Status.SyncStatus)
		if !v.Status.Synced || strings.Contains(ss, "failed") || strings.Contains(ss, "error") {
			card.HasUnsyncedVersions = true
		}

		osName := v.Spec.OperatingSystem
		arch := v.Spec.Architecture

		if filterOS != "" && osName != filterOS {
			continue
		}

		if filterArch != "" && arch != filterArch {
			continue
		}

		key := osName + "/" + arch
		platformSet[key] = ProviderPlatform{OS: osName, Arch: arch}

		if opendepotUtils.SanitizeVersion(v.Spec.Version) == latestVersionStr && v.Status.BinaryScan != nil {
			if _, exists := latestBinaryByPlatform[key]; !exists {
				latestBinaryByPlatform[key] = v
			}
		}
	}

	if len(latestBinaryByPlatform) > 0 {
		binaryKeys := make([]string, 0, len(latestBinaryByPlatform))
		for k := range latestBinaryByPlatform {
			binaryKeys = append(binaryKeys, k)
		}

		sort.Strings(binaryKeys)
		chosen := latestBinaryByPlatform[binaryKeys[0]]
		scanFindings = append(scanFindings, chosen.Status.BinaryScan.Findings...)
		scanTimes = append(scanTimes, chosen.Status.BinaryScan.ScannedAt)
	}

	if latestScan := findProviderSourceScanFromVersions(versions, ""); latestScan != nil {
		scanFindings = append(scanFindings, latestScan.Findings...)
		scanTimes = append(scanTimes, latestScan.ScannedAt)
	}

	platforms := make([]ProviderPlatform, 0, len(platformSet))
	for _, pl := range platformSet {
		platforms = append(platforms, pl)
	}

	sort.Slice(platforms, func(i, j int) bool {
		if platforms[i].OS != platforms[j].OS {
			return platforms[i].OS < platforms[j].OS
		}
		return platforms[i].Arch < platforms[j].Arch
	})

	card.Platforms = platforms
	if len(scanFindings) > 0 {
		card.ScanCounts = browseScanCounts(scanFindings)
	}

	card.LastScanned = latestOf(scanTimes...)
}

// browseListModuleVersions returns Version resources in namespace that belong to the named module.
func browseListModuleVersions(cs *kubernetes.Clientset, r *http.Request, namespace, moduleName string) ([]opendepotv1alpha1.Version, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list versions in %s: %w", namespace, err)
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version list: %w", err)
	}

	var result []opendepotv1alpha1.Version
	for _, v := range list.Items {
		if v.Spec.ModuleConfigRef == nil || v.Spec.ModuleConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ModuleConfigRef.Name != moduleName {
			continue
		}

		result = append(result, v)
	}

	return result, nil
}

// browseListProviderVersions returns all provider Version resources in the given namespace.
func browseListProviderVersions(cs *kubernetes.Clientset, r *http.Request, namespace string) ([]opendepotv1alpha1.Version, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list versions in %s: %w", namespace, err)
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version list: %w", err)
	}

	var result []opendepotv1alpha1.Version
	for _, v := range list.Items {
		if v.Spec.ProviderConfigRef == nil {
			continue
		}

		result = append(result, v)
	}

	return result, nil
}

// moduleVersionSummaries converts a slice of Version resources to BrowseVersionSummary.
func moduleVersionSummaries(versions []opendepotv1alpha1.Version) []BrowseVersionSummary {
	summaries := make([]BrowseVersionSummary, 0, len(versions))
	for _, v := range versions {
		s := BrowseVersionSummary{
			Name:             v.Name,
			Version:          v.Spec.Version,
			Synced:           v.Status.Synced,
			SyncStatus:       v.Status.SyncStatus,
			FileName:         v.Spec.FileName,
			Checksum:         v.Status.Checksum,
			ArchiveSizeBytes: v.Status.ArchiveSizeBytes,
		}

		if v.Status.SourceScan != nil {
			s.ScanCounts = browseScanCounts(v.Status.SourceScan.Findings)
			s.LastScanned = v.Status.SourceScan.ScannedAt
		}

		summaries = append(summaries, s)
	}

	return summaries
}

// providerVersionSummaries converts provider Version resources to BrowseVersionSummary.
func providerVersionSummaries(p opendepotv1alpha1.Provider, versions []opendepotv1alpha1.Version) []BrowseVersionSummary {
	summaries := make([]BrowseVersionSummary, 0)
	for _, v := range versions {
		if v.Spec.ProviderConfigRef == nil || v.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ProviderConfigRef.Name != p.Name {
			continue
		}

		s := BrowseVersionSummary{
			Name:             v.Name,
			Version:          opendepotUtils.SanitizeVersion(v.Spec.Version),
			Synced:           v.Status.Synced,
			SyncStatus:       v.Status.SyncStatus,
			OS:               v.Spec.OperatingSystem,
			Arch:             v.Spec.Architecture,
			FileName:         v.Spec.FileName,
			Checksum:         v.Status.Checksum,
			ArchiveSizeBytes: v.Status.ArchiveSizeBytes,
		}

		if v.Status.BinaryScan != nil {
			s.ScanCounts = browseScanCounts(v.Status.BinaryScan.Findings)
			s.LastScanned = v.Status.BinaryScan.ScannedAt
		}

		summaries = append(summaries, s)
	}

	return summaries
}

// collectModuleSourceFindings returns the IaC scan findings from the latest (highest) module version.
func collectModuleSourceFindings(versions []opendepotv1alpha1.Version) []opendepotv1alpha1.SecurityFinding {
	var latest *opendepotv1alpha1.Version
	for i := range versions {
		v := &versions[i]
		if v.Status.SourceScan == nil {
			continue
		}

		if latest == nil || compareVersionDesc(v.Spec.Version, latest.Spec.Version) {
			latest = v
		}
	}

	if latest == nil {
		return nil
	}

	return deduplicateFindings(latest.Status.SourceScan.Findings)
}

// findProviderSourceScanFromVersions returns the SourceScan for the given semver by inspecting
// Version.Status.SourceScan across the supplied Version slice. When version is empty the scan
// from the Version with the highest semver is returned. Returns nil when no match is found.
func findProviderSourceScanFromVersions(versions []opendepotv1alpha1.Version, version string) *opendepotv1alpha1.SourceScan {
	var best *opendepotv1alpha1.Version
	for i := range versions {
		v := &versions[i]
		if v.Status.SourceScan == nil {
			continue
		}

		if version != "" {
			if opendepotUtils.SanitizeVersion(v.Spec.Version) == version {
				return v.Status.SourceScan
			}
			continue
		}

		if best == nil || compareVersionDesc(v.Spec.Version, best.Spec.Version) {
			best = v
		}
	}

	if best != nil {
		return best.Status.SourceScan
	}

	return nil
}

// collectBinaryFindings returns a map of "os/arch" → []SecurityFinding using only the latest
// version's binary scan results for each platform.
// providerVersionsFor returns only the Version entries whose ProviderConfigRef.Name matches providerName.
func providerVersionsFor(versions []opendepotv1alpha1.Version, providerName string) []opendepotv1alpha1.Version {
	result := versions[:0:0]
	for i := range versions {
		v := &versions[i]
		if v.Spec.ProviderConfigRef != nil && v.Spec.ProviderConfigRef.Name != nil && *v.Spec.ProviderConfigRef.Name == providerName {
			result = append(result, versions[i])
		}
	}

	return result
}

func collectBinaryFindings(versions []opendepotv1alpha1.Version) map[string][]opendepotv1alpha1.SecurityFinding {
	return collectBinaryFindingsForVersion(versions, "")
}

// collectBinaryFindingsForVersion returns the binary scan findings keyed by "os/arch".
// When semver is non-empty only Version CRs matching that version are considered;
// otherwise the latest semver per platform is used (same behaviour as before).
func collectBinaryFindingsForVersion(versions []opendepotv1alpha1.Version, semver string) map[string][]opendepotv1alpha1.SecurityFinding {
	latestByPlatform := make(map[string]*opendepotv1alpha1.Version)
	for i := range versions {
		v := &versions[i]
		if v.Status.BinaryScan == nil || len(v.Status.BinaryScan.Findings) == 0 {
			continue
		}

		if semver != "" && opendepotUtils.SanitizeVersion(v.Spec.Version) != semver {
			continue
		}

		key := v.Spec.OperatingSystem + "/" + v.Spec.Architecture
		existing, ok := latestByPlatform[key]
		if !ok || compareVersionDesc(v.Spec.Version, existing.Spec.Version) {
			latestByPlatform[key] = v
		}
	}

	if len(latestByPlatform) == 0 {
		return nil
	}

	result := make(map[string][]opendepotv1alpha1.SecurityFinding, len(latestByPlatform))
	for key, v := range latestByPlatform {
		result[key] = deduplicateFindings(v.Status.BinaryScan.Findings)
	}

	return result
}

// deduplicateFindings removes duplicate SecurityFinding entries from a slice.
// Vulnerabilities are keyed by VulnerabilityID+PkgName+InstalledVersion;
// misconfigurations (empty InstalledVersion) are keyed by VulnerabilityID alone.
func deduplicateFindings(in []opendepotv1alpha1.SecurityFinding) []opendepotv1alpha1.SecurityFinding {
	seen := make(map[string]struct{})
	out := make([]opendepotv1alpha1.SecurityFinding, 0, len(in))
	for _, f := range in {
		var key string
		if f.InstalledVersion != "" {
			key = f.VulnerabilityID + "|" + f.PkgName + "|" + f.InstalledVersion
		} else {
			key = f.VulnerabilityID
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

// enrichVersionSummariesWithDownloads populates DownloadCount and LastDownloadedAt on each
// BrowseVersionSummary by issuing a single batch query against Valkey.
func enrichVersionSummariesWithDownloads(ctx context.Context, summaries []BrowseVersionSummary, namespace, kind, name string) {
	if len(summaries) == 0 {
		return
	}

	keys := make([]string, len(summaries))
	for i := range summaries {
		keys[i] = namespace + "/" + kind + "/" + name + "/" + summaries[i].Version
	}

	dlStats, err := batchVersionDownloadStats(ctx, statsClient, keys)
	if err != nil {
		logger.Error("browse: failed to query version download stats", "error", err)
		return
	}

	for i := range summaries {
		if s, ok := dlStats[keys[i]]; ok {
			summaries[i].DownloadCount = s.Count
			summaries[i].LastDownloadedAt = s.LastAt
		}
	}
}

// enrichResourceWithDownloads populates TotalDownloads and LastDownloadedAt on a
// BrowseResource using pre-fetched download stats from the batch query result.
func enrichResourceWithDownloads(resource *BrowseResource, dlStats map[string]resourceDownloadStats) {
	if dlStats == nil {
		return
	}

	key := resource.Namespace + "/" + resource.Kind + "/" + resource.Name
	if s, ok := dlStats[key]; ok {
		resource.TotalDownloads = s.Count
		resource.LastDownloadedAt = s.LastAt
	}
}

// applyBrowseFilters applies search text, synced, and severity filters.
func applyBrowseFilters(items []BrowseResource, searchText, filterSynced, filterSeverity string) []BrowseResource {
	if searchText == "" && filterSynced == "" && filterSeverity == "" {
		return items
	}

	minSeverityRank := -1
	if filterSeverity != "" {
		minSeverityRank = severityRank(filterSeverity)
	}

	filtered := make([]BrowseResource, 0, len(items))
	for _, item := range items {
		if searchText != "" {
			haystack := strings.Join([]string{
				strings.ToLower(item.Name),
				strings.ToLower(item.Namespace),
				strings.ToLower(item.Provider),
				strings.ToLower(item.ProviderNamespace),
				strings.ToLower(item.RepoURL),
				strings.ToLower(item.LatestVersion),
			}, " ")

			if !strings.Contains(haystack, searchText) {
				continue
			}
		}

		if filterSynced == "true" && !item.Synced {
			continue
		}

		if filterSynced == "false" && item.Synced {
			continue
		}

		if minSeverityRank >= 0 {
			hs := highestSeverity(item.ScanCounts)
			if hs == "" || severityRank(hs) > minSeverityRank {
				continue
			}
		}

		filtered = append(filtered, item)
	}

	return filtered
}

// sortBrowseItems sorts items in place by the given field and direction.
func sortBrowseItems(items []BrowseResource, by, dir string) {
	less := func(i, j int) bool {
		a, b := items[i], items[j]
		switch by {
		case "namespace":
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Name < b.Name
		case "latest_version":
			return a.LatestVersion < b.LatestVersion
		case "synced":
			if a.Synced != b.Synced {
				return a.Synced
			}

			return a.Name < b.Name
		case "severity":
			ra := severityRank(highestSeverity(a.ScanCounts))
			rb := severityRank(highestSeverity(b.ScanCounts))
			if ra != rb {
				return ra < rb
			}

			return a.Name < b.Name
		case "last_scanned":
			return a.LastScanned < b.LastScanned
		default: // "name"
			if a.Name != b.Name {
				return a.Name < b.Name
			}

			return a.Namespace < b.Namespace
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}

		return less(i, j)
	})
}

// derefString safely dereferences a *string, returning "" when nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// splitVersionParts splits a normalized version string into its constituent tokens
// by breaking on '.', '+', and '-' separators.
func splitVersionParts(v string) []string {
	var parts []string
	start := 0
	for i, c := range v {
		if c == '.' || c == '+' || c == '-' {
			if i > start {
				parts = append(parts, v[start:i])
			}
			start = i + 1
		}
	}
	if start < len(v) {
		parts = append(parts, v[start:])
	}
	return parts
}

// compareVersionDesc reports whether version string a is newer than b (i.e., a should
// sort before b in a descending-by-version listing). Mirrors the client-side
// compareVersionDesc function in page.tsx.
func compareVersionDesc(a, b string) bool {
	aParts := splitVersionParts(opendepotUtils.SanitizeVersion(a))
	bParts := splitVersionParts(opendepotUtils.SanitizeVersion(b))
	length := max(len(bParts), len(aParts))

	for i := range length {
		if i >= len(aParts) {
			return false
		}

		if i >= len(bParts) {
			return true
		}

		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		if aErr == nil && bErr == nil {
			if aNum != bNum {
				return aNum > bNum
			}
			continue
		}

		if aErr == nil {
			return true // numeric tokens sort before string tokens
		}

		if bErr == nil {
			return false
		}

		al := strings.ToLower(aParts[i])
		bl := strings.ToLower(bParts[i])
		if al != bl {
			return al > bl
		}
	}

	return false
}

// filterAndPaginateVersions applies in-memory text/status/os/arch filters and pagination
// to a pre-sorted slice of BrowseVersionSummary values. AvailableOS and AvailableArch
// are derived from the full (unfiltered) set so UI dropdowns stay populated while filters
// are active.
func filterAndPaginateVersions(all []BrowseVersionSummary, q, syncedStr, osFilter, archFilter string, page, pageSize int) BrowseVersionList {
	// Collect distinct OS/arch values from the unfiltered set.
	osSet := make(map[string]struct{})
	archSet := make(map[string]struct{})
	for _, v := range all {
		if v.OS != "" {
			osSet[v.OS] = struct{}{}
		}

		if v.Arch != "" {
			archSet[v.Arch] = struct{}{}
		}
	}

	// Apply filters.
	filtered := make([]BrowseVersionSummary, 0, len(all))
	for _, v := range all {
		if q != "" && !strings.Contains(strings.ToLower(v.Version), strings.ToLower(q)) {
			continue
		}

		if syncedStr != "" {
			ss := strings.ToLower(v.SyncStatus)
			problemStatus := strings.Contains(ss, "failed") || strings.Contains(ss, "error")
			problematic := !v.Synced || problemStatus
			if syncedStr == "true" && problematic {
				continue
			}

			if syncedStr == "false" && !problematic {
				continue
			}

			// progressing: not yet synced but not in a failed/error state
			if syncedStr == "progressing" && (v.Synced || problemStatus) {
				continue
			}
		}

		if osFilter != "" && !strings.EqualFold(v.OS, osFilter) {
			continue
		}

		if archFilter != "" && !strings.EqualFold(v.Arch, archFilter) {
			continue
		}

		filtered = append(filtered, v)
	}

	totalCount := len(filtered)
	start := min((page-1)*pageSize, totalCount)
	end := min(start+pageSize, totalCount)

	availableOS := make([]string, 0, len(osSet))
	for os := range osSet {
		availableOS = append(availableOS, os)
	}

	sort.Strings(availableOS)

	availableArch := make([]string, 0, len(archSet))
	for arch := range archSet {
		availableArch = append(availableArch, arch)
	}

	sort.Strings(availableArch)

	return BrowseVersionList{
		Items:         filtered[start:end],
		TotalCount:    totalCount,
		Page:          page,
		PageSize:      pageSize,
		AvailableOS:   availableOS,
		AvailableArch: availableArch,
	}
}

// handleBrowseVersionsList returns a paginated, filterable list of version summaries for a
// single module or provider resource.
// GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
func handleBrowseVersionsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	kind := strings.ToLower(chi.URLParam(r, "kind"))
	name := chi.URLParam(r, "name")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsLabels, _ := browseGetNamespaceLabels(cs, r, namespace)
	nsPublic := isPublicNamespace(nsLabels)

	// Parse query params.
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 {
		if ps > 100 {
			ps = 100
		}
		pageSize = ps
	}

	q := r.URL.Query().Get("q")
	syncedStr := r.URL.Query().Get("synced")
	osFilter := r.URL.Query().Get("os")
	archFilter := r.URL.Query().Get("arch")

	switch kind {
	case "module":
		rawModule, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("modules").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			logger.Error("browse: failed to get module", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var m opendepotv1alpha1.Module
		if err := json.Unmarshal(rawModule, &m); err != nil {
			logger.Error("browse: failed to unmarshal module", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		versions, _ := browseListModuleVersions(cs, r, namespace, name)
		summaries := moduleVersionSummaries(versions)
		enrichVersionSummariesWithDownloads(r.Context(), summaries, namespace, "module", name)
		sort.SliceStable(summaries, func(i, j int) bool {
			return compareVersionDesc(summaries[i].Version, summaries[j].Version)
		})

		result := filterAndPaginateVersions(summaries, q, syncedStr, osFilter, archFilter, page, pageSize)
		json.NewEncoder(w).Encode(result)

	case "provider":
		rawProvider, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("providers").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			logger.Error("browse: failed to get provider", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var p opendepotv1alpha1.Provider
		if err := json.Unmarshal(rawProvider, &p); err != nil {
			logger.Error("browse: failed to unmarshal provider", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "provider", p.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		versions, _ := browseListProviderVersions(cs, r, namespace)
		summaries := providerVersionSummaries(p, versions)
		enrichVersionSummariesWithDownloads(r.Context(), summaries, namespace, "provider", name)
		sort.SliceStable(summaries, func(i, j int) bool {
			return compareVersionDesc(summaries[i].Version, summaries[j].Version)
		})

		result := filterAndPaginateVersions(summaries, q, syncedStr, osFilter, archFilter, page, pageSize)
		json.NewEncoder(w).Encode(result)

	default:
		http.Error(w, "kind must be 'module' or 'provider'", http.StatusBadRequest)
	}
}

// handleBrowseScanFindings returns the scan findings for a single module or provider resource.
// GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings
// Optional query parameters:
//   - ?version=<semver>       selects a specific source scan version (modules and providers)
//   - ?binaryVersion=<semver> selects a specific binary scan version (providers only)
func handleBrowseScanFindings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	kind := strings.ToLower(chi.URLParam(r, "kind"))
	name := chi.URLParam(r, "name")
	requestedVersion := strings.TrimPrefix(r.URL.Query().Get("version"), "v")
	requestedBinaryVersion := opendepotUtils.SanitizeVersion(r.URL.Query().Get("binaryVersion"))

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsLabels, nsErr := browseGetNamespaceLabels(cs, r, namespace)
	if nsErr != nil {
		logger.Warn("browse: scan-findings: failed to get namespace labels", "namespace", namespace, "error", nsErr)
	}
	nsPublic := isPublicNamespace(nsLabels)

	switch kind {
	case "module":
		rawModule, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("modules").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			logger.Error("browse: failed to get module", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var m opendepotv1alpha1.Module
		if err := json.Unmarshal(rawModule, &m); err != nil {
			logger.Error("browse: failed to unmarshal module", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		versions, _ := browseListModuleVersions(cs, r, namespace, name)

		// Build the list of versions that have been scanned, sorted descending.
		var scannedVersions []string
		for _, v := range versions {
			if v.Status.SourceScan != nil {
				scannedVersions = append(scannedVersions, opendepotUtils.SanitizeVersion(v.Spec.Version))
			}
		}

		sort.Slice(scannedVersions, func(i, j int) bool {
			return compareVersionDesc(scannedVersions[i], scannedVersions[j])
		})

		// Select findings for the requested version, falling back to latest.
		var result BrowseScanFindings
		var selectedVersion *opendepotv1alpha1.Version
		normalizedRequest := opendepotUtils.SanitizeVersion(requestedVersion)
		for i := range versions {
			v := &versions[i]
			if v.Status.SourceScan == nil {
				continue
			}

			if normalizedRequest != "" && opendepotUtils.SanitizeVersion(v.Spec.Version) == normalizedRequest {
				selectedVersion = v
				break
			}

			if selectedVersion == nil || compareVersionDesc(opendepotUtils.SanitizeVersion(v.Spec.Version), opendepotUtils.SanitizeVersion(selectedVersion.Spec.Version)) {
				selectedVersion = v
			}
		}
		if selectedVersion != nil {
			result.SourceScanFindings = deduplicateFindings(selectedVersion.Status.SourceScan.Findings)
			result.SelectedVersion = opendepotUtils.SanitizeVersion(selectedVersion.Spec.Version)
		}

		result.ScannedVersions = scannedVersions
		json.NewEncoder(w).Encode(result)

	case "provider":
		rawProvider, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("providers").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			logger.Error("browse: failed to get provider", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var p opendepotv1alpha1.Provider
		if err := json.Unmarshal(rawProvider, &p); err != nil {
			logger.Error("browse: failed to unmarshal provider", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "provider", p.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		versions, _ := browseListProviderVersions(cs, r, namespace)
		versions = providerVersionsFor(versions, p.Name)
		result := BrowseScanFindings{
			BinaryScanFindings: collectBinaryFindingsForVersion(versions, requestedBinaryVersion),
		}

		requestedSemver := opendepotUtils.SanitizeVersion(requestedVersion)
		if scan := findProviderSourceScanFromVersions(versions, requestedSemver); scan != nil {
			result.SourceScanFindings = deduplicateFindings(scan.Findings)
			result.SelectedVersion = requestedSemver
		}

		// Build scannedVersions from distinct semver values that have a SourceScan result.
		seenSource := make(map[string]struct{})
		var scannedVersions []string
		for i := range versions {
			v := &versions[i]
			if v.Status.SourceScan == nil {
				continue
			}

			sv := opendepotUtils.SanitizeVersion(v.Spec.Version)
			if _, exists := seenSource[sv]; exists {
				continue
			}

			seenSource[sv] = struct{}{}
			scannedVersions = append(scannedVersions, sv)
		}

		if len(scannedVersions) > 0 {
			sort.Slice(scannedVersions, func(i, j int) bool {
				return compareVersionDesc(scannedVersions[i], scannedVersions[j])
			})
			result.ScannedVersions = scannedVersions
		}

		// Build binaryVersions from distinct semver values that have a BinaryScan result
		// with at least one finding. Versions scanned clean (BinaryScan != nil but Findings
		// empty) are excluded so the dropdown only lists versions that actually have findings.
		seenBinary := make(map[string]struct{})
		var binaryVersions []string
		for i := range versions {
			v := &versions[i]
			if v.Status.BinaryScan == nil || len(v.Status.BinaryScan.Findings) == 0 {
				continue
			}

			sv := opendepotUtils.SanitizeVersion(v.Spec.Version)
			if _, exists := seenBinary[sv]; exists {
				continue
			}

			seenBinary[sv] = struct{}{}
			binaryVersions = append(binaryVersions, sv)
		}

		if len(binaryVersions) > 0 {
			sort.Slice(binaryVersions, func(i, j int) bool {
				return compareVersionDesc(binaryVersions[i], binaryVersions[j])
			})
			result.BinaryVersions = binaryVersions
		}

		json.NewEncoder(w).Encode(result)

	default:
		http.Error(w, "kind must be 'module' or 'provider'", http.StatusBadRequest)
	}
}

// handleBrowseDepots returns a list of all Depot resources visible to the caller.
// GET /opendepot/ui/v1/depots
func handleBrowseDepots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("depots").
		DoRaw(r.Context())

	if err != nil {
		logger.Error("browse: failed to list depots", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var list opendepotv1alpha1.DepotList
	if err := json.Unmarshal(raw, &list); err != nil {
		logger.Error("browse: failed to unmarshal depot list", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	allNamespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: depots: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsPublic := make(map[string]bool, len(allNamespaces))
	for _, ns := range allNamespaces {
		nsPublic[ns.Metadata.Name] = isPublicNamespace(ns.Metadata.Labels)
	}

	// showAll: anonymous-auth mode or authenticated user with a GroupBinding.
	showAll := allAccess || binding != nil

	result := BrowseDepotList{Items: []BrowseDepot{}}
	for _, d := range list.Items {
		pub := nsPublic[d.Namespace] && isPublicResource(d.Labels)
		// Depots are not directly governed by GroupBinding resource lists; visibility
		// is determined at the namespace level: public depots are visible to everyone,
		// non-public depots are visible only in anonymous-auth mode or to authenticated
		// users with a GroupBinding.
		if !pub && !showAll {
			continue
		}

		item := BrowseDepot{
			Namespace:              d.Namespace,
			Name:                   d.Name,
			Modules:                d.Status.Modules,
			Providers:              d.Status.Providers,
			PollingIntervalMinutes: d.Spec.PollingIntervalMinutes,
		}

		if d.Spec.GlobalConfig != nil && d.Spec.GlobalConfig.StorageConfig != nil {
			sc := storageConfigToBrowse(d.Spec.GlobalConfig.StorageConfig)
			if sc != nil {
				item.StorageBackend = sc.Backend
			}
		}

		result.Items = append(result.Items, item)
	}

	json.NewEncoder(w).Encode(result)
}

// handleBrowseDepotsGraph builds and returns the depot relationship graph.
// GET /opendepot/ui/v1/depots/graph
func handleBrowseDepotsGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: graph: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	allNamespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: graph: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsPublic := make(map[string]bool, len(allNamespaces))
	for _, nsObj := range allNamespaces {
		nsPublic[nsObj.Metadata.Name] = isPublicNamespace(nsObj.Metadata.Labels)
	}

	showAll := allAccess || binding != nil
	ns := r.URL.Query().Get("namespace")

	// Fetch depots, modules and providers in parallel using goroutines.
	type depotResult struct {
		list opendepotv1alpha1.DepotList
		err  error
	}

	type moduleResult struct {
		list opendepotv1alpha1.ModuleList
		err  error
	}

	type providerResult struct {
		list opendepotv1alpha1.ProviderList
		err  error
	}

	depotCh := make(chan depotResult, 1)
	moduleCh := make(chan moduleResult, 1)
	providerCh := make(chan providerResult, 1)

	go func() {
		req := cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Resource("depots")
		if ns != "" {
			req = cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(ns).Resource("depots")
		}

		raw, err := req.DoRaw(r.Context())
		if err != nil {
			depotCh <- depotResult{err: err}
			return
		}

		var list opendepotv1alpha1.DepotList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			depotCh <- depotResult{err: unmarshalErr}
			return
		}

		depotCh <- depotResult{list: list}
	}()

	go func() {
		req := cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Resource("modules")
		if ns != "" {
			req = cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(ns).Resource("modules")
		}

		raw, err := req.DoRaw(r.Context())
		if err != nil {
			moduleCh <- moduleResult{err: err}
			return
		}

		var list opendepotv1alpha1.ModuleList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			moduleCh <- moduleResult{err: unmarshalErr}
			return
		}

		moduleCh <- moduleResult{list: list}
	}()

	go func() {
		req := cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Resource("providers")
		if ns != "" {
			req = cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(ns).Resource("providers")
		}

		raw, err := req.DoRaw(r.Context())
		if err != nil {
			providerCh <- providerResult{err: err}
			return
		}

		var list opendepotv1alpha1.ProviderList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			providerCh <- providerResult{err: unmarshalErr}
			return
		}

		providerCh <- providerResult{list: list}
	}()

	dr := <-depotCh
	mr := <-moduleCh
	pr := <-providerCh

	if dr.err != nil {
		logger.Error("browse: graph: failed to list depots", "error", dr.err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if mr.err != nil {
		logger.Error("browse: graph: failed to list modules", "error", mr.err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if pr.err != nil {
		logger.Error("browse: graph: failed to list providers", "error", pr.err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	graph := BrowseDepotGraph{
		Depots:      []BrowseGraphDepot{},
		Modules:     []BrowseGraphModule{},
		Providers:   []BrowseGraphProvider{},
		Edges:       []BrowseGraphEdge{},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Build depot nodes.
	for _, d := range dr.list.Items {
		pub := nsPublic[d.Namespace] && isPublicResource(d.Labels)
		if !pub && !showAll {
			continue
		}

		depotID := fmt.Sprintf("depot/%s/%s", d.Namespace, d.Name)
		node := BrowseGraphDepot{
			ID:                   depotID,
			Namespace:            d.Namespace,
			Name:                 d.Name,
			ManagedModuleNames:   d.Status.Modules,
			ManagedProviderNames: d.Status.Providers,
		}

		node.PollingIntervalMinutes = d.Spec.PollingIntervalMinutes
		if d.Spec.GlobalConfig != nil && d.Spec.GlobalConfig.StorageConfig != nil {
			sc := storageConfigToBrowse(d.Spec.GlobalConfig.StorageConfig)
			if sc != nil {
				node.StorageBackend = sc.Backend
			}
		}

		graph.Depots = append(graph.Depots, node)

		// Edges: depot → module
		for _, mRef := range d.Status.Modules {
			parts := strings.SplitN(mRef, "/", 2)
			if len(parts) != 2 {
				continue
			}

			targetID := fmt.Sprintf("module/%s/%s", parts[0], parts[1])
			graph.Edges = append(graph.Edges, BrowseGraphEdge{
				ID:     fmt.Sprintf("edge/%s/%s", depotID, targetID),
				Source: depotID,
				Target: targetID,
			})
		}

		// Edges: depot → provider
		for _, pRef := range d.Status.Providers {
			parts := strings.SplitN(pRef, "/", 2)
			if len(parts) != 2 {
				continue
			}

			targetID := fmt.Sprintf("provider/%s/%s", parts[0], parts[1])
			graph.Edges = append(graph.Edges, BrowseGraphEdge{
				ID:     fmt.Sprintf("edge/%s/%s", depotID, targetID),
				Source: depotID,
				Target: targetID,
			})
		}
	}

	// Build module nodes.
	for _, m := range mr.list.Items {
		pub := nsPublic[m.Namespace] && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
			continue
		}

		moduleID := fmt.Sprintf("module/%s/%s", m.Namespace, m.Name)
		synced := m.Status.SyncStatus == "Synced"
		node := BrowseGraphModule{
			ID:         moduleID,
			Namespace:  m.Namespace,
			Name:       m.Name,
			Provider:   m.Spec.ModuleConfig.Provider,
			Synced:     synced,
			SyncStatus: m.Status.SyncStatus,
		}

		if m.Spec.ModuleConfig.RepoUrl != nil {
			node.RepoURL = *m.Spec.ModuleConfig.RepoUrl
		}

		if m.Status.LatestVersion != nil {
			node.LatestVersion = *m.Status.LatestVersion
		}

		graph.Modules = append(graph.Modules, node)
	}

	// Build provider nodes.
	for _, p := range pr.list.Items {
		pub := nsPublic[p.Namespace] && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "provider", p.Name) {
			continue
		}

		providerID := fmt.Sprintf("provider/%s/%s", p.Namespace, p.Name)
		synced := p.Status.SyncStatus == "Synced"
		graph.Providers = append(graph.Providers, BrowseGraphProvider{
			ID:                providerID,
			Namespace:         p.Namespace,
			Name:              p.Name,
			ProviderNamespace: derefString(p.Spec.ProviderConfig.Namespace),
			UpstreamRegistry:  opendepotv1alpha1.ProviderUpstreamRegistry(&p.Spec.ProviderConfig),
			Synced:            synced,
		})
	}

	graph.Summary = BrowseGraphSummary{
		TotalDepots:    len(graph.Depots),
		TotalModules:   len(graph.Modules),
		TotalProviders: len(graph.Providers),
	}

	json.NewEncoder(w).Encode(graph)
}
