/** Preserve dialog focus restoration when a refreshed list replaces its trigger. */
import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { Dialog } from "./Dialog";
import { I18nProvider } from "../lib/i18n";

/** Model a trigger refreshed while its dialog remains open, without mocking the dialog. */
function DialogFixture({ scenario }: { scenario: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [isReplaced, setIsReplaced] = useState(false);
  const hasReplacement = !["missing", "hidden-only", "other-dialog-only"].includes(scenario);
  return (
    <I18nProvider>
      {(!isReplaced || hasReplacement) && (
        <a
          key={isReplaced ? "replacement" : "original"}
          href="?request=one"
          onClick={(event) => {
            event.preventDefault();
            setIsOpen(true);
          }}
        >
          Open request
        </a>
      )}
      {isReplaced && scenario === "ambiguous" && <a href="?request=one">Duplicate</a>}
      {isReplaced && (scenario === "hidden" || scenario === "hidden-only") && (
        <>
          <a href="?request=one" hidden>
            Hidden
          </a>
          <a href="?request=one" style={{ display: "none" }}>
            Display hidden
          </a>
          <a href="?request=one" style={{ visibility: "hidden" }}>
            Visibility hidden
          </a>
          <a href="?request=one" tabIndex={-1}>
            Unfocusable
          </a>
          <div inert>
            <a href="?request=one">Inert</a>
          </div>
        </>
      )}
      {isReplaced && ["other-dialog", "other-dialog-only"].includes(scenario) && (
        <div role="dialog" aria-label="Other dialog">
          <a href="?request=one">Other dialog request</a>
        </div>
      )}
      {isOpen && (
        <Dialog title="Request" onClose={() => setIsOpen(false)}>
          <button onClick={() => setIsReplaced(true)}>Refresh trigger</button>
        </Dialog>
      )}
    </I18nProvider>
  );
}

it("returns focus to the original connected trigger", async () => {
  render(<DialogFixture scenario="connected" />);
  const original = screen.getByRole("link", { name: "Open request" });
  await userEvent.click(original);
  await userEvent.keyboard("{Escape}");
  expect(original).toHaveFocus();
});

it.each(["replacement", "hidden", "other-dialog"])(
  "returns focus only to the unique visible replacement (%s)",
  async (scenario) => {
    render(<DialogFixture scenario={scenario} />);
    const original = screen.getByRole("link", { name: "Open request" });
    await userEvent.click(original);
    await userEvent.click(
      within(screen.getByRole("dialog", { name: "Request" })).getByRole("button", {
        name: "Refresh trigger",
      }),
    );
    expect(original.isConnected).toBe(false);
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("link", { name: "Open request" })).toHaveFocus();
  },
);

it.each(["ambiguous", "missing", "hidden-only", "other-dialog-only"])(
  "does not guess a replacement when no unique visible link exists (%s)",
  async (scenario) => {
    render(<DialogFixture scenario={scenario} />);
    await userEvent.click(screen.getByRole("link", { name: "Open request" }));
    await userEvent.click(
      within(screen.getByRole("dialog", { name: "Request" })).getByRole("button", {
        name: "Refresh trigger",
      }),
    );
    await userEvent.keyboard("{Escape}");
    expect(document.activeElement).toBe(document.body);
  },
);
