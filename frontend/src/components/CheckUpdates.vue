<script setup lang="ts">
import { ref, onMounted } from "vue";
import { CheckUpdates, ApplyUpdates } from "../../wailsjs/go/main/App";
import {
  Loader2,
  ArrowLeft,
  Download,
  AlertTriangle,
  CheckCircle,
  ChevronDown,
  ChevronUp,
} from "@lucide/vue";
import { EventsOn } from "../../wailsjs/runtime";

interface ManifestFile {
  path: string;
  sha256: string;
  size: number;
}

const props = defineProps<{
  serverId: string;
}>();

const emit = defineEmits<{
  (e: "back"): void;
}>();

const loading = ref(true);
const error = ref("");
const missing = ref<ManifestFile[]>([]);
const outdated = ref<ManifestFile[]>([]);
const orphan = ref<string[]>([]);
const selected = ref<Set<string>>(new Set());

const collapsed = ref({ missing: false, outdated: false, orphan: true });

function toggleSection(section: "missing" | "outdated" | "orphan") {
  collapsed.value[section] = !collapsed.value[section];
}

const applying = ref(false);
const applyProgress = ref(0);
const applyTotal = ref(0);
const applyStatus = ref("");

onMounted(async () => {
  try {
    const result = await CheckUpdates(props.serverId);
    missing.value = result.missing || [];
    outdated.value = result.outdated || [];
    orphan.value = result.orphan || [];

    for (const f of missing.value) selected.value.add(f.path);
    for (const f of outdated.value) selected.value.add(f.path);
  } catch (e: any) {
    error.value = e?.toString?.() || "Failed to check updates";
  } finally {
    loading.value = false;
  }
});

EventsOn("applyUpdates:status", (msg: string) => {
  applyStatus.value = msg;
});

EventsOn("applyUpdates:progress", (d: number, t: number) => {
  applyProgress.value = d;
  applyTotal.value = t;
});

EventsOn("applyUpdates:done", () => {
  applying.value = false;
  applyStatus.value = "Done!";
  emit("back");
});

EventsOn("applyUpdates:error", (msg: string) => {
  applying.value = false;
  error.value = msg;
});

function toggle(path: string) {
  if (selected.value.has(path)) {
    selected.value.delete(path);
  } else {
    selected.value.add(path);
  }
}

function selectAll() {
  for (const f of missing.value) selected.value.add(f.path);
  for (const f of outdated.value) selected.value.add(f.path);
  for (const f of orphan.value) selected.value.add(f);
}

function deselectAll() {
  selected.value.clear();
}

