import type { ReactNode } from "react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Divider,
  Drawer,
  Stack,
  Typography,
  Link,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { SelectedNode } from "../types";

type Props = {
  selectedNode: SelectedNode;
  onClose: () => void;
};

function renderSyncedChip(synced?: boolean) {
  if (synced === undefined) {
    return <Chip size="small" label="unknown" />;
  }

  return (
    <Chip
      size="small"
      color={synced ? "success" : "warning"}
      label={synced ? "synced" : "not synced"}
    />
  );
}

function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Stack direction="row" justifyContent="space-between" alignItems="flex-start" spacing={2}>
      <Typography variant="body2" color="text.secondary">{label}</Typography>
      <Box sx={{ textAlign: "right", wordBreak: "break-word" }}>
        {typeof value === "string" ? <Typography variant="body2">{value}</Typography> : value}
      </Box>
    </Stack>
  );
}

function ProviderBadge({ provider }: { provider?: string }) {
  const theme = useTheme();
  const normalized = provider?.toLowerCase();

  if (!provider) {
    return <Typography variant="body2">n/a</Typography>;
  }

  if (normalized === "aws") {
    return (
      <Box
        component="img"
        src={theme.palette.mode === "dark" ? "/img/aws-dark.svg" : "/img/aws.svg"}
        alt="AWS"
        sx={{ width: 24, height: 24, display: "block", borderRadius: 0.5 }}
      />
    );
  }

  if (normalized === "azurerm" || normalized === "azure") {
    return (
      <Box
        component="img"
        src="/img/azure.svg"
        alt="Azure"
        sx={{ width: 24, height: 24, display: "block", borderRadius: 0.5 }}
      />
    );
  }

  if (normalized === "google" || normalized === "gcp") {
    return (
      <Box
        component="img"
        src="/img/gcp.svg"
        alt="Google Cloud"
        sx={{ width: 24, height: 24, display: "block", borderRadius: 0.5 }}
      />
    );
  }

  return <Typography variant="body2">{provider}</Typography>;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Accordion disableGutters elevation={0} sx={{ border: "1px solid", borderColor: "divider", borderRadius: 1.5, overflow: "hidden" }}>
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Typography variant="subtitle2">{title}</Typography>
      </AccordionSummary>
      <AccordionDetails sx={{ pt: 0 }}>
        <Stack spacing={1.25}>{children}</Stack>
      </AccordionDetails>
    </Accordion>
  );
}

