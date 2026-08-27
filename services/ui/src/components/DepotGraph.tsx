"use client";

import * as React from "react";
import { useCallback, useMemo, useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  Handle,
  MiniMap,
  type Edge,
  type Node,
  type NodeMouseHandler,
  MarkerType,
  Position,
} from "reactflow";
import "reactflow/dist/style.css";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import type {
  BrowseDepotGraph,
  BrowseGraphDepot,
  BrowseGraphModule,
  BrowseGraphProvider,
  BrowseStorageConfig,
} from "@/lib/api";
import DepotNodePanel from "./DepotNodePanel";

// ── Brand palette ─────────────────────────────────────────────────────────
const DEPOT_BORDER = "#04cfd0";
const DEPOT_BG = "rgba(4,207,208,0.10)";
const MODULE_BORDER = "#03deb8";
const MODULE_BG = "rgba(3,222,184,0.08)";
const PROVIDER_BORDER = "#047df1";
const PROVIDER_BG = "rgba(4,125,241,0.08)";
const VERSION_BORDER = "#8b949e";
const VERSION_BG = "rgba(139,148,158,0.12)";

// ── Layout constants ───────────────────────────────────────────────────────
const LANE_X = { depot: 40, resource: 420, version: 780 };
const DEPOT_GAP = 140;
const RESOURCE_GAP = 86;
const VERSION_GAP = 80;
const NODE_WIDTH = 200;
const NODE_HEIGHT = 64;
// Number of version nodes shown per resource before collapsing the rest.
const VERSION_PAGE_SIZE = 3;

// ── Node types ─────────────────────────────────────────────────────────────
type NodeKind = "depot" | "module" | "provider" | "version" | "versionOverflow";

interface NodeData {
  kind: NodeKind;
  namespace: string;
  name: string;
  displayName?: string;
  label: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  upstreamRegistry?: string;
  storageBackend?: string;
  storageConfig?: BrowseStorageConfig;
  githubAuthenticated?: boolean;
  latestVersion?: string;
  fileName?: string;
  checksum?: string;
  lastScanned?: string;
  // versionOverflow nodes only:
  resourceId?: string;
  isExpanded?: boolean;
  hiddenCount?: number;
}

function isEffectivelySynced(synced?: boolean, syncStatus?: string): boolean {
  if (synced) {
    return true;
  }
  return /successfully\s+synced/i.test(syncStatus ?? "");
}

function compareVersionDesc(a: string, b: string): number {
  const normalize = (v: string) => v.replace(/^v/i, "");
  const tokenize = (v: string) =>
    normalize(v)
      .split(/[.+-]/)
      .map((part) => {
        const numeric = Number(part);
        return Number.isFinite(numeric) && part !== "" ? numeric : part.toLowerCase();
      });

  const aTokens = tokenize(a);
  const bTokens = tokenize(b);
  const length = Math.max(aTokens.length, bTokens.length);

  for (let i = 0; i < length; i += 1) {
    const left = aTokens[i];
    const right = bTokens[i];

    if (left === undefined) return 1;
    if (right === undefined) return -1;
    if (left === right) continue;

    if (typeof left === "number" && typeof right === "number") {
      return right - left;
    }

    if (typeof left === "number") return -1;
    if (typeof right === "number") return 1;

    return String(right).localeCompare(String(left));
  }

  return 0;
}

function nodeStyle(kind: NodeKind): React.CSSProperties {
  const map: Record<NodeKind, { border: string; bg: string }> = {
    depot: { border: DEPOT_BORDER, bg: DEPOT_BG },
    module: { border: MODULE_BORDER, bg: MODULE_BG },
    provider: { border: PROVIDER_BORDER, bg: PROVIDER_BG },
    version: { border: VERSION_BORDER, bg: VERSION_BG },
    versionOverflow: { border: VERSION_BORDER, bg: VERSION_BG },
  };
  const { border, bg } = map[kind];
  return {
    background: bg,
    border: `1px solid ${border}`,
    borderRadius: 8,
    padding: "10px 14px",
    minWidth: NODE_WIDTH,
    maxWidth: NODE_WIDTH,
    minHeight: NODE_HEIGHT,
    color: "var(--mui-palette-text-primary)",
    fontFamily: "inherit",
    fontSize: 13,
    display: "flex",
    flexDirection: "column" as const,
    justifyContent: "center",
    gap: 4,
    boxSizing: "border-box" as const,
  };
}

