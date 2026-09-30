import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";
import { defineConfig } from "eslint/config";

export default defineConfig(
  { ignores: ["**/coverage/**", "**/node_modules/**", "**/e2e/main.mjs"] },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  {
    files: [
      "**/*.mjs",
      "scripts/e2e/*.ts",
      "testdata/api-client-contract.ts",
      "tests/observability/*.{cjs,mjs}",
    ],
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        project: "./tsconfig.json",
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
);
