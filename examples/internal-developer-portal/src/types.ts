export type GithubClientConfig = {
  useAuthenticatedClient?: boolean;
  [key: string]: unknown;
};

export type StorageConfig = {
  fileSystem?: {
    directoryPath?: string;
    [key: string]: unknown;
  };
  [key: string]: unknown;
};

export type ModuleConfigSpec = {
  name?: string;
  provider?: string;
  repoOwner?: string;
  repoUrl?: string;
  immutable?: boolean;
  fileFormat?: string;
  versionConstraints?: string;
  githubClientConfig?: GithubClientConfig;
  storageConfig?: StorageConfig;
  [key: string]: unknown;
};

export type ModuleVersionRef = {
  version?: string;
  name?: string;
  fileName?: string;
  synced?: boolean;
  [key: string]: unknown;
};

export type DepotSpec = {
  pollingIntervalMinutes?: number;
  global?: {
    githubClientConfig?: GithubClientConfig;
    storageConfig?: StorageConfig;
    [key: string]: unknown;
  };
  moduleConfigs?: ModuleConfigSpec[];
  [key: string]: unknown;
};

export type DepotStatus = {
  modules?: string[];
  [key: string]: unknown;
};

export type ModuleSpec = {
  moduleConfig?: ModuleConfigSpec;
  versions?: ModuleVersionRef[];
  [key: string]: unknown;
};

export type ModuleStatus = {
  latestVersion?: string;
  synced?: boolean;
  syncStatus?: string;
  moduleVersionRefs?: Record<string, { name?: string; fileName?: string; synced?: boolean; [key: string]: unknown }>;
  [key: string]: unknown;
};

export type VersionSpec = {
  version?: string;
  fileName?: string;
  type?: string;
  moduleConfigRef?: ModuleConfigSpec;
  [key: string]: unknown;
};

export type VersionStatus = {
  synced?: boolean;
  syncStatus?: string;
  checksum?: string;
  [key: string]: unknown;
};

export type GraphDepot = {
  id: string;
  name: string;
  namespace: string;
  pollingIntervalMinutes?: number;
  managedModuleNames: string[];
  spec?: DepotSpec;
  status?: DepotStatus;
};

export type GraphModule = {
  id: string;
  name: string;
  namespace: string;
  provider?: string;
  repoUrl?: string;
  latestVersion?: string;
  synced?: boolean;
  syncStatus?: string;
  depotId: string;
  spec?: ModuleSpec;
  status?: ModuleStatus;
};

export type GraphVersion = {
  id: string;
  name: string;
  version?: string;
  synced?: boolean;
  syncStatus?: string;
  checksum?: string;
  moduleId: string;
  spec?: VersionSpec;
  status?: VersionStatus;
};

export type GraphEdge = {
  id: string;
  source: string;
  target: string;
  type: "depot-module" | "module-version";
};

export type GraphResponse = {
  namespace: string;
  generatedAt: string;
  depots: GraphDepot[];
  modules: GraphModule[];
  versions: GraphVersion[];
  edges: GraphEdge[];
  summary: {
    depotCount: number;
    moduleCount: number;
    versionCount: number;
    syncedModules: number;
    syncedVersions: number;
  };
};

export type NodeKind = "depot" | "module" | "version";

export type SelectedNode =
  | { kind: "depot"; item: GraphDepot }
  | { kind: "module"; item: GraphModule }
  | { kind: "version"; item: GraphVersion }
  | null;
