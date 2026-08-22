/**
 * Server-side API client for the OpenDepot browse endpoints.
 *
 * Called from Next.js Server Components and Route Handlers.  The base URL
 * points to the OpenDepot server service and is never exposed to the browser.
 */

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (
  process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`
).replace(/\/$/, "");

export interface BrowseScanCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
}

export interface BrowseResource {
  kind: string;
  namespace: string;
  name: string;
  latestVersion: string;
  syncStatus: string;
  synced: boolean;
  hasUnsyncedVersions?: boolean;
  public: boolean;
  provider: string;
  repoUrl: string;
  providerNamespace: string;
  upstreamRegistry: string;
  platforms: Array<{ os: string; arch: string }>;
  scanCounts: BrowseScanCounts | null;
  lastScanned: string;
  totalDownloads?: number;
  lastDownloadedAt?: string;
}

export interface BrowseResourceList {
  items: BrowseResource[];
  totalCount: number;
  page: number;
  pageSize: number;
}

export interface BrowseNamespace {
  name: string;
  public: boolean;
}

export interface BrowseNamespaceList {
  items: BrowseNamespace[];
}

export interface BrowseVersionSummary {
  name?: string;
  version: string;
  syncStatus: string;
  os: string;
  arch: string;
  lastScanned: string;
  synced: boolean;
  scanCounts: BrowseScanCounts | null;
  fileName?: string;
  checksum?: string;
  downloadCount?: number;
  lastDownloadedAt?: string;
  archiveSizeBytes?: number;
}

export interface SecurityFinding {
  id: string;
  severity: string;
  title: string;
  message: string;
  resolution: string;
  vulnerabilityID?: string;
  pkgName?: string;
  installedVersion?: string;
  fixedVersion?: string;
  fileName?: string;
  checksum?: string;
}

export interface BrowseScanFindings {
  sourceScanFindings?: SecurityFinding[];
  binaryScanFindings?: Record<string, SecurityFinding[]>;
  selectedVersion?: string;
  scannedVersions?: string[];
  binaryVersions?: string[];
}

export interface BrowseStorageConfig {
  backend: string;
  bucket?: string;
  region?: string;
  key?: string;
  directoryPath?: string;
  accountName?: string;
  accountUrl?: string;
  subscriptionID?: string;
  resourceGroup?: string;
  presignEnabled?: boolean;
  presignTTL?: string;
}

export interface BrowseGithubConfig {
  useAuthenticatedClient: boolean;
}

export interface BrowseDepotRef {
  namespace: string;
  name: string;
}

export interface BrowseResourceDetail extends BrowseResource {
  versions: BrowseVersionSummary[];
  sourceScanFindings: SecurityFinding[];
  binaryScanFindings: Record<string, SecurityFinding[]>;
  storageConfig?: BrowseStorageConfig;
  githubConfig?: BrowseGithubConfig;
  depotRef?: BrowseDepotRef;
  repoOwner?: string;
  versionHistoryLimit?: number;
  versionConstraints?: string;
  sourceRepository?: string;
  readmeContent?: string;
}

export interface BrowseDepot {
  namespace: string;
  name: string;
  modules: string[];
  providers: string[];
  pollingIntervalMinutes?: number;
  storageBackend: string;
}

export interface BrowseDepotList {
  items: BrowseDepot[];
}

export interface ListResourcesParams {
  namespace?: string;
  kind?: string;
  q?: string;
  synced?: boolean;
  os?: string;
  arch?: string;
  severity?: string;
  publicOnly?: boolean;
  sortBy?: string;
  sortDir?: "asc" | "desc";
  page?: number;
  pageSize?: number;
}

async function apiFetch<T>(
  path: string,
  token?: string,
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE_URL}${path}`, { headers, cache: "no-store" });
  if (!res.ok) {
    throw new Error(`API request failed: ${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<T>;
}

export async function listNamespaces(token?: string): Promise<BrowseNamespaceList> {
  return apiFetch<BrowseNamespaceList>("/opendepot/ui/v1/namespaces", token);
}

export async function listResources(
  params: ListResourcesParams,
  token?: string,
): Promise<BrowseResourceList> {
  const qs = new URLSearchParams();
  if (params.namespace) qs.set("namespace", params.namespace);
  if (params.kind) qs.set("kind", params.kind);
  if (params.q) qs.set("q", params.q);
  if (params.synced !== undefined) qs.set("synced", String(params.synced));
  if (params.os) qs.set("os", params.os);
  if (params.arch) qs.set("arch", params.arch);
  if (params.severity) qs.set("severity", params.severity);
  if (params.publicOnly !== undefined) qs.set("public_only", String(params.publicOnly));
  if (params.sortBy) qs.set("sort_by", params.sortBy);
  if (params.sortDir) qs.set("sort_dir", params.sortDir);
  if (params.page !== undefined) qs.set("page", String(params.page));
  if (params.pageSize !== undefined) qs.set("page_size", String(params.pageSize));

  const query = qs.toString();
  return apiFetch<BrowseResourceList>(
    `/opendepot/ui/v1/resources${query ? `?${query}` : ""}`,
    token,
  );
}

export async function getResourceDetail(
  namespace: string,
  kind: string,
  name: string,
  token?: string,
): Promise<BrowseResourceDetail> {
  return apiFetch<BrowseResourceDetail>(
    `/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
    token,
  );
}

