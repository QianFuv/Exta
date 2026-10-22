/** Node MSW server shared by component tests. */
import { setupServer } from "msw/node";
import { handlers } from "../mocks/handlers";

/** Test server intercepting the generated client's fetch calls. */
export const server = setupServer(...handlers);
