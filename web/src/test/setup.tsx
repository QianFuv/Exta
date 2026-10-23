/** Vitest setup: DOM matchers, MSW lifecycle and isolated browser storage per test. */
import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach } from "vitest";
import { createMockState, mockDb } from "../mocks/handlers";
import { server } from "./server";

configure({ asyncUtilTimeout: 5000 });

/** jsdom has no layout observer; geometry is verified against the packaged browser. */
class StaticResizeObserver implements ResizeObserver {
  /** Unit tests do not synthesize layout measurements. */
  observe() {
    return undefined;
  }
  /** Release a non-layout observation. */
  unobserve() {
    return undefined;
  }
  /** Release all non-layout observations. */
  disconnect() {
    return undefined;
  }
}
globalThis.ResizeObserver = StaticResizeObserver;

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));

beforeEach(() => {
  mockDb.state = createMockState();
  window.sessionStorage.clear();
  window.localStorage.clear();
  document.documentElement.dataset.theme = "system";
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
});

afterAll(() => server.close());
