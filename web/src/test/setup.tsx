/** Vitest setup: DOM matchers, MSW lifecycle and isolated browser storage per test. */
import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach } from "vitest";
import { createMockState, mockDb } from "../mocks/handlers";
import { server } from "./server";

configure({ asyncUtilTimeout: 5000 });

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));

beforeEach(() => {
  mockDb.state = createMockState();
  window.sessionStorage.clear();
  window.localStorage.clear();
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
});

afterAll(() => server.close());
