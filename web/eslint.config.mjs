import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// The rules are put together here, without eslint-config-next: that one
// brings braces, a library with a known vulnerability (CVE-2026-93687) and
// no fixed release.
const eslintConfig = defineConfig([
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  // Storage that is blocked is simply not used: nothing to do in the catch.
  { rules: { "no-empty": ["error", { allowEmptyCatch: true }] } },
  globalIgnores([".next/**", "out/**", "build/**", "next-env.d.ts"]),
]);

export default eslintConfig;