export function NodeDetailsPanel({ selectedNode, onClose }: Props) {
  return (
    <Drawer anchor="right" open={Boolean(selectedNode)} onClose={onClose}>
      <Box sx={{ width: 360, p: 2 }}>
        {!selectedNode && null}
        {selectedNode?.kind === "depot" && (
          <Stack spacing={2}>
            <Typography variant="h6">Depot: {selectedNode.item.name}</Typography>
            <Typography variant="body2" color="text.secondary">
              Namespace: {selectedNode.item.namespace}
            </Typography>
            <Typography variant="body2">
              Polling interval: {selectedNode.item.pollingIntervalMinutes ?? "not set"}
            </Typography>
            <Divider />
            <Typography variant="subtitle2">Managed Modules</Typography>
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              {selectedNode.item.managedModuleNames.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  No modules reported in status.
                </Typography>
              ) : (
                selectedNode.item.managedModuleNames.map((name) => (
                  <Chip key={name} label={name} size="small" />
                ))
              )}
            </Stack>
            <Divider />
            <Section title="Spec">
              <DetailRow
                label="Polling Interval"
                value={String(selectedNode.item.spec?.pollingIntervalMinutes ?? selectedNode.item.pollingIntervalMinutes ?? "not set")}
              />
              <DetailRow
                label="Storage Path"
                value={selectedNode.item.spec?.global?.storageConfig?.fileSystem?.directoryPath || "not set"}
              />
              <DetailRow
                label="GitHub Client"
                value={
                  selectedNode.item.spec?.global?.githubClientConfig?.useAuthenticatedClient === undefined
                    ? "not set"
                    : selectedNode.item.spec?.global?.githubClientConfig?.useAuthenticatedClient
                      ? "authenticated"
                      : "anonymous"
                }
              />
              <Divider />
              <Typography variant="subtitle2">Module Configs</Typography>
              {(selectedNode.item.spec?.moduleConfigs || []).length === 0 ? (
                <Typography variant="body2" color="text.secondary">No module configs.</Typography>
              ) : (
                <Stack spacing={1}>
                  {(selectedNode.item.spec?.moduleConfigs || []).map((moduleConfig, index) => (
                    <Box key={`${moduleConfig.name || "module"}-${index}`} sx={{ border: "1px solid", borderColor: "divider", borderRadius: 1, p: 1 }}>
                      <Stack spacing={0.75}>
                        <Typography variant="body2" sx={{ fontWeight: 600 }}>{moduleConfig.name || "unnamed module"}</Typography>
                        <DetailRow label="Provider" value={<ProviderBadge provider={moduleConfig.provider} />} />
                        <DetailRow label="Repo Owner" value={moduleConfig.repoOwner || "n/a"} />
                        <DetailRow label="File Format" value={moduleConfig.fileFormat || "n/a"} />
                        <DetailRow label="Version Constraints" value={moduleConfig.versionConstraints || "n/a"} />
                        {moduleConfig.repoUrl && (
                          <Link href={moduleConfig.repoUrl} target="_blank" rel="noreferrer">Repository</Link>
                        )}
                      </Stack>
                    </Box>
                  ))}
                </Stack>
              )}
            </Section>
            <Section title="Status">
              <Typography variant="subtitle2">Managed Modules</Typography>
              <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                {(selectedNode.item.status?.modules || selectedNode.item.managedModuleNames || []).length === 0 ? (
                  <Typography variant="body2" color="text.secondary">No modules reported.</Typography>
                ) : (
                  (selectedNode.item.status?.modules || selectedNode.item.managedModuleNames || []).map((name) => (
                    <Chip key={name} label={name} size="small" />
                  ))
                )}
              </Stack>
            </Section>
          </Stack>
        )}

        {selectedNode?.kind === "module" && (
          <Stack spacing={2}>
            <Typography variant="h6">Module: {selectedNode.item.name}</Typography>
            <Typography variant="body2" color="text.secondary">
              Namespace: {selectedNode.item.namespace}
            </Typography>
            <Stack direction="row" spacing={1} alignItems="center">
              {renderSyncedChip(selectedNode.item.synced)}
              <Typography variant="body2">Provider:</Typography>
              <ProviderBadge provider={selectedNode.item.provider} />
            </Stack>
            <Typography variant="body2">
              Latest version: {selectedNode.item.latestVersion || "n/a"}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {selectedNode.item.syncStatus || "No sync status available"}
            </Typography>
            {selectedNode.item.repoUrl && (
              <Link href={selectedNode.item.repoUrl} target="_blank" rel="noreferrer">
                GitHub repository
              </Link>
            )}
            <Divider />
            <Section title="Spec">
              <DetailRow label="Module Name" value={selectedNode.item.spec?.moduleConfig?.name || selectedNode.item.name} />
              <DetailRow
                label="Provider"
                value={<ProviderBadge provider={selectedNode.item.spec?.moduleConfig?.provider || selectedNode.item.provider} />}
              />
              <DetailRow label="Repo Owner" value={selectedNode.item.spec?.moduleConfig?.repoOwner || "n/a"} />
              <DetailRow label="File Format" value={selectedNode.item.spec?.moduleConfig?.fileFormat || "n/a"} />
              <DetailRow label="Version Constraints" value={selectedNode.item.spec?.moduleConfig?.versionConstraints || "n/a"} />
              <DetailRow
                label="Storage Path"
                value={selectedNode.item.spec?.moduleConfig?.storageConfig?.fileSystem?.directoryPath || "not set"}
              />
              <Divider />
              <Typography variant="subtitle2">Declared Versions</Typography>
              {(selectedNode.item.spec?.versions || []).length === 0 ? (
                <Typography variant="body2" color="text.secondary">No declared versions.</Typography>
              ) : (
                <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
                  {(selectedNode.item.spec?.versions || []).map((versionRef) => (
                    <Chip key={`${versionRef.version || versionRef.name}`} label={versionRef.version || versionRef.name || "unknown"} size="small" />
                  ))}
                </Stack>
              )}
            </Section>
            <Section title="Status">
              <DetailRow label="Latest Version" value={selectedNode.item.status?.latestVersion || selectedNode.item.latestVersion || "n/a"} />
              <DetailRow label="Sync Status" value={selectedNode.item.status?.syncStatus || selectedNode.item.syncStatus || "n/a"} />
              <Stack direction="row" spacing={1} alignItems="center">
                <Typography variant="body2" color="text.secondary">Synced</Typography>
                {renderSyncedChip(selectedNode.item.status?.synced ?? selectedNode.item.synced)}
              </Stack>
              <Divider />
              <Typography variant="subtitle2">Version References</Typography>
              {!selectedNode.item.status?.moduleVersionRefs || Object.keys(selectedNode.item.status.moduleVersionRefs).length === 0 ? (
                <Typography variant="body2" color="text.secondary">No module version refs.</Typography>
              ) : (
                <Stack spacing={1}>
                  {Object.entries(selectedNode.item.status.moduleVersionRefs).map(([version, ref]) => (
                    <Box key={version} sx={{ border: "1px solid", borderColor: "divider", borderRadius: 1, p: 1 }}>
                      <Stack spacing={0.75}>
                        <DetailRow label="Version" value={version} />
                        <DetailRow label="Name" value={ref.name || "n/a"} />
                        <DetailRow label="File" value={ref.fileName || "n/a"} />
                      </Stack>
                    </Box>
                  ))}
                </Stack>
              )}
            </Section>
          </Stack>
        )}

        {selectedNode?.kind === "version" && (
          <Stack spacing={2}>
            <Typography variant="h6">Version: {selectedNode.item.version || selectedNode.item.name}</Typography>
            {renderSyncedChip(selectedNode.item.synced)}
            <Typography variant="body2" color="text.secondary">
              {selectedNode.item.syncStatus || "No sync status available"}
            </Typography>
            <Divider />
            <Typography variant="subtitle2">Checksum</Typography>
            <Typography variant="body2" sx={{ wordBreak: "break-all" }}>
              {selectedNode.item.checksum || "not available"}
            </Typography>
            <Divider />
            <Section title="Spec">
              <DetailRow label="Version" value={selectedNode.item.spec?.version || selectedNode.item.version || "n/a"} />
              <DetailRow label="Type" value={selectedNode.item.spec?.type || "n/a"} />
              <DetailRow label="File Name" value={selectedNode.item.spec?.fileName || "n/a"} />
              <Divider />
              <Typography variant="subtitle2">Module Config Ref</Typography>
              <DetailRow label="Name" value={selectedNode.item.spec?.moduleConfigRef?.name || "n/a"} />
              <DetailRow label="Provider" value={<ProviderBadge provider={selectedNode.item.spec?.moduleConfigRef?.provider} />} />
              <DetailRow label="Repo Owner" value={selectedNode.item.spec?.moduleConfigRef?.repoOwner || "n/a"} />
              <DetailRow label="File Format" value={selectedNode.item.spec?.moduleConfigRef?.fileFormat || "n/a"} />
              {selectedNode.item.spec?.moduleConfigRef?.repoUrl && (
                <Link href={selectedNode.item.spec.moduleConfigRef.repoUrl} target="_blank" rel="noreferrer">Repository</Link>
              )}
            </Section>
            <Section title="Status">
              <Stack direction="row" spacing={1} alignItems="center">
                <Typography variant="body2" color="text.secondary">Synced</Typography>
                {renderSyncedChip(selectedNode.item.status?.synced ?? selectedNode.item.synced)}
              </Stack>
              <DetailRow label="Sync Status" value={selectedNode.item.status?.syncStatus || selectedNode.item.syncStatus || "n/a"} />
              <DetailRow label="Checksum" value={selectedNode.item.status?.checksum || selectedNode.item.checksum || "n/a"} />
            </Section>
          </Stack>
        )}
      </Box>
    </Drawer>
  );
}
