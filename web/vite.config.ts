/** Build Exta's static application for embedding in the Go executable. */
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/app/",
  plugins: [react()],
  build: {
    manifest: true,
    assetsInlineLimit: 0,
    sourcemap: false,
    modulePreload: { polyfill: false },
  },
  server: {
    host: "127.0.0.1",
    strictPort: true,
    proxy: {
      "/v1": "http://127.0.0.1:8789",
      "/mcp": "http://127.0.0.1:8789",
    },
  },
});
