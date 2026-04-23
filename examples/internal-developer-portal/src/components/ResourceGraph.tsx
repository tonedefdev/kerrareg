import { useEffect, useMemo, useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  type Edge,
  type Node,
  type NodeMouseHandler,
  MarkerType,
  Position,
} from "reactflow";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import type { GraphResponse, SelectedNode } from "../types";

const depotColor = "#0057B8";
const moduleColor = "#0A7D45";
const versionColor = "#A15700";
const laneX = {
  depot: 40,
  module: 420,
  version: 820,
};

const layout = {
  topPadding: 44,
  depotGap: 96,
  moduleGap: 28,
  moduleInnerTop: 12,
  versionGap: 58,
  minModuleBlockHeight: 84,
  minDepotBlockHeight: 180,
};

type Props = {
  graph: GraphResponse;
  onSelect: (selected: SelectedNode) => void;
};

type ParsedSemver = {
  major: number;
  minor: number;
  patch: number;
  prerelease: string[];
};

function parseSemver(input: string): ParsedSemver | null {
  const normalized = input.trim().replace(/^v/, "");
  const match = normalized.match(/^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/);

  if (!match) {
    return null;
  }

  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4] ? match[4].split(".") : [],
  };
}

function compareSemverDesc(a: string, b: string): number {
  const parsedA = parseSemver(a);
  const parsedB = parseSemver(b);

  if (!parsedA || !parsedB) {
    return b.localeCompare(a);
  }

  if (parsedA.major !== parsedB.major) {
    return parsedB.major - parsedA.major;
  }
  if (parsedA.minor !== parsedB.minor) {
    return parsedB.minor - parsedA.minor;
  }
  if (parsedA.patch !== parsedB.patch) {
    return parsedB.patch - parsedA.patch;
  }

  const aStable = parsedA.prerelease.length === 0;
  const bStable = parsedB.prerelease.length === 0;
  if (aStable && !bStable) {
    return -1;
  }
  if (!aStable && bStable) {
    return 1;
  }

  for (let i = 0; i < Math.max(parsedA.prerelease.length, parsedB.prerelease.length); i += 1) {
    const idA = parsedA.prerelease[i];
    const idB = parsedB.prerelease[i];

    if (idA === undefined) {
      return -1;
    }
    if (idB === undefined) {
      return 1;
    }

    const numA = Number(idA);
    const numB = Number(idB);
    const isNumA = !Number.isNaN(numA) && /^\d+$/.test(idA);
    const isNumB = !Number.isNaN(numB) && /^\d+$/.test(idB);

    if (isNumA && isNumB && numA !== numB) {
      return numA - numB;
    }

    if (isNumA && !isNumB) {
      return -1;
    }
    if (!isNumA && isNumB) {
      return 1;
    }

    const stringCompare = idA.localeCompare(idB);
    if (stringCompare !== 0) {
      return stringCompare;
    }
  }

  return 0;
}

function versionNodeLabel(value: string, isLatest: boolean) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      <span>{value}</span>
      {isLatest ? (
        <span
          style={{
            display: "inline-flex",
            alignItems: "center",
            border: "1px solid #A15700",
            color: "#7A3E00",
            background: "#FFE8D2",
            borderRadius: 999,
            fontSize: 10,
            fontWeight: 700,
            lineHeight: 1,
            padding: "3px 7px",
            textTransform: "uppercase",
            letterSpacing: 0.3,
          }}
        >
          latest
        </span>
      ) : null}
    </span>
  );
}

function depotNodeLabel(depotName: string) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 0 }}>
      <WarehouseIcon sx={{ fontSize: 18, color: "#0057B8", flexShrink: 0 }} />
      <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{depotName}</span>
    </span>
  );
}

function moduleNodeLabel(
  moduleName: string,
  logo: { src: string; alt: string } | null,
  versionCount: number,
  collapsed: boolean,
  onToggle: () => void
) {
  const canCollapse = versionCount > 3;

  return (
    <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", width: "100%", gap: 8 }}>
      <span style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 0 }}>
        {logo ? <img src={logo.src} alt={logo.alt} width={16} height={16} style={{ display: "block", flexShrink: 0 }} /> : null}
        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{moduleName}</span>
      </span>
      {canCollapse ? (
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation();
            onToggle();
          }}
          style={{
            border: "1px solid #0A7D45",
            background: "#F0FBF6",
            color: "#0A5B34",
            borderRadius: 999,
            width: 20,
            height: 20,
            minWidth: 20,
            minHeight: 20,
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 13,
            fontWeight: 700,
            lineHeight: 1,
            padding: 0,
            cursor: "pointer",
          }}
          aria-label={collapsed ? "Expand versions" : "Collapse versions"}
          title={collapsed ? "Expand versions" : "Collapse versions"}
        >
          {collapsed ? "+" : "-"}
        </button>
      ) : null}
    </span>
  );
}

function providerLogo(provider?: string) {
  const normalized = provider?.toLowerCase();

  if (normalized === "aws") {
    return { src: "/img/aws.svg", alt: "AWS" };
  }

  if (normalized === "azurerm" || normalized === "azure") {
    return { src: "/img/azure.svg", alt: "Azure" };
  }

  if (normalized === "google" || normalized === "gcp") {
    return { src: "/img/gcp.svg", alt: "Google Cloud" };
  }

  return null;
}

