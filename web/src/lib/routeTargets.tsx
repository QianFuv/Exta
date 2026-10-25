/** Contract mapping of each operation to the provider profiles that can serve it. */
import type { Schemas } from "./api";

/** Static profile/provider pairs per operation from the contract's mapping table. */
export const STATIC_TARGETS: Record<Schemas["Operation"], Schemas["RouteTarget"][]> = {
  search: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
  ],
  advanced_search: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
    { provider: "exa", profile: "exa_deep" },
    { provider: "tavily", profile: "tavily_advanced" },
  ],
  fetch: [
    { provider: "exa", profile: "common" },
    { provider: "tavily", profile: "common" },
  ],
  map: [{ provider: "tavily", profile: "common" }],
  crawl: [{ provider: "tavily", profile: "common" }],
  research_create: [
    { provider: "exa", profile: "exa_auto" },
    { provider: "tavily", profile: "tavily_mini" },
    { provider: "tavily", profile: "tavily_pro" },
  ],
};
