import { Card, CardContent, Grid, Typography } from "@mui/material";
import type { GraphResponse } from "../types";

type Props = {
  graph: GraphResponse;
};

export function ResourceSummaryCards({ graph }: Props) {
  const items = [
    { label: "Depots", value: graph.summary.depotCount },
    { label: "Modules", value: graph.summary.moduleCount },
    { label: "Versions", value: graph.summary.versionCount },
    {
      label: "Synced Versions",
      value: `${graph.summary.syncedVersions}/${graph.summary.versionCount}`,
    },
  ];

  return (
    <Grid container spacing={2}>
      {items.map((item) => (
        <Grid key={item.label} size={{ xs: 12, sm: 6, md: 3 }}>
          <Card elevation={0} sx={{ border: "1px solid", borderColor: "divider" }}>
            <CardContent>
              <Typography variant="caption" color="text.secondary">
                {item.label}
              </Typography>
              <Typography variant="h5">{item.value}</Typography>
            </CardContent>
          </Card>
        </Grid>
      ))}
    </Grid>
  );
}
