import { type NextRequest, NextResponse } from "next/server";
import crypto from "crypto";
import { fetchOIDCEndpoint, validateOIDCEndpoint } from "@/lib/oidc";

// PKCE + state + nonce authorization redirect.
export async function GET(_req: NextRequest): Promise<NextResponse> {
  const issuer = process.env.OIDC_ISSUER_URL;
  const clientId = process.env.OIDC_CLIENT_ID;
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL;
  const callbackPath = process.env.OIDC_CALLBACK_PATH ?? "/auth/callback";
  const scopes = process.env.OIDC_SCOPES ?? "openid profile email groups";

  if (!issuer || !clientId || !baseUrl) {
    return new NextResponse("OIDC is not configured", { status: 503 });
  }

  const allowInsecureHTTP = process.env.OIDC_ALLOW_INSECURE_HTTP === "true";
  try {
    validateOIDCEndpoint(issuer, issuer, { allowInsecureHTTP });
  } catch {
    return new NextResponse("Invalid OIDC issuer URL", { status: 503 });
  }

  // Fetch OIDC discovery document to get the authorization endpoint.
  let discoveryRes: Response;
  try {
    discoveryRes = await fetchOIDCEndpoint(`${issuer}/.well-known/openid-configuration`);
  } catch {
    return new NextResponse("OIDC provider is unreachable", { status: 503 });
  }
  if (!discoveryRes.ok) {
    return new NextResponse("Failed to fetch OIDC discovery document", {
      status: 502,
    });
  }
  const discovery = (await discoveryRes.json()) as {
    authorization_endpoint: string;
  };

  try {
    validateOIDCEndpoint(discovery.authorization_endpoint, issuer, { allowInsecureHTTP });
  } catch {
    return new NextResponse("Invalid OIDC discovery endpoint", { status: 502 });
  }

  // Generate PKCE code verifier + challenge.
  const codeVerifier = crypto.randomBytes(32).toString("base64url");
  const codeChallenge = crypto
    .createHash("sha256")
    .update(codeVerifier)
    .digest("base64url");

  const state = crypto.randomBytes(16).toString("hex");
  const nonce = crypto.randomBytes(16).toString("hex");

  const redirectUri = `${baseUrl}${callbackPath}`;
  // OIDC_AUTHZ_URL overrides the authorization endpoint from discovery.
  // Use this in local Kind environments where the issuerUrl is an in-cluster
  // address that is unreachable from browsers (e.g. port-forward dev setups).
  const authzUrlOverride = process.env.OIDC_AUTHZ_URL;
  let authUrl: URL;
  try {
    authUrl = validateOIDCEndpoint(authzUrlOverride ?? discovery.authorization_endpoint, issuer, {
      allowCrossOrigin: authzUrlOverride !== undefined,
      allowInsecureHTTP,
    });
  } catch {
    return new NextResponse("Invalid OIDC authorization endpoint", { status: 502 });
  }
  authUrl.searchParams.set("client_id", clientId);
  authUrl.searchParams.set("redirect_uri", redirectUri);
  authUrl.searchParams.set("response_type", "code");
  authUrl.searchParams.set("scope", scopes);
  authUrl.searchParams.set("state", state);
  authUrl.searchParams.set("nonce", nonce);
  authUrl.searchParams.set("code_challenge", codeChallenge);
  authUrl.searchParams.set("code_challenge_method", "S256");

  // Store state, nonce, and verifier in short-lived SameSite=Lax cookies.
  const response = NextResponse.redirect(authUrl.toString());
  const cookieOpts = {
    httpOnly: true,
    secure: (baseUrl ?? "").startsWith("https://"),
    sameSite: "lax" as const,
    maxAge: 300,
    path: "/",
  };
  response.cookies.set("oidc_state", state, cookieOpts);
  response.cookies.set("oidc_nonce", nonce, cookieOpts);
  response.cookies.set("oidc_cv", codeVerifier, cookieOpts);

  return response;
}
