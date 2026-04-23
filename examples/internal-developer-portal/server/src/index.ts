import cors from "cors";
import express from "express";
import fs from "node:fs";
import path from "node:path";
import { buildKerraregGraph } from "./k8s";

const app = express();
const port = Number(process.env.PORT || 8082);
const defaultNamespace = process.env.KERRAREG_NAMESPACE || "kerrareg-system";

app.use(cors());
app.use(express.json());

app.get("/healthz", (_req, res) => {
  res.json({ ok: true, service: "kerrareg-portal-api" });
});

app.get("/api/graph", async (req, res) => {
  try {
    const namespace = String(req.query.namespace || defaultNamespace);
    const graph = await buildKerraregGraph(namespace);
    res.json(graph);
  } catch (error) {
    const message = error instanceof Error ? error.message : "unknown error";
    res.status(500).json({
      error: "failed_to_load_graph",
      message,
      hint: "Check kubeconfig access, namespace, CRDs, and RBAC for depots/modules/versions.",
    });
  }
});

const distPath = path.resolve(process.cwd(), "dist");
if (fs.existsSync(distPath)) {
  app.use(express.static(distPath));

  app.get(/.*/, (_req, res) => {
    res.sendFile(path.join(distPath, "index.html"));
  });
}

app.listen(port, () => {
  console.log(`kerrareg portal api listening on :${port}`);
});
