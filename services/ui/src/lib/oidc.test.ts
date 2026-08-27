import {
  createLocalJWKSet,
  exportJWK,
  generateKeyPair,
  SignJWT,
} from "jose";
import { createServer, type Server } from "node:http";
import { beforeAll, describe, expect, it } from "vitest";
import { fetchOIDCEndpoint, validateOIDCEndpoint, verifyIDToken } from "./oidc";

const issuer = "https://issuer.example.com";
const clientId = "opendepot-ui";
const nonce = "expected-nonce";
const keyId = "test-key";

let privateKey: CryptoKey;
let getKey: ReturnType<typeof createLocalJWKSet>;

async function signToken(
  overrides: Record<string, unknown> = {},
  signingKey: CryptoKey = privateKey,
): Promise<string> {
  const now = Math.floor(Date.now() / 1000);

  return new SignJWT({
    nonce,
    iss: issuer,
    sub: "user-123",
    aud: clientId,
    iat: now,
    exp: now + 300,
    ...overrides,
  })
    .setProtectedHeader({ alg: "RS256", kid: keyId })
    .sign(signingKey);
}

describe("verifyIDToken", () => {
  beforeAll(async () => {
    const keyPair = await generateKeyPair("RS256", { extractable: true });
    privateKey = keyPair.privateKey;
    const publicJwk = await exportJWK(keyPair.publicKey);
    getKey = createLocalJWKSet({
      keys: [{ ...publicJwk, alg: "RS256", kid: keyId, use: "sig" }],
    });
  });

  it("accepts a signed token with the expected OIDC claims", async () => {
    const token = await signToken();

    await expect(
      verifyIDToken(token, { issuer, clientId, nonce, jwksUri: issuer }, getKey),
    ).resolves.toMatchObject({ iss: issuer, aud: clientId, nonce });
  });

  it.each([
    ["issuer", { iss: "https://attacker.example.com" }],
    ["audience", { aud: "other-client" }],
    ["nonce", { nonce: "other-nonce" }],
    ["expiry", { exp: Math.floor(Date.now() / 1000) - 1 }],
    ["authorized party", { aud: [clientId, "other-client"], azp: "other-client" }],
    ["missing authorized party", { aud: [clientId, "other-client"] }],
  ])("rejects a token with an invalid %s", async (_name, claims) => {
    const token = await signToken(claims);

    await expect(
      verifyIDToken(token, { issuer, clientId, nonce, jwksUri: issuer }, getKey),
    ).rejects.toThrow();
  });

  it("rejects a token signed by an untrusted key", async () => {
    const untrustedKeyPair = await generateKeyPair("RS256");
    const token = await signToken({}, untrustedKeyPair.privateKey);

    await expect(
      verifyIDToken(token, { issuer, clientId, nonce, jwksUri: issuer }, getKey),
    ).rejects.toThrow();
  });
});

describe("validateOIDCEndpoint", () => {
  it("accepts HTTPS endpoints on the configured issuer origin", () => {
    expect(
      validateOIDCEndpoint("https://issuer.example.com/oauth/token", issuer).href,
    ).toBe("https://issuer.example.com/oauth/token");
  });

  it.each([
    ["loopback", "http://127.0.0.1/token"],
    ["private network", "https://10.0.0.5/token"],
    ["link-local", "http://169.254.169.254/latest/meta-data"],
    ["cross-origin", "https://attacker.example.com/token"],
  ])("rejects a discovered %s endpoint", (_name, endpoint) => {
    expect(() => validateOIDCEndpoint(endpoint, issuer)).toThrow();
  });

  it.each([
    ["non-HTTP scheme", "file:///etc/passwd"],
    ["embedded credentials", "https://user:password@issuer.example.com/token"],
    ["fragment", "https://issuer.example.com/token#fragment"],
    ["HTTP downgrade", "http://issuer.example.com/token"],
  ])("rejects an endpoint with %s", (_name, endpoint) => {
    expect(() => validateOIDCEndpoint(endpoint, issuer)).toThrow();
  });

  it("allows same-origin HTTP only when development mode explicitly enables it", () => {
    expect(
      validateOIDCEndpoint("http://localhost:5556/token", "http://localhost:5556/dex", {
        allowInsecureHTTP: true,
      }).href,
    ).toBe("http://localhost:5556/token");
  });

  it("allows an explicitly configured cross-origin authorization URL", () => {
    expect(
      validateOIDCEndpoint("https://login.example.com/auth", issuer, {
        allowCrossOrigin: true,
      }).href,
    ).toBe("https://login.example.com/auth");
  });
});

describe("fetchOIDCEndpoint", () => {
  function listen(server: Server): Promise<number> {
    return new Promise((resolve) => {
      server.listen(0, "127.0.0.1", () => {
        const address = server.address();
        if (!address || typeof address === "string") {
          throw new Error("test server did not bind to a TCP port");
        }

        resolve(address.port);
      });
    });
  }

  it("does not forward a token POST across a redirect", async () => {
    let destinationRequests = 0;
    const destination = createServer((_request, response) => {
      destinationRequests += 1;
      response.writeHead(200).end();
    });
    const destinationPort = await listen(destination);
    const redirector = createServer((_request, response) => {
      response.writeHead(307, {
        Location: `http://127.0.0.1:${destinationPort}/stolen-token`,
      }).end();
    });
    const redirectorPort = await listen(redirector);

    try {
      await expect(
        fetchOIDCEndpoint(`http://127.0.0.1:${redirectorPort}/token`, {
          method: "POST",
          body: "client_secret=top-secret",
        }),
      ).rejects.toThrow();
      expect(destinationRequests).toBe(0);
    } finally {
      redirector.close();
      destination.close();
    }
  });
});