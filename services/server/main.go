package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
)

var (
	logger                              *slog.Logger
	opendepotAnonymousAuth              *bool
	opendepotUseBearerToken             *bool
	opendepotOIDCIssuerURL              *string
	opendepotOIDCClientID               *string
	opendepotOIDCGroupsClaim            *string
	opendepotOIDCAllowSAFallback        *bool
	opendepotOIDCAllowClientCredentials *bool
	opendepotOIDCUIClientID             *string
	opendepotOIDCAuthzURL               *string
	opendepotOIDCTokenURL               *string
	opendepotOIDCDexProxyEnabled        *bool
	opendepotOIDCDexInternalURL         *string
	opendepotServerNamespace            *string
	opendepotFilesystemMountPath        *string

	// statsClient is the Valkey/Redis client used to track download events.
	// Stats tracking is always enabled; the server will not start if Valkey is unreachable.
	statsClient *redis.Client

	// oidcVerifier is set at startup when --oidc-issuer-url and --oidc-client-id
	// are both provided. It is used to validate OIDC JWTs on every request.
	oidcVerifier *gooidc.IDTokenVerifier
	// oidcCCVerifier is like oidcVerifier but skips the audience check. It is
	// used only for the client credentials path when --oidc-allow-client-credentials
	// is set, to accept tokens issued for a different client ID by the same Dex.
	oidcCCVerifier *gooidc.IDTokenVerifier
	// oidcUIVerifier is like oidcVerifier but uses the UI client ID as the expected
	// audience. Set when --oidc-ui-client-id is provided to allow the UI's OIDC
	// tokens (issued under a separate client registration) to be accepted by the
	// browse endpoints for GroupBinding evaluation.
	oidcUIVerifier *gooidc.IDTokenVerifier
	// oidcProvider is the discovered OIDC provider; its Endpoint() is used to
	// populate the login.v1 service discovery response.
	oidcProvider *gooidc.Provider
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
}

