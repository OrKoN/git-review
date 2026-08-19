import eslint from "@eslint/js";
import css from "@eslint/css";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**", ".cache/**", "test-results/**", "bazel-*/**"] },
  { ...eslint.configs.recommended, files: ["**/*.{js,mjs}"] },
  ...tseslint.configs.recommendedTypeChecked.map((config) => ({ ...config, files: ["**/*.ts"] })),
  {
    files: ["**/*.ts"],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    rules: {
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "off",
      "@typescript-eslint/unbound-method": "off",
      "@typescript-eslint/no-base-to-string": "off",
      "@typescript-eslint/no-unnecessary-type-assertion": "off",
      "preserve-caught-error": "off",
    },
  },
  {
    files: ["**/*.test.ts", "e2e/**/*.ts"],
    languageOptions: {
      parserOptions: {
        projectService: { allowDefaultProject: ["e2e/*.ts"] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/no-floating-promises": "off",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-call": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-return": "off",
    },
  },
  {
    files: ["**/*.css"],
    plugins: { css },
    language: "css/css",
    rules: { ...css.configs.recommended.rules, "css/no-important": "off", "css/use-baseline": "off" },
  },
  {
    files: ["**/*.{js,mjs}"],
    languageOptions: { globals: { URL: "readonly", process: "readonly" } },
  },
);
