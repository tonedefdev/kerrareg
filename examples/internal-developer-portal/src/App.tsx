import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  AppBar,
  Box,
  Button,
  Chip,
  CircularProgress,
  Container,
  Stack,
  TextField,
  Toolbar,
} from "@mui/material";
import RefreshIcon from "@mui/icons-material/Refresh";
import { fetchGraph } from "./api";
import type { GraphResponse, SelectedNode } from "./types";
import { ResourceSummaryCards } from "./components/ResourceSummaryCards";
import { ResourceGraph } from "./components/ResourceGraph";
import { NodeDetailsPanel } from "./components/NodeDetailsPanel";

const defaultNamespace = "kerrareg-system";
const depotColor = "#0057B8";
const moduleColor = "#0A7D45";
const versionColor = "#A15700";

export function App() {
  const [namespace, setNamespace] = useState(defaultNamespace);
  const [graph, setGraph] = useState<GraphResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedNode, setSelectedNode] = useState<SelectedNode>(null);

  const logoSrc = "/img/kerrareg.png";

  const loadGraph = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchGraph(namespace);
      setGraph(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data");
    } finally {
      setLoading(false);
    }
  }, [namespace]);

  useEffect(() => {
    loadGraph();
  }, [loadGraph]);

  useEffect(() => {
    const interval = setInterval(() => {
      loadGraph();
    }, 15000);

    return () => clearInterval(interval);
  }, [loadGraph]);

  const generatedAt = useMemo(() => {
    if (!graph) {
      return "n/a";
    }
    return new Date(graph.generatedAt).toLocaleTimeString();
  }, [graph]);

  return (
    <Box
      sx={{
        minHeight: "100vh",
        background: "linear-gradient(180deg, #111827 0%, #020617 100%)",
      }}
    >
      <AppBar position="static" elevation={0} color="transparent" sx={{ borderBottom: "1px solid", borderColor: "divider" }}>
        <Toolbar sx={{ justifyContent: "space-between" }}>
          <Stack direction="row" spacing={1} alignItems="center">
            <Box
              component="img"
              src={logoSrc}
              alt="Kerrareg"
              sx={{
                width: { xs: 100, sm: 120, md: 136 },
                height: "auto",
                maxHeight: 36,
                objectFit: "contain",
              }}
            />
          </Stack>
        </Toolbar>
      </AppBar>

      <Container maxWidth="xl" sx={{ py: 3 }}>
        <Stack spacing={2}>
          <Stack
            direction={{ xs: "column", md: "row" }}
            spacing={2}
            alignItems={{ xs: "stretch", md: "center" }}
            justifyContent="space-between"
          >
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2} alignItems={{ xs: "stretch", sm: "center" }}>
              <TextField
                size="small"
                label="Namespace"
                value={namespace}
                onChange={(event) => setNamespace(event.target.value)}
              />
              <Button variant="contained" startIcon={<RefreshIcon />} onClick={loadGraph}>
                Refresh
              </Button>
            </Stack>

            <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
              <Chip label="Depot" size="small" sx={{ bgcolor: "#EAF2FF", borderColor: depotColor, borderWidth: 1, borderStyle: "solid", color: "#012f66" }} />
              <Chip label="Module" size="small" sx={{ bgcolor: "#E8F7EF", borderColor: moduleColor, borderWidth: 1, borderStyle: "solid", color: "#064126" }} />
              <Chip label="Version" size="small" sx={{ bgcolor: "#FFF2E7", borderColor: versionColor, borderWidth: 1, borderStyle: "solid", color: "#663400" }} />
              <Chip label={`Updated: ${generatedAt}`} size="small" />
              <Chip label="Auto refresh: 15s" size="small" color="secondary" />
            </Stack>
          </Stack>

          {error && <Alert severity="error">{error}</Alert>}

          {loading && !graph && (
            <Stack alignItems="center" justifyContent="center" sx={{ py: 10 }}>
              <CircularProgress />
            </Stack>
          )}

          {graph && (
            <Stack spacing={2}>
              <ResourceSummaryCards graph={graph} />
              <Box sx={{ border: "1px solid", borderColor: "divider", borderRadius: 3, p: 1, backgroundColor: "background.paper" }}>
                <ResourceGraph graph={graph} onSelect={setSelectedNode} />
              </Box>
            </Stack>
          )}
        </Stack>
      </Container>

      <NodeDetailsPanel selectedNode={selectedNode} onClose={() => setSelectedNode(null)} />
    </Box>
  );
}