export async function listDepots(token?: string): Promise<BrowseDepotList> {
  return apiFetch<BrowseDepotList>("/opendepot/ui/v1/depots", token);
}

// ── Graph types ────────────────────────────────────────────────────────────

export interface BrowseGraphDepot {
  id: string;
  namespace: string;
  name: string;
  storageBackend?: string;
  pollingIntervalMinutes?: number;
  managedModuleNames?: string[];
  managedProviderNames?: string[];
}

export interface BrowseGraphModule {
  id: string;
  namespace: string;
  name: string;
  provider?: string;
  synced: boolean;
  syncStatus?: string;
  repoURL?: string;
  latestVersion?: string;
  depotID?: string;
  scanCounts?: BrowseScanCounts;
}

export interface BrowseGraphProvider {
  id: string;
  namespace: string;
  name: string;
  providerNamespace?: string;
  upstreamRegistry?: string;
  synced: boolean;
}

export interface BrowseGraphEdge {
  id: string;
  source: string;
  target: string;
}

export interface BrowseGraphSummary {
  totalDepots: number;
  totalModules: number;
  totalProviders: number;
}

export interface BrowseDepotGraph {
  depots: BrowseGraphDepot[];
  modules: BrowseGraphModule[];
  providers: BrowseGraphProvider[];
  edges: BrowseGraphEdge[];
  summary: BrowseGraphSummary;
  generatedAt: string;
}

export async function getDepotsGraph(namespace?: string, token?: string): Promise<BrowseDepotGraph> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<BrowseDepotGraph>(`/opendepot/ui/v1/depots/graph${params}`, token);
}

// ── Stats types ────────────────────────────────────────────────────────────

export interface SyncHealthStats {
  syncedVersions: number;
  unsyncedVersions: number;
  failedVersions: number;
}

export interface SecurityPostureStats {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
  totalAffectedResources: number;
}

export interface StorageBackendStat {
  backend: string;
  count: number;
}

export interface PopularResource {
  namespace: string;
  kind: string;
  name: string;
  version: string;
  downloadCount: number;
  lastDownloadedAt?: string;
}

export interface BrowseStats {
  totalModules: number;
  totalProviders: number;
  totalVersions: number;
  totalStorageBytes: number;
  totalDownloads: number;
  syncHealth: SyncHealthStats;
  securityPosture: SecurityPostureStats;
  storageDistribution: StorageBackendStat[];
  mostDownloaded: PopularResource[];
}

export async function getStats(namespace?: string, token?: string): Promise<BrowseStats> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<BrowseStats>(`/opendepot/ui/v1/stats${params}`, token);
}
