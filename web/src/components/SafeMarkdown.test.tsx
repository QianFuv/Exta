/** Report rendering must not execute HTML, load remote images or keep unsafe links. */
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HOSTILE_MARKDOWN } from "../mocks/fixtures";
import { toSafeHref } from "../lib/safeUrl";
import { SafeMarkdown } from "./SafeMarkdown";

describe("SafeMarkdown", () => {
  it("drops raw HTML, blocks images and neutralizes script links", () => {
    const { container } = render(<SafeMarkdown source={HOSTILE_MARKDOWN} />);
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(container.innerHTML).not.toContain("onerror");
    expect(container.innerHTML).not.toContain("tracker.example");
    expect((window as unknown as { __extaPwned?: boolean }).__extaPwned).toBeUndefined();
    expect(screen.getByText("[remote chart]")).toBeInTheDocument();

    const source = screen.getByRole("link", { name: "source" });
    expect(source).toHaveAttribute("href", "https://example.com/report");
    expect(source).toHaveAttribute("target", "_blank");
    expect(source.getAttribute("rel")).toContain("noopener");
    expect(source.getAttribute("rel")).toContain("noreferrer");

    expect(screen.queryByRole("link", { name: "click me" })).toBeNull();
    expect(screen.getByText("click me")).toBeInTheDocument();
    for (const anchor of Array.from(container.querySelectorAll("a"))) {
      expect(anchor.getAttribute("href") ?? "").not.toMatch(/^javascript:/i);
    }
  });

  it("allows only http, https, mailto and fragments", () => {
    expect(toSafeHref("https://example.com/a")).toBe("https://example.com/a");
    expect(toSafeHref("mailto:ops@example.com")).toBe("mailto:ops@example.com");
    expect(toSafeHref("#notes")).toBe("#notes");
    expect(toSafeHref("javascript:alert(1)")).toBe("");
    expect(toSafeHref("data:text/html,hi")).toBe("");
    expect(toSafeHref("/relative")).toBe("");
  });
});
