import { describe, expect, it } from "vitest";

import {
  buildCanonicalProviderSource,
  buildProviderMirrorUrl,
} from "./registrySource";

describe("provider network mirror helpers", () => {
  it("preserves the canonical provider identity", () => {
    expect(buildCanonicalProviderSource("registry.opentofu.org", "hashicorp", "aws")).toBe("registry.opentofu.org/hashicorp/aws");
    expect(buildCanonicalProviderSource("registry.terraform.io", "hashicorp", "null")).toBe("registry.terraform.io/hashicorp/null");
    expect(buildCanonicalProviderSource("", "", "null")).toBe("registry.opentofu.org/hashicorp/null");
  });

  it("builds a namespace-scoped mirror base URL", () => {
    expect(
      buildProviderMirrorUrl("https://opendepot.example.com/", "opendepot-system"),
    ).toBe(
      "https://opendepot.example.com/opendepot/providers/mirror/v1/opendepot-system/",
    );
  });
});
