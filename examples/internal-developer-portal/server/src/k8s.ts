import { CustomObjectsApi, KubeConfig } from "@kubernetes/client-node";
import fs from "node:fs";
import type {
  DepotResource,
  GraphResponse,
  ModuleResource,
  VersionResource,
  GraphDepot,
  GraphModule,
  GraphVersion,
  GraphEdge,
} from "./types";

const GROUP = "kerrareg.io";
const VERSION = "v1alpha1";

function hasInClusterContext(): boolean {
  const host = process.env.KUBERNETES_SERVICE_HOST;
  const port = process.env.KUBERNETES_SERVICE_PORT;
  const tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token";
  const caPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt";
  return Boolean(host && port) && fs.existsSync(tokenPath) && fs.existsSync(caPath);
}

function makeKubeConfig(): KubeConfig {
  const kc = new KubeConfig();
  const mode = (process.env.KERRAREG_K8S_AUTH_MODE || "auto").toLowerCase();

  if (mode === "incluster") {
    if (!hasInClusterContext()) {
      throw new Error("incluster auth requested but serviceaccount context is not available");
    }
    kc.loadFromCluster();
    return kc;
  }

  if (mode === "kubeconfig") {
    const kubeconfigPath = process.env.KUBECONFIG;
    if (kubeconfigPath) {
      kc.loadFromFile(kubeconfigPath);
    } else {
      kc.loadFromDefault();
    }
    return kc;
  }

  if (hasInClusterContext()) {
    kc.loadFromCluster();
    return kc;
  }

  kc.loadFromDefault();
  return kc;
}

function resourceItems<T>(data: unknown): T[] {
  if (typeof data !== "object" || data === null) {
    return [];
  }
  const maybeItems = (data as { items?: T[] }).items;
  return Array.isArray(maybeItems) ? maybeItems : [];
}

function safeName(name?: string): string {
  return name || "unknown";
}

export async function buildKerraregGraph(namespace: string): Promise<GraphResponse> {
  const kc = makeKubeConfig();
  const api = kc.makeApiClient(CustomObjectsApi);

  const [depotsRes, modulesRes, versionsRes] = await Promise.all([
    api.listNamespacedCustomObject({
      group: GROUP,
      version: VERSION,
      namespace,
      plural: "depots",
    }),
    api.listNamespacedCustomObject({
      group: GROUP,
      version: VERSION,
      namespace,
      plural: "modules",
    }),
    api.listNamespacedCustomObject({
      group: GROUP,
      version: VERSION,
      namespace,
      plural: "versions",
    }),
  ]);

  const depots = resourceItems<DepotResource>(depotsRes);
  const modules = resourceItems<ModuleResource>(modulesRes);
  const versions = resourceItems<VersionResource>(versionsRes);

  const graphDepots: GraphDepot[] = [];
  const graphModules: GraphModule[] = [];
  const graphVersions: GraphVersion[] = [];
  const graphEdges: GraphEdge[] = [];

  const moduleByName = new Map<string, ModuleResource>();
  for (const module of modules) {
    const moduleName = module.metadata?.name;
    if (moduleName) {
      moduleByName.set(moduleName, module);
    }
  }

  const versionsByModuleName = new Map<string, VersionResource[]>();
  for (const version of versions) {
    const labels = version.metadata?.labels || {};
    const moduleName = labels["kerrareg.io/module"];
    if (!moduleName) {
      continue;
    }
    const grouped = versionsByModuleName.get(moduleName) || [];
    grouped.push(version);
    versionsByModuleName.set(moduleName, grouped);
  }

  const depotIdByManagedModuleName = new Map<string, string>();

  for (const depot of depots) {
    const depotName = safeName(depot.metadata?.name);
    const depotId = `depot:${depotName}`;
    const managedModuleNames = depot.status?.modules || [];

    graphDepots.push({
      id: depotId,
      name: depotName,
      namespace: depot.metadata?.namespace || namespace,
      pollingIntervalMinutes: depot.spec?.pollingIntervalMinutes,
      managedModuleNames,
      spec: depot.spec,
      status: depot.status,
    });

    for (const moduleName of managedModuleNames) {
      if (!depotIdByManagedModuleName.has(moduleName)) {
        depotIdByManagedModuleName.set(moduleName, depotId);
      }
    }
  }

  for (const module of modules) {
    const moduleName = safeName(module.metadata?.name);
    const moduleId = `module:${moduleName}`;
    const managedByDepotId = depotIdByManagedModuleName.get(moduleName) || "depot:unassigned";

    graphModules.push({
      id: moduleId,
      name: moduleName,
      namespace: module.metadata?.namespace || namespace,
      provider: module.spec?.moduleConfig?.provider,
      repoUrl: module.spec?.moduleConfig?.repoUrl,
      latestVersion: module.status?.latestVersion,
      synced: module.status?.synced,
      syncStatus: module.status?.syncStatus,
      depotId: managedByDepotId,
      spec: module.spec,
      status: module.status,
    });

    if (managedByDepotId !== "depot:unassigned") {
      graphEdges.push({
        id: `${managedByDepotId}->${moduleId}`,
        source: managedByDepotId,
        target: moduleId,
        type: "depot-module",
      });
    }

    const moduleVersions = versionsByModuleName.get(moduleName) || [];
    for (const version of moduleVersions) {
      const versionName = safeName(version.metadata?.name);
      const versionId = `version:${versionName}`;

      graphVersions.push({
        id: versionId,
        name: versionName,
        version: version.spec?.version,
        synced: version.status?.synced,
        syncStatus: version.status?.syncStatus,
        checksum: version.status?.checksum,
        moduleId,
        spec: version.spec,
        status: version.status,
      });

      graphEdges.push({
        id: `${moduleId}->${versionId}`,
        source: moduleId,
        target: versionId,
        type: "module-version",
      });
    }
  }

  const uniqueVersions = Array.from(new Map(graphVersions.map((v) => [v.id, v])).values());
  const uniqueEdges = Array.from(new Map(graphEdges.map((e) => [e.id, e])).values());

  return {
    namespace,
    generatedAt: new Date().toISOString(),
    depots: graphDepots,
    modules: graphModules,
    versions: uniqueVersions,
    edges: uniqueEdges,
    summary: {
      depotCount: graphDepots.length,
      moduleCount: graphModules.length,
      versionCount: uniqueVersions.length,
      syncedModules: graphModules.filter((m) => m.synced).length,
      syncedVersions: uniqueVersions.filter((v) => v.synced).length,
    },
  };
}
