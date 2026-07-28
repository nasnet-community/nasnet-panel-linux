import js from "@eslint/js"
import globals from "globals"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"
import tseslint from "typescript-eslint"

export default tseslint.config(
    {
        ignores: [
            "dist",
            "coverage",
            "*.tsbuildinfo",
            "vite.config.js",
        ],
    },

    js.configs.recommended,
    tseslint.configs.recommended,
    reactHooks.configs.flat["recommended-latest"],

    {
        files: ["**/*.{ts,tsx}"],
        languageOptions: {
            ecmaVersion: 2022,
            globals: globals.browser,
        },
        plugins: {
            "react-refresh": reactRefresh,
        },
        rules: {
            "react-refresh/only-export-components": [
                "warn",
                { allowConstantExport: true },
            ],

            "@typescript-eslint/no-unused-vars": [
                "warn",
                {
                    argsIgnorePattern: "^_",
                    varsIgnorePattern: "^_",
                    caughtErrorsIgnorePattern: "^_",
                },
            ],
            "@typescript-eslint/no-explicit-any": "warn",
            "react-hooks/exhaustive-deps": "warn",

            "react-hooks/set-state-in-effect": "warn",
            "react-hooks/purity": "warn",
            "react-hooks/refs": "warn",
            "react-hooks/immutability": "warn",
            "react-hooks/static-components": "warn",
            "react-hooks/void-use-memo": "warn",
            "react-hooks/preserve-manual-memoization": "warn",
            "react-hooks/incompatible-library": "warn",
            "react-hooks/use-memo": "warn",
            "react-hooks/globals": "warn",
            "react-hooks/error-boundaries": "warn",
            "react-hooks/unsupported-syntax": "warn",

            "react-hooks/rules-of-hooks": "error",
            "react-hooks/set-state-in-render": "error",
        },
    },

    {
        files: ["**/*.test.{ts,tsx}", "src/test/**/*.{ts,tsx}"],
        languageOptions: {
            globals: { ...globals.vitest, ...globals.node },
        },
    },

    {
        files: ["*.config.{ts,mts,js,mjs}"],
        languageOptions: {
            globals: globals.node,
        },
    },
)
