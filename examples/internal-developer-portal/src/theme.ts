import { createTheme } from "@mui/material/styles";

export function getAppTheme() {
  return createTheme({
    palette: {
      mode: "dark",
      primary: {
        main: "#0057B8",
      },
      secondary: {
        main: "#007A5A",
      },
      background: {
        default: "#0f172a",
        paper: "#111827",
      },
    },
    typography: {
      fontFamily: "'IBM Plex Sans', 'Segoe UI', sans-serif",
      h4: {
        fontWeight: 700,
      },
      h6: {
        fontWeight: 600,
      },
    },
    shape: {
      borderRadius: 12,
    },
  });
}
