"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import { useColorScheme } from "@mui/material/styles";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import { buildCanonicalProviderSource, buildModuleSource } from "@/lib/registrySource";

// Make prism-react-renderer use the full prismjs instance so it picks up the
// HCL grammar we registered above via the side-effectful import (mirrors
// UsageSnippet.tsx).
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

// Height (px) of the README viewport when collapsed/expanded. Both states keep
// their own scrollbar so a long README never forces the whole page to scroll
// past it before reaching Overview/Usage/etc.
const COLLAPSED_HEIGHT = 360;
const EXPANDED_HEIGHT = 720;

// terraform-docs emits raw <a name="..."> anchors for its table of contents.
// Extend the default sanitize schema so those survive rehype-sanitize instead
// of being stripped, while everything else (scripts, event handlers, etc.)
// from untrusted repo READMEs stays blocked.
const sanitizeSchema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    a: [...(defaultSchema.attributes?.a ?? []), "name"],
  },
};

const HCL_LANGUAGES = new Set(["hcl", "terraform", "tf"]);

// terraform-docs generates its Usage example against the source repo (e.g.
// GitHub or the upstream registry). Rewrite the `source = "..."` value inside
// fenced HCL/terraform blocks so copy-pasted README snippets point at our
// registry instead, matching the "Usage" section below it.
function rewriteSource(content: string, source: string): string {
  return content.replace(/```(\w*)\n([\s\S]*?)```/g, (block, lang: string, code: string) => {
    if (!HCL_LANGUAGES.has(lang.toLowerCase())) {
      return block;
    }

    const rewritten = code.replace(/(source\s*=\s*)(["'])[^"']*\2/g, `$1$2${source}$2`);
    return "```" + lang + "\n" + rewritten + "```";
  });
}

interface ResourceReadmeProps {
  content: string;
  kind: "module" | "provider";
  namespace: string;
  name: string;
  /** Module only — the Terraform provider name (e.g. "azurerm"). */
  provider?: string;
  /** Host + port derived from NEXT_PUBLIC_BASE_URL (e.g. "opendepot.localtest.me:8080"). */
  registryHost: string;
  /** Provider only — canonical upstream namespace (e.g. "hashicorp"). */
  providerNamespace?: string;
  /** Provider only — canonical upstream registry hostname. */
  upstreamRegistry?: string;
}

export default function ResourceReadme({ content, kind, namespace, name, provider, registryHost, providerNamespace = "hashicorp", upstreamRegistry = "registry.opentofu.org" }: ResourceReadmeProps) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const codeBg = resolvedMode === "light" ? "#f0f7ff" : "#1e1e1e";
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;

  const [expanded, setExpanded] = React.useState(false);
  const [overflows, setOverflows] = React.useState(false);
  const scrollRef = React.useRef<HTMLDivElement>(null);

  const registrySource =
    kind === "provider"
      ? buildCanonicalProviderSource(upstreamRegistry, providerNamespace, name)
      : buildModuleSource(registryHost, namespace, name, provider);
  const rewrittenContent = React.useMemo(
    () => rewriteSource(content, registrySource),
    [content, registrySource],
  );

  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [rewrittenContent, expanded]);

  return (
    <Box>
      <Box
        ref={scrollRef}
        sx={{
          maxHeight: expanded ? EXPANDED_HEIGHT : COLLAPSED_HEIGHT,
          overflowY: "auto",
          pr: 0.5,
          "& h1, & h2, & h3, & h4, & h5, & h6": { fontWeight: 600, mt: 2, mb: 1 },
          "& h1": { fontSize: "1.5rem" },
          "& h2": { fontSize: "1.25rem" },
          "& h3": { fontSize: "1.125rem" },
          "& p": { mb: 1.5, lineHeight: 1.6 },
          "& ul, & ol": { mb: 1.5, pl: 3 },
          "& li": { mb: 0.5 },
          "& a": { color: "primary.main", textDecoration: "none" },
          "& a:hover": { textDecoration: "underline" },
          "& code": {
            fontFamily: "monospace",
            fontSize: "0.8125rem",
            bgcolor: codeBg,
            px: 0.5,
            py: 0.25,
            borderRadius: 0.5,
          },
          "& pre.md-plain-pre": {
            bgcolor: codeBg,
            p: 2,
            borderRadius: 1.5,
            border: "1px solid",
            borderColor: "divider",
            overflowX: "auto",
          },
          "& pre.md-plain-pre code": { bgcolor: "transparent", p: 0 },
          "& blockquote": {
            borderLeft: "3px solid",
            borderColor: "divider",
            pl: 2,
            ml: 0,
            color: "text.secondary",
          },
          "& table": { borderCollapse: "collapse", mb: 1.5 },
          "& th, & td": {
            border: "1px solid",
            borderColor: "divider",
            px: 1.5,
            py: 0.75,
          },
          "& img": { maxWidth: "100%" },
        }}
      >
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema]]}
          components={{
            pre({ children }) {
              const codeElement = React.isValidElement(children) ? children : null;
              const codeProps = codeElement?.props as
                | { className?: string; children?: React.ReactNode }
                | undefined;
              const lang = /language-(\w+)/.exec(codeProps?.className || "")?.[1];

              if (!lang || !HCL_LANGUAGES.has(lang)) {
                return <pre className="md-plain-pre">{children}</pre>;
              }

              const code = String(codeProps?.children ?? "").replace(/\n$/, "");
              return (
                <Highlight
                  prism={Prism as typeof Prism}
                  theme={prismTheme}
                  code={code}
                  language={"hcl" as Language}
                >
                  {({ style, tokens, getLineProps, getTokenProps }) => (
                    <Box
                      component="pre"
                      sx={{
                        m: 0,
                        p: 2,
                        borderRadius: 1.5,
                        border: "1px solid",
                        borderColor: "divider",
                        fontFamily: "monospace",
                        fontSize: "0.8125rem",
                        lineHeight: 1.65,
                        overflowX: "auto",
                        whiteSpace: "pre",
                        mb: 1.5,
                        ...style,
                        ...(resolvedMode === "light" && { backgroundColor: "#f0f7ff" }),
                      }}
                    >
                      {tokens.map((line, i) => (
                        <div key={i} {...getLineProps({ line })}>
                          {line.map((token, key) => (
                            <span key={key} {...getTokenProps({ token })} />
                          ))}
                        </div>
                      ))}
                    </Box>
                  )}
                </Highlight>
              );
            },
          }}
        >
          {rewrittenContent}
        </ReactMarkdown>
      </Box>
      {overflows && (
        <Box sx={{ display: "flex", justifyContent: "center", pt: 1 }}>
          <Button
            size="small"
            onClick={() => setExpanded((prev) => !prev)}
            startIcon={expanded ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
          >
            {expanded ? "Show less" : "Show more"}
          </Button>
        </Box>
      )}
    </Box>
  );
}
