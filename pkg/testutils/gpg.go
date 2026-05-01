/*
Copyright 2026 Anthony Owens.

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

package testutils

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GenerateTestGPGKeyPair generates an ephemeral GPG key pair in the provided
// gpgHome directory and returns the key ID, ASCII-armored public key, and
// base64-encoded ASCII-armored private key. All GPG operations are isolated to
// gpgHome via GNUPGHOME so they do not affect the user's default keyring.
func GenerateTestGPGKeyPair(gpgHome string) (keyID, asciiArmor, privateKeyBase64 string, err error) {
	runGPG := func(args ...string) (string, error) {
		cmd := exec.Command("gpg", args...)
		cmd.Env = append(os.Environ(), fmt.Sprintf("GNUPGHOME=%s", gpgHome))
		out, e := cmd.CombinedOutput()
		return string(out), e
	}

	batchInput := "%no-protection\nKey-Type: RSA\nKey-Length: 2048\nName-Real: Kerrareg E2E Test\nName-Email: test@kerrareg.test\nExpire-Date: 0\n%commit\n"
	batchFile := filepath.Join(gpgHome, "keybatch")
	if err = os.WriteFile(batchFile, []byte(batchInput), 0600); err != nil {
		return "", "", "", fmt.Errorf("failed to write gpg batch file: %w", err)
	}

	if _, err = runGPG("--batch", "--gen-key", batchFile); err != nil {
		return "", "", "", fmt.Errorf("failed to generate gpg key: %w", err)
	}

	// Parse the key fingerprint from colon-delimited output.
	listOut, err := runGPG("--list-keys", "--keyid-format", "long", "--with-colons")
	if err != nil {
		return "", "", "", fmt.Errorf("failed to list gpg keys: %w", err)
	}
	for _, line := range strings.Split(listOut, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 10 && fields[0] == "fpr" && fields[9] != "" {
			fpr := fields[9]
			if len(fpr) >= 16 {
				keyID = fpr[len(fpr)-16:]
			}
			break
		}
	}
	if keyID == "" {
		return "", "", "", fmt.Errorf("could not parse gpg key ID from output: %s", listOut)
	}

	// Export ASCII-armored public key.
	pubOut, err := runGPG("--armor", "--export", keyID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to export gpg public key: %w", err)
	}
	asciiArmor = pubOut

	// Export ASCII-armored private key and base64-encode it.
	privOut, err := runGPG("--armor", "--export-secret-keys", keyID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to export gpg private key: %w", err)
	}
	privateKeyBase64 = base64.StdEncoding.EncodeToString([]byte(privOut))

	return keyID, asciiArmor, privateKeyBase64, nil
}
