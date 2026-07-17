module.exports = {
  root: true,
  env: { browser: true, es2022: true, node: true },
  parser: 'vue-eslint-parser',
  parserOptions: {
    parser: '@typescript-eslint/parser',
    ecmaVersion: 'latest',
    sourceType: 'module',
  },
  extends: [
    'eslint:recommended',
    'plugin:vue/vue3-recommended',
    'plugin:@typescript-eslint/recommended',
    'prettier',
  ],
  plugins: ['@typescript-eslint'],
  rules: {
    'vue/multi-word-component-names': 'off',
    'vue/no-v-html': 'off',
    '@typescript-eslint/no-explicit-any': 'off',
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    '@typescript-eslint/no-non-null-assertion': 'off',
    // Vite 自动生成的 *.vue 类型垫片使用 DefineComponent<{}, {}, any>，属公认误报，关闭此规则
    '@typescript-eslint/ban-types': 'off',
    // 流式读取使用 while (true) 属合理写法，仅对 if/三元做常量检查
    'no-constant-condition': ['error', { checkLoops: false }],
  },
  ignorePatterns: ['dist', 'node_modules', '.eslintrc.cjs'],
}