async function apply() {
  const paths = Array.from(selected.value);
  if (paths.length === 0) return;
  applying.value = true;
  applyProgress.value = 0;
  applyStatus.value = "starting...";
  await ApplyUpdates(props.serverId, paths);
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-6">
      <button
        class="p-2 rounded-lg bg-neutral-800 hover:bg-neutral-700 transition-colors"
        @click="emit('back')"
      >
        <ArrowLeft class="w-4 h-4" />
      </button>
      <h2 class="text-xl font-bold">{{ serverId }}</h2>
    </div>

    <div v-if="loading" class="flex items-center gap-2 text-neutral-400">
      <Loader2 class="w-5 h-5 animate-spin" />
      Checking for updates...
    </div>

    <div
      v-else-if="error"
      class="p-4 rounded-lg bg-red-900/20 border border-red-800 text-red-300 text-sm"
    >
      <div class="flex items-center gap-2 mb-1 font-medium text-red-200">
        <AlertTriangle class="w-4 h-4" />
        <span>Could not check for updates</span>
      </div>
      <p class="text-red-300/80">{{ error }}</p>
    </div>

    <div v-else>
      <div class="flex items-center justify-between mb-4">
        <p class="text-sm text-neutral-400">
          {{ selected.size }} file(s) selected
        </p>
        <div class="flex gap-2">
          <button
            class="text-xs text-neutral-400 hover:text-white"
            @click="selectAll"
          >
            Select all
          </button>
          <button
            class="text-xs text-neutral-400 hover:text-white"
            @click="deselectAll"
          >
            Deselect all
          </button>
        </div>
      </div>

      <div
        v-if="
          missing.length === 0 && outdated.length === 0 && orphan.length === 0
        "
        class="text-center py-12 text-neutral-500"
      >
        <CheckCircle class="w-10 h-10 mx-auto mb-2" />
        <p>Everything is up to date</p>
      </div>

      <div v-else class="space-y-4 max-w-3xl">
        <div v-if="missing.length > 0">
          <button
            class="text-sm font-medium text-neutral-300 mb-2 flex items-center gap-1.5 w-full text-left hover:text-white transition-colors"
            @click="toggleSection('missing')"
          >
            <Download class="w-4 h-4 text-emerald-400" />
            Missing ({{ missing.length }})
            <component
              :is="collapsed.missing ? ChevronDown : ChevronUp"
              class="w-4 h-4 ml-auto text-neutral-500"
            />
          </button>
          <div v-if="!collapsed.missing" class="space-y-1">
            <label
              v-for="f in missing"
              :key="f.path"
              class="flex items-center gap-3 p-2 rounded-lg bg-neutral-800/50 hover:bg-neutral-800 cursor-pointer"
            >
              <input
                type="checkbox"
                :checked="selected.has(f.path)"
                class="rounded border-neutral-600 bg-neutral-700 text-primary"
                @change="toggle(f.path)"
              />
              <span class="flex-1 text-sm truncate">{{ f.path }}</span>
              <span class="text-xs text-neutral-500">{{
                formatSize(f.size)
              }}</span>
            </label>
          </div>
        </div>

        <div v-if="outdated.length > 0">
          <button
            class="text-sm font-medium text-neutral-300 mb-2 flex items-center gap-1.5 w-full text-left hover:text-white transition-colors"
            @click="toggleSection('outdated')"
          >
            <AlertTriangle class="w-4 h-4 text-amber-400" />
            Outdated ({{ outdated.length }})
            <component
              :is="collapsed.outdated ? ChevronDown : ChevronUp"
              class="w-4 h-4 ml-auto text-neutral-500"
            />
          </button>
          <div v-if="!collapsed.outdated" class="space-y-1">
            <label
              v-for="f in outdated"
              :key="f.path"
              class="flex items-center gap-3 p-2 rounded-lg bg-neutral-800/50 hover:bg-neutral-800 cursor-pointer"
            >
              <input
                type="checkbox"
                :checked="selected.has(f.path)"
                class="rounded border-neutral-600 bg-neutral-700 text-primary"
                @change="toggle(f.path)"
              />
              <span class="flex-1 text-sm truncate">{{ f.path }}</span>
              <span class="text-xs text-neutral-500">{{
                formatSize(f.size)
              }}</span>
            </label>
          </div>
        </div>

        <div v-if="orphan.length > 0">
          <button
            class="text-sm font-medium text-neutral-300 mb-2 flex items-center gap-1.5 w-full text-left hover:text-white transition-colors"
            @click="toggleSection('orphan')"
          >
            <AlertTriangle class="w-4 h-4 text-red-400" />
            Orphan ({{ orphan.length }})
            <component
              :is="collapsed.orphan ? ChevronDown : ChevronUp"
              class="w-4 h-4 ml-auto text-neutral-500"
            />
          </button>
          <div v-if="!collapsed.orphan" class="space-y-1">
            <label
              v-for="f in orphan"
              :key="f"
              class="flex items-center gap-3 p-2 rounded-lg bg-neutral-800/50 hover:bg-neutral-800 cursor-pointer"
            >
              <input
                type="checkbox"
                :checked="selected.has(f)"
                class="rounded border-neutral-600 bg-neutral-700 text-primary"
                @change="toggle(f)"
              />
              <span class="flex-1 text-sm truncate text-red-300">{{ f }}</span>
            </label>
          </div>
        </div>

        <button
          v-if="!applying && selected.size > 0"
          class="w-full py-3 rounded-xl bg-primary hover:bg-primary/90 text-white font-medium transition-colors mt-4"
          @click="apply"
        >
          Apply {{ selected.size }} update(s)
        </button>

        <div v-if="applying" class="space-y-2 mt-4">
          <div class="flex items-center gap-2 text-sm text-neutral-300">
            <Loader2 class="w-4 h-4 animate-spin" />
            <span class="capitalize">{{ applyStatus }}</span>
          </div>
          <div
            v-if="applyTotal > 0"
            class="w-full bg-neutral-800 rounded-full h-2"
          >
            <div
              class="bg-primary h-2 rounded-full transition-all"
              :style="{
                width: `${Math.min(100, (applyProgress / applyTotal) * 100)}%`,
              }"
            ></div>
          </div>
          <p v-if="applyTotal > 0" class="text-xs text-neutral-500 text-right">
            {{ Math.round(applyProgress / 1024 / 1024) }} /
            {{ Math.round(applyTotal / 1024 / 1024) }} MB
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
