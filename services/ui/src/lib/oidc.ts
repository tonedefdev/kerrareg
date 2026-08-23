interface OIDCEndpointValidationOptions {
  allowCrossOrigin?: boolean;
  allowInsecureHTTP?: boolean;
}

export function fetchOIDCEndpoint(
  input: string | URL,
  init: RequestInit = {},
): Promise<Response> {
  return fetch(input, { ...init, redirect: "error" });
}

export function validateOIDCEndpoint(
  endpoint: string,
  issuer: string,
  options: OIDCEndpointValidationOptions = {},
): URL {
  const endpointURL = new URL(endpoint);
  const issuerURL = new URL(issuer);

  if (endpointURL.protocol !== "https:" && endpointURL.protocol !== "http:") {
    throw new Error("OIDC endpoint must use HTTP or HTTPS");
  }

  if (endpointURL.username || endpointURL.password || endpointURL.hash) {
    throw new Error("OIDC endpoint must not contain credentials or a fragment");
  }

  if (endpointURL.protocol !== "https:" && !options.allowInsecureHTTP) {
    throw new Error("OIDC endpoint must use HTTPS");
  }

  if (!options.allowCrossOrigin && endpointURL.origin !== issuerURL.origin) {
    throw new Error("OIDC endpoint must share the configured issuer origin");
  }

  return endpointURL;
}
import {
  createRemoteJWKSet,
  jwtVerify,
  type JWTPayload,
  type JWTVerifyGetKey,
} from "jose";

export interface IDTokenVerificationOptions {
  issuer: string;
  clientId: string;
  nonce: string;
  jwksUri: string;
}

export async function verifyIDToken(
  token: string,
  options: IDTokenVerificationOptions,
  getKey: JWTVerifyGetKey = createRemoteJWKSet(new URL(options.jwksUri)),
): Promise<JWTPayload> {
  const { payload } = await jwtVerify(token, getKey, {
    issuer: options.issuer,
    audience: options.clientId,
    requiredClaims: ["iss", "sub", "aud", "exp", "iat", "nonce"],
  });

  if (payload.nonce !== options.nonce) {
    throw new Error("ID token nonce does not match the authorization request");
  }

  if (
    (Array.isArray(payload.aud) && payload.aud.length > 1 && !payload.azp) ||
    (payload.azp !== undefined && payload.azp !== options.clientId)
  ) {
    throw new Error("ID token authorized party does not match the client ID");
  }

  return payload;
}