function nodeStyle(kind: "depot" | "module" | "version") {
  if (kind === "depot") {
    return {
      background: "#EAF2FF",
      border: `1px solid ${depotColor}`,
      color: "#012f66",
      borderRadius: 12,
      padding: "10px 14px",
      width: 250,
      fontWeight: 600,
      boxShadow: "0 2px 10px rgba(1, 47, 102, 0.08)",
    };
  }
  if (kind === "module") {
    return {
      background: "#E8F7EF",
      border: `1px solid ${moduleColor}`,
      color: "#064126",
      borderRadius: 12,
      padding: "10px 14px",
      width: 280,
      fontWeight: 600,
      boxShadow: "0 2px 10px rgba(6, 65, 38, 0.08)",
    };
  }
  return {
    background: "#FFF2E7",
    border: `1px solid ${versionColor}`,
    color: "#663400",
    borderRadius: 12,
    padding: "9px 12px",
    width: 280,
    fontWeight: 500,
    boxShadow: "0 2px 10px rgba(102, 52, 0, 0.08)",
  };
}

export function ResourceGraph({ graph, onSelect }: Props) {
  const [expandedModuleIds, setExpandedModuleIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    const knownModuleIds = new Set(graph.modules.map((module) => module.id));
    setExpandedModuleIds((previous) => {
      const next = new Set<string>();
      previous.forEach((moduleId) => {
        if (knownModuleIds.has(moduleId)) {
          next.add(moduleId);
        }
      });
      return next;
    });
  }, [graph.modules]);

  const nodes = useMemo<Node[]>(() => {
    const built: Node[] = [];
    let cursorY = layout.topPadding;
    const renderedModuleIds = new Set<string>();

    const renderModuleWithVersions = (module: (typeof graph.modules)[number], moduleCursorY: number) => {
      const sortedVersions = graph.versions
        .filter((v) => v.moduleId === module.id)
        .slice()
        .sort((a, b) => compareSemverDesc(a.version || a.name, b.version || b.name));
      const shouldCollapse = sortedVersions.length > 3 && !expandedModuleIds.has(module.id);
      const versions = shouldCollapse ? sortedVersions.slice(0, 3) : sortedVersions;
      const versionBlockHeight = Math.max(layout.minModuleBlockHeight, versions.length * layout.versionGap);
      const logo = providerLogo(module.provider);

      built.push({
        id: module.id,
        data: {
          label: moduleNodeLabel(
            module.name,
            logo,
            sortedVersions.length,
            shouldCollapse,
            () => {
              setExpandedModuleIds((previous) => {
                const next = new Set(previous);
                if (next.has(module.id)) {
                  next.delete(module.id);
                } else {
                  next.add(module.id);
                }
                return next;
              });
            }
          ),
        },
        position: { x: laneX.module, y: moduleCursorY + layout.moduleInnerTop },
        style: nodeStyle("module"),
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      });

      versions.forEach((version, versionIndex) => {
        const versionLabel = version.version || version.name;

        built.push({
          id: version.id,
          data: { label: versionNodeLabel(versionLabel, versionIndex === 0) },
          position: {
            x: laneX.version,
            y: moduleCursorY + versionIndex * layout.versionGap,
          },
          style: nodeStyle("version"),
          sourcePosition: Position.Right,
          targetPosition: Position.Left,
        });
      });

      renderedModuleIds.add(module.id);
      return versionBlockHeight + layout.moduleGap;
    };

    graph.depots.forEach((depot) => {
      const depotStartY = cursorY;

      built.push({
        id: depot.id,
        data: { label: depotNodeLabel(depot.name) },
        position: { x: laneX.depot, y: depotStartY + 22 },
        style: nodeStyle("depot"),
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      });

      const modules = graph.modules.filter((m) => m.depotId === depot.id);
      let moduleCursorY = depotStartY;

      modules.forEach((module) => {
        moduleCursorY += renderModuleWithVersions(module, moduleCursorY);
      });

      const depotBlockHeight = Math.max(layout.minDepotBlockHeight, moduleCursorY - depotStartY);
      cursorY += depotBlockHeight + layout.depotGap;
    });

    const orphanModules = graph.modules.filter((module) => !renderedModuleIds.has(module.id));
    if (orphanModules.length > 0) {
      let orphanCursorY = cursorY;
      orphanModules.forEach((module) => {
        orphanCursorY += renderModuleWithVersions(module, orphanCursorY);
      });
    }

    return built;
  }, [expandedModuleIds, graph]);

  const edges = useMemo<Edge[]>(() => {
    const visibleNodeIds = new Set(nodes.map((node) => node.id));
    return graph.edges
      .filter((edge) => visibleNodeIds.has(edge.source) && visibleNodeIds.has(edge.target))
      .map((edge) => ({
        id: edge.id,
        source: edge.source,
        target: edge.target,
        animated: edge.type === "depot-module",
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { strokeWidth: 1.6 },
        type: "smoothstep",
      }));
  }, [graph.edges, nodes]);

  const graphHeight = Math.max(680, Math.min(1400, nodes.length * 56));

  const onNodeClick: NodeMouseHandler = (_event, node) => {
    if (node.id.startsWith("depot:")) {
      const depot = graph.depots.find((d) => d.id === node.id);
      if (depot) {
        onSelect({ kind: "depot", item: depot });
      }
      return;
    }

    if (node.id.startsWith("module:")) {
      const module = graph.modules.find((m) => m.id === node.id);
      if (module) {
        onSelect({ kind: "module", item: module });
      }
      return;
    }

    const version = graph.versions.find((v) => v.id === node.id);
    if (version) {
      onSelect({ kind: "version", item: version });
    }
  };

  return (
    <div style={{ width: "100%", height: graphHeight }}>
      <ReactFlow nodes={nodes} edges={edges} onNodeClick={onNodeClick} fitView fitViewOptions={{ padding: 0.24 }}>
        <MiniMap />
        <Controls />
        <Background />
      </ReactFlow>
    </div>
  );
}
