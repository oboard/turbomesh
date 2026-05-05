import { describe, expect, it } from "vitest";
import { buildSessionURL, getSessionSlug, isValidSlug } from "./domain";

describe("domain helpers", () => {
  it("extracts a wildcard session slug", () => {
    expect(getSessionSlug("abc12345.web.oboard.fun")).toBe("abc12345");
    expect(getSessionSlug("web.oboard.fun")).toBe("");
    expect(getSessionSlug("localhost")).toBe("");
  });

  it("validates slugs for DNS labels", () => {
    expect(isValidSlug("abc12345")).toBe(true);
    expect(isValidSlug("session-123")).toBe(true);
    expect(isValidSlug("short")).toBe(false);
    expect(isValidSlug("-abc12345")).toBe(false);
    expect(isValidSlug("ABC12345")).toBe(false);
  });

  it("preserves the homepage scheme when opening a slug", () => {
    expect(buildSessionURL("abc12345", "https:")).toBe("https://abc12345.web.oboard.fun/");
    expect(buildSessionURL("abc12345", "http:")).toBe("http://abc12345.web.oboard.fun/");
  });
});
