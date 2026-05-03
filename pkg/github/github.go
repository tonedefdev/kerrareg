package github

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v81/github"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// jwtTransport is a custom HTTP transport that adds the JWT to the Authorization header.
type jwtTransport struct {
	Transport http.RoundTripper
	JWT       string
}

// GithubClientConfig defines the configuration for an authenticated Github client.
type GithubClientConfig struct {
	// The Github application's ID.
	AppID int64
	// The Github application's install ID.
	InstallationID int64
	// The Github application's private key as a byte slice.
	PrivateKeyData []byte
}

// CreateGithubClient creates an authenticated client with the provided GithubClientConfig.
// If the client config is nil a github.Client is returned with a default http.Client type.
func CreateGithubClient(ctx context.Context, useAuthenticatedClient bool, githubConfig *GithubClientConfig) (*github.Client, error) {
	if useAuthenticatedClient && githubConfig == nil {
		return nil, fmt.Errorf("resource is marked to UseAuthenticatedClient but GithubClientConfig is nil")
	}

	if useAuthenticatedClient && githubConfig != nil {
		authClient, err := GenerateAuthenticatedGithubClient(ctx, githubConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to generate authenticated github client: %v", err)
		}
		return authClient, nil
	}

	return github.NewClient(nil), nil
}

// GetModuleArchiveFromRef gets a module from Github based on its ref and returns a byte slice and the file's base64 encoded SHA256 checksum.
func GetModuleArchiveFromRef(ctx context.Context, log logr.Logger, githubClient *github.Client, version *opendepotv1alpha1.Version, format github.ArchiveFormat) (moduleBytes []byte, checksum *string, err error) {
	ref := version.Spec.Version
	if !strings.HasPrefix(ref, "v") {
		ref = "v" + ref
	}

	var moduleReq *http.Response
	moduleReq, err = GetArchiveRequest(ctx, githubClient, version, format, ref)
	if err != nil {
		return nil, nil, err
	}
	defer moduleReq.Body.Close()

	log.V(5).Info("module req status code with 'v' prefix", "statusCode", moduleReq.StatusCode)

	if moduleReq.StatusCode != 200 {
		// If we get a 404 not found error it may be because the tag is prefixed without a 'v'
		// so we try again without the 'v' prefix before returning an error.
		if moduleReq.StatusCode == 404 {
			var moduleReq *http.Response

			refNoV := strings.TrimPrefix(ref, "v")
			moduleReq, err = GetArchiveRequest(ctx, githubClient, version, format, refNoV)
			if err != nil {
				return nil, nil, err
			}
			defer moduleReq.Body.Close()

			log.V(5).Info("module req status code without 'v' prefix", "statusCode", moduleReq.StatusCode)

			moduleBytes, err = io.ReadAll(moduleReq.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read module archive data: %w", err)
			}

			log.V(5).Info("module req bytes length", "length", len(moduleBytes))

			sha256Sum := sha256.Sum256(moduleBytes)
			checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
			checksum = &checksumSha256
			return
		}

		return nil, nil, fmt.Errorf("failed to get module archive from Github: status code %d", moduleReq.StatusCode)
	}

	moduleBytes, err = io.ReadAll(moduleReq.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read module archive data: %w", err)
	}

	log.V(5).Info("module req bytes length", "length", len(moduleBytes))

	sha256Sum := sha256.Sum256(moduleBytes)
	checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
	checksum = &checksumSha256

	return
}

// GetArchiveRequest retrieves the archive link for a given repository and reference (branch, tag, or commit SHA).
func GetArchiveRequest(ctx context.Context, githubClient *github.Client, version *opendepotv1alpha1.Version, format github.ArchiveFormat, ref string) (*http.Response, error) {
	al, alResp, err := githubClient.Repositories.GetArchiveLink(ctx, version.Spec.ModuleConfigRef.RepoOwner, *version.Spec.ModuleConfigRef.Name, format, &github.RepositoryContentGetOptions{
		Ref: ref,
	}, 10)

	if err != nil {
		if alResp != nil {
			alResp.Body.Close()
		}
		return nil, err
	}

	defer alResp.Body.Close()

	if al == nil || alResp == nil {
		return nil, fmt.Errorf("the response from the Github API was nil")
	}

	if alResp.StatusCode != 302 {
		return nil, fmt.Errorf("failed to get Github archive link: status code %d", alResp.StatusCode)
	}

	archiveReq, err := http.NewRequestWithContext(ctx, http.MethodGet, al.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request for archive link: %w", err)
	}

	archiveResp, err := http.DefaultClient.Do(archiveReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request for archive link: %w", err)
	}

	return archiveResp, nil
}

