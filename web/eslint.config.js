/** Apply the shared frontend checks without rewriting generated contract files. */
import javascript from "@eslint/js";
import typescript from "typescript-eslint";
import hooks from "eslint-plugin-react-hooks";

export default typescript.config(
  {
    ignores: [
      "dist/**",
      "node_modules/**",
      "src/generated/**",
      "coverage/**",
      "test-results/**",
      "playwright-report/**",
    ],
  },
  javascript.configs.recommended,
  ...typescript.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { "react-hooks": hooks },
    rules: {
      ...hooks.configs.recommended.rules,
    },
  },
);