function VersionFlowNode({ data }: { data: NodeData }) {
  const primaryText = data.displayName ?? data.latestVersion ?? "No version";
  const secondaryText = data.displayName ? (data.latestVersion ?? data.name) : data.name;

  return (
    <div style={nodeStyle("version")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <span style={{ fontWeight: 600, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {primaryText}
      </span>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {secondaryText}
      </span>
    </div>
  );
}

function DepotFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("depot")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <span style={{ fontWeight: 700, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.storageBackend ? ` · ${data.storageBackend}` : ""}
      </span>
    </div>
  );
}

function ModuleFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("module")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <span style={{ fontWeight: 600, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.provider ? ` · ${data.provider}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

function ProviderFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("provider")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <span style={{ fontWeight: 600, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.providerNamespace ? `/${data.providerNamespace}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

function VersionOverflowFlowNode({ data }: { data: NodeData }) {
  const overflowStyle: React.CSSProperties = {
    ...nodeStyle("version"),
    background: "rgba(139,148,158,0.06)",
    border: "1px dashed #8b949e",
    cursor: "pointer",
    alignItems: "center",
    flexDirection: "row",
    gap: 8,
    justifyContent: "center",
  };
  return (
    <div style={overflowStyle}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      {data.isExpanded ? (
        <Tooltip title="collapse" placement="top">
          <span style={{ fontSize: 18, lineHeight: 1, fontWeight: 600 }}>−</span>
        </Tooltip>
      ) : (
        <>
          <span style={{ fontSize: 18, lineHeight: 1, fontWeight: 600 }}>+</span>
          <span style={{ fontWeight: 500, fontSize: 12, color: "var(--mui-palette-text-secondary)" }}>
            {`${data.hiddenCount} more versions`}
          </span>
        </>
      )}
    </div>
  );
}

const nodeTypes = {
  depotNode: DepotFlowNode,
  moduleNode: ModuleFlowNode,
  providerNode: ProviderFlowNode,
  versionNode: VersionFlowNode,
  versionOverflowNode: VersionOverflowFlowNode,
};

// ── Layout builder ─────────────────────────────────────────────────────────
function buildGraph(
  graph: BrowseDepotGraph,
  moduleVersionsByKey: Record<string, string[]>,
  providerVersionsByKey: Record<string, string[]>,
  providerVersionMetaByKey: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>,
  moduleVersionMetaByKey: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>,
  moduleDetailByKey: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>,
  expandedResources: Set<string>,
): { nodes: Node<NodeData>[]; edges: Edge[] } {
  const nodes: Node<NodeData>[] = [];
  const edges: Edge[] = [];

  // Position depot nodes
  graph.depots.forEach((d: BrowseGraphDepot, i: number) => {
    nodes.push({
      id: d.id,
      type: "depotNode",
      position: { x: LANE_X.depot, y: i * DEPOT_GAP },
      data: {
        kind: "depot",
        namespace: d.namespace,
        name: d.name,
        label: d.name,
        storageBackend: d.storageBackend,
      },
    });
  });

  // Position module/provider nodes with enough vertical space for module version fanout.
  const resourceNodes: Array<{ id: string; type: string; data: NodeData; stride: number }> = [];
  graph.modules.forEach((m: BrowseGraphModule) => {
    const effectiveSynced = isEffectivelySynced(m.synced, m.syncStatus);
    const key = `${m.namespace}/${m.name}`;
    const moduleDetail = moduleDetailByKey[key];
    const moduleVersions = moduleVersionsByKey[key] ?? (m.latestVersion ? [m.latestVersion] : []);
    const uniqueVersions = Array.from(new Set(moduleVersions)).filter(Boolean);
    const isExpanded = expandedResources.has(m.id);
    const visibleCount =
      uniqueVersions.length > VERSION_PAGE_SIZE && !isExpanded
        ? VERSION_PAGE_SIZE
        : uniqueVersions.length;
    // +1 for overflow node when we need to truncate
    const slotCount = uniqueVersions.length > VERSION_PAGE_SIZE ? visibleCount + 1 : visibleCount;
    const versionStackHeight = NODE_HEIGHT + Math.max(0, slotCount - 1) * VERSION_GAP;
    const stride = Math.max(RESOURCE_GAP, versionStackHeight + 20);
    resourceNodes.push({
      id: m.id,
      type: "moduleNode",
      stride,
      data: {
        kind: "module",
        namespace: m.namespace,
        name: m.name,
        label: m.name,
        synced: effectiveSynced,
        provider: m.provider,
        storageConfig: moduleDetail?.storageConfig,
        githubAuthenticated: moduleDetail?.githubAuthenticated,
        latestVersion: m.latestVersion,
      },
    });
  });
  graph.providers.forEach((p: BrowseGraphProvider) => {
    const key = `${p.namespace}/${p.name}`;
    const providerMeta = providerVersionMetaByKey[key] ?? [];
    const totalVersionCount = providerMeta.length > 0
      ? providerMeta.length
      : Array.from(new Set(providerVersionsByKey[key] ?? [])).filter(Boolean).length;
    const isExpanded = expandedResources.has(p.id);
    const visibleCount =
      totalVersionCount > VERSION_PAGE_SIZE && !isExpanded
        ? VERSION_PAGE_SIZE
        : totalVersionCount;
    const slotCount = totalVersionCount > VERSION_PAGE_SIZE ? visibleCount + 1 : visibleCount;
    const versionStackHeight = NODE_HEIGHT + Math.max(0, slotCount - 1) * VERSION_GAP;
    const stride = Math.max(RESOURCE_GAP, versionStackHeight + 20);
    resourceNodes.push({
      id: p.id,
      type: "providerNode",
      stride,
      data: {
        kind: "provider",
        namespace: p.namespace,
        name: p.name,
        label: p.name,
        synced: p.synced,
        providerNamespace: p.providerNamespace,
        upstreamRegistry: p.upstreamRegistry,
      },
    });
  });

  let currentResourceY = 0;
  const resourceYById = new Map<string, number>();
  resourceNodes.forEach((n) => {
    resourceYById.set(n.id, currentResourceY);
    nodes.push({
      id: n.id,
      type: n.type,
      position: { x: LANE_X.resource, y: currentResourceY },
      data: n.data,
    });
    currentResourceY += n.stride;
  });

  // Add one version node per module version.
  graph.modules.forEach((m) => {
    const key = `${m.namespace}/${m.name}`;
    const versionMeta = moduleVersionMetaByKey[key] ?? [];
    const moduleVersions =
      versionMeta.length > 0
        ? versionMeta.map((entry) => entry.version)
        : moduleVersionsByKey[key] ?? (m.latestVersion ? [m.latestVersion] : []);
    const versions = Array.from(new Set(moduleVersions)).filter(Boolean).sort(compareVersionDesc);
    if (versions.length === 0) {
      return;
    }
    const baseY = resourceYById.get(m.id);
    if (baseY === undefined) {
      return;
    }

    const isExpanded = expandedResources.has(m.id);
    const visibleVersions =
      versions.length > VERSION_PAGE_SIZE && !isExpanded
        ? versions.slice(0, VERSION_PAGE_SIZE)
        : versions;

    visibleVersions.forEach((version, index) => {
      const versionNodeId = `version/${m.namespace}/${m.name}/${version}`;
      const meta = versionMeta.find((entry) => entry.version === version);
      nodes.push({
        id: versionNodeId,
        type: "versionNode",
        position: { x: LANE_X.version, y: baseY + index * VERSION_GAP },
        data: {
          kind: "version",
          namespace: m.namespace,
          name: m.name,
          label: version,
          latestVersion: version,
          fileName: meta?.fileName,
          checksum: meta?.checksum,
          lastScanned: meta?.lastScanned,
        },
      });

      edges.push({
        id: `edge-${m.id}-${versionNodeId}`,
        source: m.id,
        target: versionNodeId,
        markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
        style: { stroke: "#8b949e", strokeWidth: 1.6, strokeDasharray: "4 3" },
        type: "smoothstep",
      });
    });

    if (versions.length > VERSION_PAGE_SIZE) {
      const overflowIndex = visibleVersions.length;
      const overflowNodeId = `versionOverflow/${m.id}`;
      nodes.push({
        id: overflowNodeId,
        type: "versionOverflowNode",
        position: { x: LANE_X.version, y: baseY + overflowIndex * VERSION_GAP },
        data: {
          kind: "versionOverflow",
          namespace: m.namespace,
          name: m.name,
          label: isExpanded ? "collapse" : `+${versions.length - VERSION_PAGE_SIZE} more versions`,
          resourceId: m.id,
          isExpanded,
          hiddenCount: versions.length - VERSION_PAGE_SIZE,
        },
      });
      edges.push({
        id: `edge-${m.id}-${overflowNodeId}`,
        source: m.id,
        target: overflowNodeId,
        style: { stroke: "#8b949e", strokeWidth: 1, strokeDasharray: "3 4", opacity: 0.5 },
        type: "smoothstep",
      });
    }
  });

  // Add one version node per provider version.
  graph.providers.forEach((p) => {
    const key = `${p.namespace}/${p.name}`;
    const providerMeta = providerVersionMetaByKey[key] ?? [];

    const items: Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }> =
      providerMeta.length > 0
        ? [...providerMeta]
        : Array.from(new Set(providerVersionsByKey[key] ?? []))
            .filter(Boolean)
            .map((version) => ({ version }));

    items.sort((a, b) => {
      const versionCompare = compareVersionDesc(a.version, b.version);
      if (versionCompare !== 0) {
        return versionCompare;
      }
      return (a.name ?? "").localeCompare(b.name ?? "");
    });

    if (items.length === 0) {
      return;
    }

    const baseY = resourceYById.get(p.id);
    if (baseY === undefined) {
      return;
    }

    const isExpanded = expandedResources.has(p.id);
    const visibleItems =
      items.length > VERSION_PAGE_SIZE && !isExpanded
        ? items.slice(0, VERSION_PAGE_SIZE)
        : items;

    visibleItems.forEach((item, index) => {
      const version = item.version;
      const versionName = item.name?.trim() ? item.name : p.name;
      const rawIdSuffix = item.name?.trim()
        ? item.name
        : `${version}-${index}`;
      const idSuffix = encodeURIComponent(rawIdSuffix);
      const versionNodeId = `version/${p.namespace}/${p.name}/${idSuffix}`;
      nodes.push({
        id: versionNodeId,
        type: "versionNode",
        position: { x: LANE_X.version, y: baseY + index * VERSION_GAP },
        data: {
          kind: "version",
          namespace: p.namespace,
          name: versionName,
          displayName: item.name,
          label: version,
          latestVersion: version,
          fileName: item.fileName,
          checksum: item.checksum,
          lastScanned: item.lastScanned,
        },
      });

      edges.push({
        id: `edge-${p.id}-${versionNodeId}`,
        source: p.id,
        target: versionNodeId,
        markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
        style: { stroke: "#8b949e", strokeWidth: 1.6, strokeDasharray: "4 3" },
        type: "smoothstep",
      });
    });

    if (items.length > VERSION_PAGE_SIZE) {
      const overflowIndex = visibleItems.length;
      const overflowNodeId = `versionOverflow/${p.id}`;
      nodes.push({
        id: overflowNodeId,
        type: "versionOverflowNode",
        position: { x: LANE_X.version, y: baseY + overflowIndex * VERSION_GAP },
        data: {
          kind: "versionOverflow",
          namespace: p.namespace,
          name: p.name,
          label: isExpanded ? "collapse" : `+${items.length - VERSION_PAGE_SIZE} more versions`,
          resourceId: p.id,
          isExpanded,
          hiddenCount: items.length - VERSION_PAGE_SIZE,
        },
      });
      edges.push({
        id: `edge-${p.id}-${overflowNodeId}`,
        source: p.id,
        target: overflowNodeId,
        style: { stroke: "#8b949e", strokeWidth: 1, strokeDasharray: "3 4", opacity: 0.5 },
        type: "smoothstep",
      });
    }
  });

  // Build edges from graph data
  const relationshipEdges: Edge[] = [];
  graph.edges.forEach((e) => {
    relationshipEdges.push({
      id: e.id,
      source: e.source,
      target: e.target,
      markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 12, height: 12 },
      style: { stroke: "#8b949e", strokeWidth: 1.5 },
      type: "smoothstep",
    });
  });

  // Fallback: when backend edges are empty, infer depot relationships from managed names.
  if (relationshipEdges.length === 0) {
    for (const depot of graph.depots) {
      for (const moduleName of depot.managedModuleNames ?? []) {
        const mod = graph.modules.find((m) => m.namespace === depot.namespace && m.name === moduleName);
        if (!mod) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${mod.id}`,
          source: depot.id,
          target: mod.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }

      for (const providerName of depot.managedProviderNames ?? []) {
        const prov = graph.providers.find((p) => p.namespace === depot.namespace && p.name === providerName);
        if (!prov) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${prov.id}`,
          source: depot.id,
          target: prov.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }
    }
  }

  relationshipEdges.forEach((e) => {
    edges.push({
      ...e,
      animated: true,
    });
  });

  return { nodes, edges };
}

// ── Main component ─────────────────────────────────────────────────────────
interface DepotGraphProps {
  graph: BrowseDepotGraph;
  moduleVersionsByKey?: Record<string, string[]>;
  providerVersionsByKey?: Record<string, string[]>;
  providerVersionMetaByKey?: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
  moduleVersionMetaByKey?: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
  moduleDetailByKey?: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>;
}

type SelectedNode = {
  kind: NodeKind;
  namespace: string;
  name: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  storageBackend?: string;
  storageConfig?: BrowseStorageConfig;
  githubAuthenticated?: boolean;
  latestVersion?: string;
  fileName?: string;
  checksum?: string;
  lastScanned?: string;
} | null;

export default function DepotGraph({ graph, moduleVersionsByKey = {}, providerVersionsByKey = {}, providerVersionMetaByKey = {}, moduleVersionMetaByKey = {}, moduleDetailByKey = {} }: DepotGraphProps) {
  const [selected, setSelected] = useState<SelectedNode>(null);
  const [expandedResources, setExpandedResources] = useState<Set<string>>(new Set());

  const { nodes, edges } = useMemo(
    () => buildGraph(graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey, moduleVersionMetaByKey, moduleDetailByKey, expandedResources),
    [graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey, moduleVersionMetaByKey, moduleDetailByKey, expandedResources],
  );

  const onNodeClick: NodeMouseHandler = useCallback((_event, node) => {
    const data = node.data as NodeData;
    if (data.kind === "versionOverflow" && data.resourceId) {
      setExpandedResources((prev) => {
        const next = new Set(prev);
        if (data.isExpanded) {
          next.delete(data.resourceId!);
        } else {
          next.add(data.resourceId!);
        }
        return next;
      });
      return;
    }
    setSelected({
      kind: data.kind,
      namespace: data.namespace,
      name: data.name,
      synced: data.synced,
      provider: data.provider,
      providerNamespace: data.providerNamespace,
      storageBackend: data.storageBackend,
      storageConfig: data.storageConfig,
      githubAuthenticated: data.githubAuthenticated,
      latestVersion: data.latestVersion,
      fileName: data.fileName,
      checksum: data.checksum,
      lastScanned: data.lastScanned,
    });
  }, []);

  if (graph.depots.length === 0 && graph.modules.length === 0 && graph.providers.length === 0) {
    return (
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "center", height: 400 }}>
        <Typography color="text.secondary">No depot relationships to display.</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", height: "calc(100vh - 290px)", minHeight: 420, gap: 0 }}>
      {/* Graph canvas */}
      <Box sx={{ flex: 1, position: "relative" }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodeClick={onNodeClick}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          minZoom={0.2}
          maxZoom={2}
          proOptions={{ hideAttribution: true }}
          style={{ background: "var(--mui-palette-background-default)" }}
        >
          <Background color="var(--mui-palette-divider)" gap={20} />
          <Controls
            style={{
              background: "var(--mui-palette-background-paper)",
              border: "1px solid var(--mui-palette-divider)",
              borderRadius: 6,
            }}
          />
          <MiniMap
            style={{
              background: "var(--mui-palette-background-paper)",
              border: "1px solid var(--mui-palette-divider)",
              borderRadius: 6,
            }}
            nodeColor={(n) => {
              const d = n.data as NodeData;
              if (d.kind === "depot") return DEPOT_BORDER;
              if (d.kind === "module") return MODULE_BORDER;
              if (d.kind === "provider") return PROVIDER_BORDER;
              return VERSION_BORDER;
            }}
          />
        </ReactFlow>
      </Box>

      {/* Side panel */}
      {selected && (
        <DepotNodePanel
          node={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </Box>
  );
}
