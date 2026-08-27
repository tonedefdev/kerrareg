import { type NextRequest, NextResponse } from "next/server";
import { getIronSession } from "iron-session";
import { fetchOIDCEndpoint, validateOIDCEndpoint, verifyIDToken } from "@/lib/oidc";
import type { SessionData } from "@/lib/session";
import { sessionOptions } from "@/lib/session";

export async function GET(req: NextRequest): Promise<NextResponse> {
  const issuer = process.env.OIDC_ISSUER_URL;
  const clientId = process.env.OIDC_CLIENT_ID;
  const clientSecret = process.env.OIDC_CLIENT_SECRET;
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL;
  const callbackPath = process.env.OIDC_CALLBACK_PATH ?? "/auth/callback";

  if (!issuer || !clientId || !baseUrl) {
    return new NextResponse("OIDC is not configured", { status: 503 });
  }

  const allowInsecureHTTP = process.env.OIDC_ALLOW_INSECURE_HTTP === "true";
  try {
    validateOIDCEndpoint(issuer, issuer, { allowInsecureHTTP });
  } catch {
    return new NextResponse("Invalid OIDC issuer URL", { status: 503 });
  }

  const searchParams = req.nextUrl.searchParams;
  const code = searchParams.get("code");
  const state = searchParams.get("state");
  const errorParam = searchParams.get("error");

  if (errorParam) {
    const desc = searchParams.get("error_description") ?? errorParam;
    return NextResponse.redirect(new URL(`/?auth_error=${encodeURIComponent(desc)}`, baseUrl));
  }

  if (!code || !state) {
    return new NextResponse("Missing code or state", { status: 400 });
  }

  const storedState = req.cookies.get("oidc_state")?.value;
  const storedNonce = req.cookies.get("oidc_nonce")?.value;
  const codeVerifier = req.cookies.get("oidc_cv")?.value;

  if (!storedState || state !== storedState) {
    return new NextResponse("State mismatch — possible CSRF", { status: 400 });
  }
  if (!storedNonce || !codeVerifier) {
    return new NextResponse("Missing nonce or PKCE verifier", { status: 400 });
  }

  // Fetch OIDC discovery document.
  let discoveryRes: Response;
  try {
    discoveryRes = await fetchOIDCEndpoint(`${issuer}/.well-known/openid-configuration`);
  } catch {
    return new NextResponse("OIDC provider is unreachable", { status: 502 });
  }
  if (!discoveryRes.ok) {
    return new NextResponse("Failed to fetch OIDC discovery document", { status: 502 });
  }
  const discovery = (await discoveryRes.json()) as {
    issuer: string;
    token_endpoint: string;
    jwks_uri: string;
  };

  if (
    discovery.issuer !== issuer ||
    !discovery.token_endpoint ||
    !discovery.jwks_uri
  ) {
    return new NextResponse("Invalid OIDC discovery document", { status: 502 });
  }

  try {
    validateOIDCEndpoint(discovery.token_endpoint, issuer, { allowInsecureHTTP });
    validateOIDCEndpoint(discovery.jwks_uri, issuer, { allowInsecureHTTP });
  } catch {
    return new NextResponse("Invalid OIDC discovery endpoints", { status: 502 });
  }

  // Exchange authorization code for token set.
  const tokenBody = new URLSearchParams({
    grant_type: "authorization_code",
    code,
    redirect_uri: `${baseUrl}${callbackPath}`,
    client_id: clientId,
    code_verifier: codeVerifier,
    ...(clientSecret ? { client_secret: clientSecret } : {}),
  });

  let tokenRes: Response;
  try {
    tokenRes = await fetchOIDCEndpoint(discovery.token_endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: tokenBody.toString(),
    });
  } catch {
    return new NextResponse("Token exchange failed", { status: 502 });
  }

  if (!tokenRes.ok) {
    return new NextResponse("Token exchange failed", { status: 502 });
  }

  const tokenSet = (await tokenRes.json()) as {
    id_token?: string;
    access_token?: string;
    expires_in?: number;
  };

  if (!tokenSet.id_token) {
    return new NextResponse("No id_token in response", { status: 502 });
  }

  try {
    await verifyIDToken(tokenSet.id_token, {
      issuer,
      clientId,
      nonce: storedNonce,
      jwksUri: discovery.jwks_uri,
    });
  } catch {
    return new NextResponse("Invalid ID token", { status: 400 });
  }

  // Store the token set in the session cookie.
  const response = NextResponse.redirect(new URL("/", baseUrl));
  const session = await getIronSession<SessionData>(req, response, sessionOptions);
  session.idToken = tokenSet.id_token;
  session.accessToken = tokenSet.access_token;
  if (tokenSet.expires_in) {
    session.expiresAt = new Date(Date.now() + tokenSet.expires_in * 1000).toISOString();
  }
  await session.save();

  // Clear PKCE/state cookies via headers.append to avoid ResponseCookies
  // overwriting the iron-session cookie that was set via headers.append above.
  for (const name of ["oidc_state", "oidc_nonce", "oidc_cv"]) {
    response.headers.append(
      "set-cookie",
      `${name}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax`,
    );
  }

  return response;
}
