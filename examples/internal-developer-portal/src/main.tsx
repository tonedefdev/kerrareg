import React from "react";
import ReactDOM from "react-dom/client";
import { useMemo } from "react";
import { CssBaseline, ThemeProvider } from "@mui/material";
import { getAppTheme } from "./theme";
import { App } from "./App";
import "reactflow/dist/style.css";

function Root() {
  const theme = useMemo(() => getAppTheme(), []);

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <App />
    </ThemeProvider>
  );
}

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>
);
