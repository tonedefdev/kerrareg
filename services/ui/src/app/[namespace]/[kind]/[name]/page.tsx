import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import IconButton from "@mui/material/IconButton";
import Breadcrumbs from "@mui/material/Breadcrumbs";
import Tooltip from "@mui/material/Tooltip";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import ErrorIcon from "@mui/icons-material/Error";
import StorageIcon from "@mui/icons-material/Storage";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import GitHubIcon from "@mui/icons-material/GitHub";
import InventoryIcon from "@mui/icons-material/Inventory";
import DescriptionIcon from "@mui/icons-material/Description";
import Link from "next/link";
import CodeIcon from "@mui/icons-material/Code";
import SeverityBadge from "@/components/SeverityBadge";
import ScanDrillDown from "@/components/ScanDrillDown";
import ProviderLogo from "@/components/ProviderLogo";
import CopyButton from "@/components/CopyButton";
import DrillDownWarningBridge from "@/components/DrillDownWarningBridge";
import UsageSnippet from "@/components/UsageSnippet";
import ResourceReadme from "@/components/ResourceReadme";
import { getResourceDetail, listDepots } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { notFound } from "next/navigation";

// Resource-kind hues are fixed brand colors (matching DepotGraph.tsx's
// MODULE_BORDER/PROVIDER_BORDER) — they must NOT swap with the primary/
// secondary palette between light and dark mode, otherwise "provider" and
// "module" chips would trade colors depending on the active scheme.
const MODULE_COLOR = "#03deb8";
const PROVIDER_COLOR = "#047df1";

interface PageProps {
  params: Promise<{
    namespace: string;
    kind: string;
    name: string;
  }>;
}

function SectionCard({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card sx={{ mb: 2 }}>
      <CardContent>
        <Box display="flex" alignItems="center" gap={1} mb={2}>
          <Box sx={{ color: "primary.main", display: "flex" }}>{icon}</Box>
          <Typography variant="h6" sx={{ fontSize: "0.9375rem", fontWeight: 600 }}>
            {title}
          </Typography>
        </Box>
        {children}
      </CardContent>
    </Card>
  );
}

function LabelValue({ label, value }: { label: string; value?: React.ReactNode }) {
  if (!value && value !== 0) return null;
  return (
    <Box display="flex" gap={1} mb={0.75} alignItems="flex-start">
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          minWidth: 150,
          flexShrink: 0,
          pt: "2px",
          textTransform: "uppercase",
          letterSpacing: "0.04em",
          fontSize: "0.7rem",
          fontWeight: 600,
        }}
      >
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontFamily: "monospace", fontSize: "0.8125rem", wordBreak: "break-all" }}>
        {value}
      </Typography>
    </Box>
  );
}

function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
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

