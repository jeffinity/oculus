import js from "@eslint/js";
import globals from "globals";
import reactPlugin from "eslint-plugin-react";
import reactHooksPlugin from "eslint-plugin-react-hooks";
import jsxA11yPlugin from "eslint-plugin-jsx-a11y";
import importPlugin from "eslint-plugin-import";
import promisePlugin from "eslint-plugin-promise";
import unicornPlugin from "eslint-plugin-unicorn";
import eslintConfigPrettier from "eslint-config-prettier";

const webFiles = ["web/**/*.{js,jsx}"];

export default [
  {
    ignores: [
      "**/dist/**",
      "**/node_modules/**",
      "**/.build/**"
    ]
  },

  js.configs.recommended,

  {
    files: webFiles,

    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      parserOptions: {
        ecmaFeatures: {
          jsx: true
        }
      },
      globals: {
        ...globals.browser
      }
    },

    plugins: {
      react: reactPlugin,
      "react-hooks": reactHooksPlugin,
      "jsx-a11y": jsxA11yPlugin,
      import: importPlugin,
      promise: promisePlugin,
      unicorn: unicornPlugin
    },

    settings: {
      react: {
        version: "detect"
      },
      "import/resolver": {
        node: {
          extensions: [".js", ".jsx"]
        }
      }
    },

    rules: {
      // 已有推荐规则
      ...reactPlugin.configs.recommended.rules,
      ...reactPlugin.configs["jsx-runtime"].rules,
      ...reactHooksPlugin.configs.recommended.rules,
      ...jsxA11yPlugin.flatConfigs.recommended.rules,

      // ----------------------------
      // 1) 基础正确性 / 安全 / 坏味道
      // ----------------------------
      "no-console": ["warn", { allow: ["warn", "error"] }],
      "no-debugger": "error",
      "no-alert": "error",
      "no-caller": "error",
      "eqeqeq": ["error", "smart"],
      "curly": ["error", "all"],
      "consistent-return": "error",
      "default-case-last": "error",
      "dot-notation": "error",
      "no-implicit-coercion": "warn",
      "no-lonely-if": "warn",
      "no-nested-ternary": "warn",
      "no-shadow": "warn",
      "no-unneeded-ternary": "warn",
      "no-param-reassign": ["warn", { props: false }],
      "object-shorthand": ["warn", "always"],
      "prefer-arrow-callback": ["warn", { allowNamedFunctions: false, allowUnboundThis: true }],
      "prefer-const": ["error", { destructuring: "all" }],
      "prefer-destructuring": ["warn", { object: true, array: false }],
      "prefer-template": "warn",

      "no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
          ignoreRestSiblings: true
        }
      ],

      // ----------------------------
      // 2) 复杂度 / 规模约束
      // ----------------------------
      // cyclop / cyclomatic 对应的核心就是 complexity
      "complexity": ["warn", 10],
      "max-depth": ["warn", 4],
      "max-lines": [
        "warn",
        {
          max: 500,
          skipBlankLines: true,
          skipComments: true
        }
      ],
      "max-lines-per-function": [
        "warn",
        {
          max: 80,
          skipBlankLines: true,
          skipComments: true
        }
      ],
      "max-nested-callbacks": ["warn", 3],
      "max-params": ["warn", 4],
      "max-statements": ["warn", 20],

      // ----------------------------
      // 3) import 规范
      // ----------------------------
      "import/first": "error",
      "import/newline-after-import": ["warn", { count: 1 }],
      "import/no-absolute-path": "error",
      "import/no-cycle": "warn",
      "import/no-duplicates": "error",
      "import/no-mutable-exports": "error",
      "import/no-named-as-default": "warn",
      "import/no-unresolved": "error",
      "import/order": [
        "warn",
        {
          groups: [
            "builtin",
            "external",
            "internal",
            "parent",
            "sibling",
            "index",
            "object",
            "type"
          ],
          "newlines-between": "always",
          alphabetize: {
            order: "asc",
            caseInsensitive: true
          }
        }
      ],

      // ----------------------------
      // 4) Promise / async 最佳实践
      // ----------------------------
      "promise/always-return": "error",
      "promise/catch-or-return": "error",
      "promise/no-callback-in-promise": "warn",
      "promise/no-multiple-resolved": "error",
      "promise/no-nesting": "warn",
      "promise/no-new-statics": "error",
      "promise/no-promise-in-callback": "warn",
      "promise/no-return-in-finally": "error",
      "promise/no-return-wrap": "error",
      "promise/param-names": "error",
      "promise/prefer-await-to-then": "warn",
      "promise/valid-params": "error",

      // ----------------------------
      // 5) React / JSX 编码习惯
      // ----------------------------
      "react/prop-types": "off",
      "react/react-in-jsx-scope": "off",
      "react/jsx-no-target-blank": "error",
      "react/button-has-type": "warn",
      "react/self-closing-comp": "warn",
      "react/jsx-boolean-value": ["warn", "never"],
      "react/jsx-fragments": ["warn", "syntax"],
      "react/jsx-no-useless-fragment": "warn",
      "react/no-array-index-key": "warn",
      "react-hooks/exhaustive-deps": "warn",

      // ----------------------------
      // 6) 现代 JS 习惯（高价值精选）
      // ----------------------------
      "unicorn/consistent-function-scoping": "warn",
      "unicorn/error-message": "error",
      "unicorn/no-array-for-each": "warn",
      "unicorn/no-instanceof-array": "error",
      "unicorn/no-useless-undefined": "warn",
      "unicorn/prefer-add-event-listener": "warn",
      "unicorn/prefer-array-find": "warn",
      "unicorn/prefer-dom-node-append": "warn",
      "unicorn/prefer-modern-dom-apis": "warn",
      "unicorn/prefer-optional-catch-binding": "warn",
      "unicorn/prefer-string-replace-all": "warn",
      "unicorn/prefer-string-slice": "warn",
      "unicorn/prevent-abbreviations": "off",
      "unicorn/throw-new-error": "error"
    }
  },

  // 必须放最后，关闭和 Prettier 冲突的格式类规则
  eslintConfigPrettier
];