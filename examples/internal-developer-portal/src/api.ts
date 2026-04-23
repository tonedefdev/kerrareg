import type { GraphResponse } from "./types";

function getApiBaseUrl(): string {
  const configured = import.meta.env.VITE_API_BASE_URL as string | undefined;
  if (configured && configured.trim().length > 0) {
    return configured.replace(/\/$/, "");
  }

  // In local dev, fall back directly to the API server to avoid proxy-related 404s.
  if (import.meta.env.DEV) {
    return "http://localhost:8082";
  }

  return "";
}

export async function fetchGraph(namespace: string): Promise<GraphResponse> {
  const baseUrl = getApiBaseUrl();
  const response = await fetch(`${baseUrl}/api/graph?namespace=${encodeURIComponent(namespace)}`);
  if (!response.ok) {
    const body = await response.json().catch(() => ({ message: "unknown error" }));
    throw new Error(body.message || `Request failed with status ${response.status}`);
  }
  return response.json();
}