export default async function ResourceDetailPage({ params }: PageProps) {
  const { namespace, kind, name } = await params;
  const token = await getServerSessionToken();

  let detail;
  try {
    detail = await getResourceDetail(namespace, kind, name, token);
  } catch (err) {
    const msg = err instanceof Error ? err.message : "";
    if (msg.includes("404")) {
      notFound();
    }
    return (
      <Container maxWidth="xl" sx={{ py: 4 }}>
        <Alert severity="error">Failed to load resource detail: {msg}</Alert>
      </Container>
    );
  }

  const capitalizeKind = kind.charAt(0).toUpperCase() + kind.slice(1);
  const depots = detail.depotRef ? await listDepots(token).catch(() => undefined) : undefined;
  const managingDepot = detail.depotRef
    ? depots?.items.find((d) => d.namespace === detail.depotRef?.namespace && d.name === detail.depotRef?.name)
    : undefined;
  const hasStorageConfigValues =
    !!detail.storageConfig?.backend ||
    !!detail.storageConfig?.bucket ||
    !!detail.storageConfig?.region ||
    !!detail.storageConfig?.key ||
    !!detail.storageConfig?.directoryPath ||
    !!detail.storageConfig?.accountName ||
    !!detail.storageConfig?.accountUrl ||
    !!detail.storageConfig?.subscriptionID ||
    !!detail.storageConfig?.resourceGroup;
  const sortedVersions = [...(detail.versions ?? [])].sort((a, b) => compareVersionDesc(a.version, b.version));
  const hasUnsyncedVersions = (detail.versions ?? []).some(
    (v) => !v.synced || /failed|error/i.test(v.syncStatus ?? ""),
  );
  const isProviderKind = kind === "provider";

  const rawBase = process.env.NEXT_PUBLIC_BASE_URL ?? "";
  const registryHost = rawBase ? new URL(rawBase).host : "your-opendepot-host";
  const registryBaseUrl = rawBase || "https://your-opendepot-host";

  return (
    <Container maxWidth="xl" sx={{ py: 4, px: { xs: 2, md: 4 } }}>
      {/* Breadcrumbs */}
      <Breadcrumbs sx={{ mb: 3, "& a": { textDecoration: "none", color: "text.secondary", "&:hover": { color: "primary.main" } } }}>
        <Link href="/">Registry</Link>
        <Link href={`/?namespace=${namespace}`}>
          <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
            {namespace}
          </Typography>
        </Link>
        <Link href={`/?kind=${kind}`}>{capitalizeKind}</Link>
        <Typography variant="body2" color="text.primary" fontWeight={600}>
          {name}
        </Typography>
      </Breadcrumbs>

      {/* Hero */}
      <Box
        sx={{
          display: "flex",
          alignItems: "flex-start",
          gap: 2.5,
          mb: 4,
          p: 3,
          borderRadius: 2,
          border: isProviderKind ? "1px solid rgba(4,125,241,0.25)" : "1px solid rgba(4,207,208,0.2)",
          background: isProviderKind
            ? "linear-gradient(135deg, rgba(4,125,241,0.08) 0%, rgba(4,125,241,0.03) 100%)"
            : "linear-gradient(135deg, rgba(4,207,208,0.06) 0%, rgba(3,222,184,0.03) 100%)",
        }}
      >
        {isProviderKind
          ? <ProviderLogo provider={detail.name} size={44} />
          : detail.provider && <ProviderLogo provider={detail.provider} size={44} />}

        <Box flex={1} minWidth={0}>
          <Typography variant="h4" component="h1" sx={{ fontWeight: 700, mb: 0.5, wordBreak: "break-word" }}>
            {detail.name}
          </Typography>
          <Typography variant="body2" sx={{ color: "text.secondary", fontFamily: "monospace", mb: 1.5 }}>
            {namespace} / {capitalizeKind}
          </Typography>

          <Box display="flex" flexWrap="wrap" gap={1} alignItems="center">
            <Chip
              label={capitalizeKind}
              size="small"
              variant="outlined"
              sx={{
                color: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                borderColor: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                '[data-mui-color-scheme="light"] &': {
                  color: "#fff",
                  bgcolor: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                },
              }}
            />
            {detail.provider && (
              <Chip
                label={detail.provider}
                size="small"
                variant="outlined"
                color="primary"
                sx={{
                  fontFamily: "monospace",
                  '[data-mui-color-scheme="light"] &': { color: "#fff", bgcolor: "primary.main" },
                }}
              />
            )}
            {detail.latestVersion && (
              <Chip
                label={displayVersion(detail.latestVersion)}
                size="small"
                variant="outlined"
                sx={{
                  fontFamily: "monospace",
                  color: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                  borderColor: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                  '[data-mui-color-scheme="light"] &': {
                    color: "#fff",
                    bgcolor: isProviderKind ? PROVIDER_COLOR : MODULE_COLOR,
                  },
                }}
              />
            )}
            <Box display="flex" alignItems="center" gap={0.5}>
              {/failed|error/i.test(detail.syncStatus) ? (
                <ErrorIcon sx={{ fontSize: 14, color: "error.main" }} />
              ) : detail.synced ? (
                <CheckCircleIcon sx={{ fontSize: 14, color: "success.main" }} />
              ) : (
                <SyncProblemIcon sx={{ fontSize: 14, color: "warning.main" }} />
              )}
              <Typography variant="caption" color="text.secondary">
                {detail.syncStatus || (detail.synced ? "Synced" : "Not synced")}
              </Typography>
            </Box>
          </Box>

          {detail.scanCounts && (
            <Box display="flex" flexWrap="wrap" gap={0.5} mt={1.5}>
              <SeverityBadge counts={detail.scanCounts} />
            </Box>
          )}
        </Box>

        {/* Source repo link */}
        {(detail.repoUrl || detail.sourceRepository) && (
          <Box flexShrink={0}>
            <Tooltip title="Open source repository" placement="top">
              <IconButton
                component="a"
                href={detail.repoUrl || detail.sourceRepository}
                target="_blank"
                rel="noopener noreferrer"
                aria-label="Open source repository"
                sx={{
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: 1.25,
                }}
              >
                <GitHubIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Box>
        )}
      </Box>

      {/* Overview */}
      <SectionCard icon={<InventoryIcon fontSize="small" />} title="Overview">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
          <LabelValue label="Namespace" value={detail.namespace} />
          <LabelValue label="Kind" value={capitalizeKind} />
          <LabelValue label="Provider" value={detail.provider} />
          {isProviderKind && (
            <LabelValue label="Provider Namespace" value={detail.providerNamespace || "hashicorp"} />
          )}
          <LabelValue label="Latest Version" value={detail.latestVersion ? displayVersion(detail.latestVersion) : undefined} />
          {detail.repoOwner && <LabelValue label="Repo Owner" value={detail.repoOwner} />}
          {detail.versionHistoryLimit !== undefined && detail.versionHistoryLimit > 0 && (
            <LabelValue label="Version History Limit" value={String(detail.versionHistoryLimit)} />
          )}
          {detail.versionConstraints && (
            <LabelValue label="Version Constraints" value={detail.versionConstraints} />
          )}

        </Box>
      </SectionCard>

      {/* README */}
      {detail.readmeContent && (
        <SectionCard icon={<DescriptionIcon fontSize="small" />} title="README">
          <ResourceReadme
            content={detail.readmeContent}
            kind={isProviderKind ? "provider" : "module"}
            namespace={detail.namespace}
            name={detail.name}
            provider={detail.provider}
            registryHost={registryHost}
            providerNamespace={detail.providerNamespace}
          />
        </SectionCard>
      )}

      {/* Usage */}
      <SectionCard icon={<CodeIcon fontSize="small" />} title="Usage">
        <UsageSnippet
          kind={isProviderKind ? "provider" : "module"}
          namespace={detail.namespace}
          name={detail.name}
          provider={detail.provider}
          latestVersion={detail.latestVersion}
          versionConstraints={detail.versionConstraints}
          registryHost={registryHost}
          registryBaseUrl={registryBaseUrl}
          providerNamespace={detail.providerNamespace}
        />
      </SectionCard>

      {/* Storage Configuration */}
      <SectionCard icon={<StorageIcon fontSize="small" />} title="Storage Configuration">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
          <LabelValue
            label="Backend"
            value={detail.storageConfig?.backend || managingDepot?.storageBackend || "Not configured"}
          />
          {detail.storageConfig?.bucket && <LabelValue label="Bucket" value={detail.storageConfig.bucket} />}
          {detail.storageConfig?.region && <LabelValue label="Region" value={detail.storageConfig.region} />}
          {detail.storageConfig?.key && <LabelValue label="Key" value={detail.storageConfig.key} />}
          {detail.storageConfig?.directoryPath && <LabelValue label="Directory Path" value={detail.storageConfig.directoryPath} />}
          {detail.storageConfig?.accountName && <LabelValue label="Account Name" value={detail.storageConfig.accountName} />}
          {detail.storageConfig?.accountUrl && <LabelValue label="Account URL" value={detail.storageConfig.accountUrl} />}
          {detail.storageConfig?.subscriptionID && <LabelValue label="Subscription ID" value={detail.storageConfig.subscriptionID} />}
          {detail.storageConfig?.resourceGroup && <LabelValue label="Resource Group" value={detail.storageConfig.resourceGroup} />}
          <LabelValue label="Presign Enabled" value={detail.storageConfig?.presignEnabled ? "Yes" : "No"} />
          {detail.storageConfig?.presignTTL && <LabelValue label="Presign TTL" value={detail.storageConfig.presignTTL} />}
          {!hasStorageConfigValues && managingDepot?.storageBackend && (
            <LabelValue label="Inherited From" value={`${managingDepot.namespace} / ${managingDepot.name}`} />
          )}
        </Box>
      </SectionCard>

      {/* GitHub Configuration */}
      <SectionCard icon={<GitHubIcon fontSize="small" />} title="GitHub Configuration">
        <LabelValue
          label="Authenticated Client"
          value={detail.githubConfig?.useAuthenticatedClient ? "Yes" : "No"}
        />
      </SectionCard>

      {/* Depot Association */}
      {detail.depotRef && (
        <SectionCard icon={<WarehouseIcon fontSize="small" />} title="Depot">
          <Box display="flex" gap={1} alignItems="center">
            <Chip
              label={`${detail.depotRef.namespace} / ${detail.depotRef.name}`}
              variant="outlined"
              size="small"
              sx={{
                fontFamily: "monospace",
                borderColor: "rgba(3,222,184,0.4)",
                color: "#03deb8",
                '[data-mui-color-scheme="light"] &': {
                  color: "#fff",
                  bgcolor: "primary.main",
                  borderColor: "primary.main",
                },
              }}
            />
          </Box>
        </SectionCard>
      )}

      <Divider sx={{ my: 3 }} />

      {/* Versions */}
      <DrillDownWarningBridge
        initialHasUnsynced={hasUnsyncedVersions}
        namespace={namespace}
        kind={kind}
        name={name}
      />

      {/* Scan findings */}
      <Divider sx={{ my: 3 }} />
      <ScanDrillDown
        namespace={namespace}
        kind={kind}
        name={name}
        initialSourceScanFindings={detail.sourceScanFindings ?? []}
        initialBinaryScanFindings={detail.binaryScanFindings ?? {}}
        versions={sortedVersions}
      />
    </Container>
  );
}
