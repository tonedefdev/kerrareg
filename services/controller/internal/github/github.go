package github

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/go-github/v50/github"
	"golang.org/x/oauth2"
)

// jwtTransport is a custom HTTP transport that adds the JWT to the Authorization header
type jwtTransport struct {
	Transport http.RoundTripper
	JWT       string
}

type GithubClientConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPath string
}

// GenerateGitHubClient creates a GitHub client using a GitHub App for authentication.
func GenerateGitHubClient(ctx context.Context, githubClientConfig *GithubClientConfig) (*github.Client, error) {
	// Read the private key file
	keyData, err := os.ReadFile(githubClientConfig.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	// Parse the private key
	block, _ := pem.Decode(keyData)
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