func main() {
	opendepotAnonymousAuth = flag.Bool("anonymous-auth", false, "when true use the server's service account to serve modules and versions without requiring client authentication")
	opendepotUseBearerToken = flag.Bool("use-bearer-token", false, "when true use a bearer token instead of a base64 encoded kubeconfig to authenticate with the kubernetes API server")
	opendepotOIDCIssuerURL = flag.String("oidc-issuer-url", "", "OIDC issuer URL (e.g. https://dex.example.com/dex); when set together with --oidc-client-id the server validates OIDC JWTs and uses its own service account for Kubernetes API calls")
	opendepotOIDCClientID = flag.String("oidc-client-id", "", "OIDC client ID; must be set together with --oidc-issuer-url")
	opendepotOIDCGroupsClaim = flag.String("oidc-groups-claim", "groups", "JWT claim name containing the OIDC groups; used to match GroupBinding expressions")
	opendepotOIDCAllowSAFallback = flag.Bool("oidc-allow-sa-fallback", false, "when true and OIDC mode is active, Kubernetes ServiceAccount bearer tokens with a non-OIDC issuer are authenticated via the bearer token path using the caller's own RBAC; GroupBinding is bypassed for SA tokens")
	opendepotOIDCAllowClientCredentials = flag.Bool("oidc-allow-client-credentials", false, "when true and OIDC mode is active, Dex client credentials tokens (audience != oidc-client-id) are accepted; the token's sub claim is mapped to a virtual group \"client:<sub>\" and evaluated against GroupBinding resources")
	opendepotOIDCUIClientID = flag.String("oidc-ui-client-id", "", "when set, tokens issued to this OIDC client ID are also accepted by browse endpoints; use when the UI registers as a separate confidential client from the main --oidc-client-id")
	opendepotOIDCAuthzURL = flag.String("oidc-authz-url", "", "override the authorization URL advertised in /.well-known/terraform.json login.v1; when blank uses the authz URL from the OIDC provider discovery document")
	opendepotOIDCTokenURL = flag.String("oidc-token-url", "", "override the token URL advertised in /.well-known/terraform.json login.v1; when blank uses the token URL from the OIDC provider discovery document")
	opendepotOIDCDexProxyEnabled = flag.Bool("oidc-dex-proxy-enabled", false, "when true, the server reverse-proxies /dex/* requests to the internal Dex service so Dex never needs to be exposed on its own public ingress; requires --oidc-issuer-url to be the external issuer URL and --oidc-dex-internal-url to be set")
	opendepotOIDCDexInternalURL = flag.String("oidc-dex-internal-url", "", "internal (in-cluster) Dex base URL used for OIDC discovery, JWKS fetching, and reverse-proxying /dex/* requests; required when --oidc-dex-proxy-enabled is set")
	opendepotServerNamespace = flag.String("namespace", "opendepot-system", "namespace where GroupBinding resources are managed")
	opendepotFilesystemMountPath = flag.String("filesystem-mount-path", "/data/modules", "allowed root path for filesystem module storage; download requests for paths outside this prefix are rejected")
	opendepotValkeyAddr := flag.String("stats-valkey-addr", "valkey:6379", "address of the Valkey/Redis instance used for download stats tracking")
	opendepotCertPath := flag.String("tls-cert-path", "", "path to TLS certificate file for HTTPS server")
	opendepotCertKey := flag.String("tls-cert-key", "", "path to TLS certificate key file for HTTPS server")
	flag.Parse()

	client := redis.NewClient(&redis.Options{
		Addr:     *opendepotValkeyAddr,
		Password: os.Getenv("OPENDEPOT_VALKEY_PASSWORD"),
	})

	const maxAttempts = 10
	var pingErr error
	for i := range maxAttempts {
		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr = client.Ping(pingCtx).Err()
		cancel()

		if pingErr == nil {
			break
		}

		logger.Warn("stats: waiting for Valkey", "addr", *opendepotValkeyAddr, "attempt", i+1, "error", pingErr)
		time.Sleep(3 * time.Second)
	}

	if pingErr != nil {
		logger.Error("stats: failed to connect to Valkey", "addr", *opendepotValkeyAddr, "error", pingErr)
		os.Exit(1)
	}

	statsClient = client
	logger.Info("stats tracking enabled", "addr", *opendepotValkeyAddr)

	if (*opendepotOIDCIssuerURL == "") != (*opendepotOIDCClientID == "") {
		logger.Error("--oidc-issuer-url and --oidc-client-id must both be set or both be empty")
		os.Exit(1)
	}

	if *opendepotOIDCDexProxyEnabled {
		if *opendepotOIDCIssuerURL == "" {
			logger.Error("--oidc-dex-proxy-enabled requires --oidc-issuer-url to be set to the external, publicly reachable issuer URL")
			os.Exit(1)
		}

		if *opendepotOIDCDexInternalURL == "" {
			logger.Error("--oidc-dex-proxy-enabled requires --oidc-dex-internal-url to be set to Dex's internal (in-cluster) base URL")
			os.Exit(1)
		}
	}

	for flagName, rawURL := range map[string]string{
		"--oidc-authz-url":        *opendepotOIDCAuthzURL,
		"--oidc-token-url":        *opendepotOIDCTokenURL,
		"--oidc-dex-internal-url": *opendepotOIDCDexInternalURL,
	} {
		if rawURL == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			logger.Error("flag must be a well-formed http or https URL", "flag", flagName, "value", rawURL)
			os.Exit(1)
		}
	}

	if *opendepotOIDCIssuerURL != "" {
		ctx := context.Background()
		var provider *gooidc.Provider
		var err error

		if *opendepotOIDCDexProxyEnabled {
			// InsecureIssuerURLContext skips go-oidc's discovery issuer check so
			// discovery can be fetched from Dex's internal address while the
			// external issuer URL is still used everywhere else. Issuer
			// consistency is instead enforced by newInternalDexKeySet's jwks_uri
			// prefix check, and every JWT's iss claim is verified against the
			// external issuer via a manually-constructed gooidc.NewVerifier below.
			discoveryCtx := gooidc.InsecureIssuerURLContext(ctx, *opendepotOIDCIssuerURL)
			provider, err = gooidc.NewProvider(discoveryCtx, *opendepotOIDCDexInternalURL)
		} else {
			provider, err = gooidc.NewProvider(ctx, *opendepotOIDCIssuerURL)
		}

		if err != nil {
			logger.Error("failed to discover OIDC provider", "issuerUrl", *opendepotOIDCIssuerURL, "error", err)
			os.Exit(1)
		}

		oidcProvider = provider

		var keySet gooidc.KeySet
		if *opendepotOIDCDexProxyEnabled {
			keySet, err = newInternalDexKeySet(ctx, provider, *opendepotOIDCIssuerURL, *opendepotOIDCDexInternalURL)
			if err != nil {
				logger.Error("failed to configure internal Dex JWKS key set", "error", err)
				os.Exit(1)
			}

			logger.Info("Dex reverse proxy mode enabled", "internalDexUrl", *opendepotOIDCDexInternalURL)
		}

		newVerifier := func(cfg *gooidc.Config) *gooidc.IDTokenVerifier {
			if *opendepotOIDCDexProxyEnabled {
				return gooidc.NewVerifier(*opendepotOIDCIssuerURL, keySet, cfg)
			}

			return provider.Verifier(cfg)
		}

		oidcVerifier = newVerifier(&gooidc.Config{ClientID: *opendepotOIDCClientID})
		if *opendepotOIDCAllowClientCredentials {
			oidcCCVerifier = newVerifier(&gooidc.Config{SkipClientIDCheck: true})
		}

		if *opendepotOIDCUIClientID != "" {
			oidcUIVerifier = newVerifier(&gooidc.Config{ClientID: *opendepotOIDCUIClientID})
			logger.Info("OIDC UI client ID configured", "uiClientId", *opendepotOIDCUIClientID)
		}

		logger.Info("OIDC auth mode enabled", "issuerUrl", *opendepotOIDCIssuerURL, "clientId", *opendepotOIDCClientID)
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Get("/.well-known/terraform.json", serviceDiscoveryHandler)
	r.Get("/opendepot/modules/v1/{namespace}/{name}/{system}/versions", getModuleVersions)
	r.Get("/opendepot/modules/v1/{namespace}/{name}/{system}/{version}/download", getDownloadModuleUrl)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/versions", getProviderVersions)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}", getProviderPackageMetadata)
	r.Get("/opendepot/providers/v1/download/{namespace}/{type}/{version}", serveProviderPackageDownload)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}", getProviderPackageSHA256SUMS)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}", getProviderPackageSHA256SUMSSignature)
	r.Get("/opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/index.json", getProviderMirrorVersions)
	r.Get("/opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{versionFile}", getProviderMirrorArchives)
	r.Get("/opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{version}/{os}/{arch}/{filename}", serveProviderMirrorArchive)

	r.Get("/opendepot/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}", serveModuleFromAzureBlob)
	r.Get("/opendepot/modules/v1/download/fileSystem/{directory}/{name}/{fileName}", serveModuleFromFileSystem)
	r.Get("/opendepot/modules/v1/download/gcs/{bucket}/{name}/{fileName}", serveModuleFromGCS)
	r.Get("/opendepot/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}", serveModuleFromS3)

	r.Get("/opendepot/ui/v1/namespaces", handleBrowseNamespaces)
	r.Get("/opendepot/ui/v1/resources", handleBrowseResources)
	r.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}", handleBrowseResourceDetail)
	r.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions", handleBrowseVersionsList)
	r.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings", handleBrowseScanFindings)
	r.Get("/opendepot/ui/v1/depots", handleBrowseDepots)
	r.Get("/opendepot/ui/v1/depots/graph", handleBrowseDepotsGraph)
	r.Get("/opendepot/ui/v1/stats", handleBrowseStats)

	if *opendepotOIDCDexProxyEnabled {
		dexProxyHandler, err := newDexProxyHandler(*opendepotOIDCDexInternalURL)
		if err != nil {
			logger.Error("failed to configure Dex reverse proxy", "error", err)
			os.Exit(1)
		}

		r.Handle("/dex/*", dexProxyHandler)
	}

	if *opendepotCertPath != "" && *opendepotCertKey != "" {
		logger.Info("Server started and listening on port 8080 with TLS")
		if err := http.ListenAndServeTLS(":8080", *opendepotCertPath, *opendepotCertKey, r); err != nil {
			logger.Error("Failed to start server with TLS", "error", err)
		}
	} else {
		logger.Info("Server started and listening on default port: 8080 without TLS. For secure communication, provide paths to TLS certificate and key using --tls-cert-path and --tls-cert-key flags.")
		if err := http.ListenAndServe(":8080", r); err != nil {
			logger.Error("Failed to start server", "error", err)
		}
	}
}