// GenerateGithubClient creates a GitHub client using a GitHub Application for authentication.
func GenerateAuthenticatedGithubClient(ctx context.Context, githubClientConfig *GithubClientConfig) (*github.Client, error) {
	// Parse the private key
	block, _ := pem.Decode(githubClientConfig.PrivateKeyData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("failed to decode PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create a JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Minute * 10).Unix(),
		"iss": githubClientConfig.AppID,
	})

	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Create a custom HTTP client with the JWT in the Authorization header
	jwtHTTPClient := &http.Client{
		Transport: &jwtTransport{
			Transport: http.DefaultTransport,
			JWT:       signedToken,
		},
	}
	jwtClient := github.NewClient(jwtHTTPClient)

	// Use the JWT-authenticated client to fetch the installation token
	instToken, _, err := jwtClient.Apps.CreateInstallationToken(ctx, githubClientConfig.InstallationID, &github.InstallationTokenOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create installation token: %w", err)
	}

	// Create an authenticated GitHub client with the installation token
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: instToken.GetToken()})
	oauthClient := oauth2.NewClient(ctx, ts)
	return github.NewClient(oauthClient), nil
}

// GetGithubApplicationSecret retrieves the opendepot-github-application-secret kubernetes secret from the cluster
// using the client received by k8sClient. It returns a GithubClientConfig for making authenticated requests to the Github API.
// The k8sClient parameter should be received by the controller's client.
func GetGithubApplicationSecret(ctx context.Context, k8sClient client.Client, secretNamespace string) (*GithubClientConfig, error) {
	object := client.ObjectKey{
		Name:      opendepotv1alpha1.OpenDepotGithubSecretName,
		Namespace: secretNamespace,
	}

	secret := corev1.Secret{}
	if err := k8sClient.Get(ctx, object, &secret); err != nil {
		return nil, err
	}

	appID, err := strconv.ParseInt(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID, err)
	}

	installID, err := strconv.ParseInt(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID, err)
	}

	keyData, err := base64.StdEncoding.DecodeString(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey]))
	if err != nil {
		return nil, fmt.Errorf("unable to decode '%s': %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey, err)
	}

	githubClientConfig := &GithubClientConfig{
		AppID:          appID,
		InstallationID: installID,
		PrivateKeyData: keyData,
	}

	return githubClientConfig, nil
}

// GetProviderGoMod fetches the go.mod file for a provider version from its GitHub source repository
// using the provided GitHub client (authenticated or unauthenticated). Both 'v{version}' and bare
// '{version}' ref formats are tried, matching the retry pattern used by GetModuleArchiveFromRef.
func GetProviderGoMod(ctx context.Context, githubClient *github.Client, owner, repo, version string) ([]byte, error) {
	bare := strings.TrimPrefix(version, "v")
	for _, ref := range []string{"v" + bare, bare} {
		fileContent, _, _, err := githubClient.Repositories.GetContents(ctx, owner, repo, "go.mod",
			&github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			continue
		}
		if fileContent == nil {
			continue
		}
		data, err := fileContent.GetContent()
		if err != nil {
			return nil, fmt.Errorf("failed to decode go.mod content for %s/%s@%s: %w", owner, repo, ref, err)
		}
		return []byte(data), nil
	}
	return nil, fmt.Errorf("go.mod not found in %s/%s at version %s", owner, repo, version)
}

// RoundTrip sets the authorization header and executes a single HTTP transaction, returning a Response for the provided Request.
func (t *jwtTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.JWT))
	return t.Transport.RoundTrip(req)
}
