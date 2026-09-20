const js = require('@eslint/js');
const globals = require('globals');

module.exports = [
  { ignores: ['static/htmx.min.js', 'node_modules/'] },
  js.configs.recommended,
  {
    files: ['static/**/*.js'],
    languageOptions: {
      sourceType: 'script',
      globals: {
        ...globals.browser,
        htmx: 'readonly',
      },
    },
    rules: {
      // Top-level functions/vars are routinely called from inline
      // onclick="" (etc.) attributes in templates, invisible to ESLint —
      // only flag genuinely unused *local* declarations.
      'no-unused-vars': ['warn', { vars: 'local', argsIgnorePattern: '^_' }],
    },
  },
  {
    // gallery.js relies on openLightbox() from lightbox.js — both are
    // loaded as sibling <script> tags on the same page and share globals.
    files: ['static/gallery.js'],
    languageOptions: {
      globals: { openLightbox: 'readonly' },
    },
  },
];
