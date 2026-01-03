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
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/go-github/v50/github"
	"golang.org/x/oauth2"

	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

// jwtTransport is a custom HTTP transport that adds the JWT to the Authorization header
type jwtTransport struct {
	Transport http.RoundTripper
	JWT       string
}

type GithubClientConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyData []byte
}

// CreateGithubClient creates an authenticated client with the provided GithubClientConfig
// Otherwise it creates a default client suitable only for accessing public repositories
func CreateGithubClient(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion, githubConfig *GithubClientConfig) (*github.Client, error) {
	if moduleVersion.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient && githubConfig == nil {
		return nil, fmt.Errorf("module '%s' is marked to UseAuthenticatedClient but GithubClientConfig is nil", moduleVersion.Spec.ModuleConfig.Name)
	}

	if moduleVersion.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient && githubConfig != nil {
		authClient, err := GenerateAuthenticatedGithubClient(ctx, githubConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to generate authenticated github client: %v", err)
		}
		return authClient, nil
	}

	return github.NewClient(nil), nil
}

// GetModuleArchiveFromTag gets a module from Github based on its ref and returns a byte slice and updates the
// ModuleVersion with the FileName and Checksum
func GetModuleArchiveFromRef(ctx context.Context, githubClient *github.Client, moduleVersion *versionv1alpha1.ModuleVersion, format github.ArchiveFormat) (moduleBytes []byte, checksum *string, err error) {
	al, alResp, err := githubClient.Repositories.GetArchiveLink(ctx, moduleVersion.Spec.ModuleConfig.RepoOwner, moduleVersion.Spec.Version, format, &github.RepositoryContentGetOptions{
		Ref: moduleVersion.Spec.Version,
	}, true)

	if alResp.StatusCode != 302 {
		return nil, nil, fmt.Errorf("failed to get Github archive link: status code %d: %w", alResp.StatusCode, err)
	}

	moduleReq, err := http.Get(al.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create HTTP request for archive link: %w", err)
	}
	defer moduleReq.Body.Close()

	moduleBytes, err = io.ReadAll(moduleReq.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read module archive data: %w", err)
	}

	sha256Sum := sha256.Sum256(moduleBytes)
	checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
	checksum = &checksumSha256

	return
}

// GenerateGithubClient creates a GitHub client using a GitHub App for authentication.
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

func (t *jwtTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.JWT))
	return t.Transport.RoundTrip(req)
}
