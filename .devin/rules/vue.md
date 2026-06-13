# Vue Coding Rules — Minimin Sync (Wails Desktop)

## File Size
- **Maximum 600 lines per file, including `<template>`, `<script>`, and `<style>`.**
- If a component exceeds this limit, decompose it:
  - Extract child components.
  - Move composables / utilities to `composables/` or `utils/`.

## Component Structure
Use this order inside `.vue` files:
1. `<script setup lang="ts">`
2. `<template>`
3. `<style scoped>` (only if Tailwind classes are insufficient)

## TypeScript
- Strict mode is on. No `any` without a documented reason.
- Define props with `defineProps<{}>()` and emits with `defineEmits<{}>()`.
- Prefer interfaces over types for object shapes.

## Composition API
- Use `<script setup>` and Composition API exclusively. No Options API.
- Extract reusable logic into `composables/` (e.g., `useServers()`, `useUpdates()`).
- Keep components focused: one primary responsibility per component.

## Templates
- Use `kebab-case` for custom components in templates.
- Always provide `key` attributes in `v-for`.
- Avoid complex expressions in templates; use computed properties.

## Styling
- TailwindCSS is the default. Use utility classes first.
- Custom scoped styles only for complex overrides or third-party component theming.
- Never use element selectors (e.g., `div { ... }`) in scoped styles.

## Wails-Specific
- Call Go methods via `window.go.main.App.*` (e.g., `window.go.main.App.GetServers()`).
- Subscribe to Wails events via `EventsOn` / `EventsOff` from `wailsjs/runtime`.
- Always unbind events in `onUnmounted` to prevent leaks.
- Server state lives in composables using `ref`/`reactive`; sync with Wails events.
- Handle loading and error states for every async Go call.

## Performance
- Lazy-load heavy components with `defineAsyncComponent`.
- Debounce rapid user input (search, resize handlers).
- Prefer `v-show` over `v-if` for toggling visibility when the DOM cost is high.

## Linting
- Run the project's lint command before pushing.
- Fix all TypeScript and Vue compiler warnings.
