<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from "vue";
import {
  CheckUpdates,
  ApplyUpdates,
  IsOperationRunning,
} from "../../wailsjs/go/main/App";
import {
  Loader2,
  Download,
  AlertTriangle,
  CheckCircle,
  Check,
  Plug,
  Search,
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

type TabKey = "missing" | "outdated" | "orphan";
const activeTab = ref<TabKey>("missing");

const tabs = computed(() => [
  {
    key: "missing" as TabKey,
    label: "Missing",
    count: missing.value.length,
    icon: Download,
    color: "text-emerald-400",
  },
  {
    key: "outdated" as TabKey,
    label: "Outdated",
    count: outdated.value.length,
    icon: AlertTriangle,
    color: "text-amber-400",
  },
  {
    key: "orphan" as TabKey,
    label: "Orphan",
    count: orphan.value.length,
    icon: AlertTriangle,
    color: "text-red-400",
  },
]);

const visibleTabs = computed(() => tabs.value.filter((t) => t.count > 0));

watch(
  visibleTabs,
  (tabs) => {
    if (tabs.length > 0 && !tabs.find((t) => t.key === activeTab.value)) {
      activeTab.value = tabs[0].key;
    }
  },
  { immediate: true },
);

const applying = ref(false);
const applyProgress = ref(0);
const applyTotal = ref(0);
const applyStatus = ref("");
const opRunning = ref(false);

const steps = [
  { label: "Connecting", icon: Plug },
  { label: "Fetching data", icon: Download },
  { label: "Scanning files", icon: Search },
  { label: "Scanning orphan files", icon: AlertTriangle },
  { label: "Done", icon: CheckCircle },
];

const stepMap: Record<string, number> = {
  connecting: 0,
  fetching_info: 1,
  fetching_manifest: 1,
  scanning_files: 2,
  scanning_orphan: 3,
  complete: 4,
};

const currentStep = ref(0);

let pollTimer: ReturnType<typeof setInterval>;

onMounted(async () => {
  try {
    const result = await CheckUpdates(props.serverId);
    missing.value = result.missing || [];
    outdated.value = result.outdated || [];
    orphan.value = result.orphan || [];

    for (const f of missing.value) selected.value.add(f.path);
    for (const f of outdated.value) selected.value.add(f.path);
    for (const f of orphan.value) selected.value.add(f);
  } catch (e: any) {
    error.value = e?.toString?.() || "Failed to check updates";
  } finally {
    loading.value = false;
  }

  pollTimer = setInterval(async () => {
    opRunning.value = await IsOperationRunning();
  }, 1000);
});

onUnmounted(() => {
  clearInterval(pollTimer);
});

if ((window as any).runtime) {
  EventsOn("checkUpdates:status", (status: string) => {
    if (status in stepMap) {
      currentStep.value = stepMap[status];
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
}

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
  if (await IsOperationRunning()) {
    error.value = "Another operation is already in progress";
    return;
  }
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

interface CategoryInfo {
  key: string;
  label: string;
  dir: string;
  order: number;
}

function getCategory(path: string): CategoryInfo {
  const p = path.toLowerCase().replace(/^\.minecraft\//, "minecraft/");
  if (p.includes("/mods/")) {
    return { key: "mods", label: "Mods", dir: "minecraft/mods", order: 0 };
  }
  if (p.includes("/resourcepacks/")) {
    return {
      key: "resourcepacks",
      label: "Resource Packs",
      dir: "minecraft/resourcepacks",
      order: 1,
    };
  }
  if (p.includes("/shaderpacks/")) {
    return {
      key: "shaderpacks",
      label: "Shader Packs",
      dir: "minecraft/shaderpacks",
      order: 2,
    };
  }
  return { key: "other", label: "Other", dir: "", order: 3 };
}

function basename(path: string): string {
  const idx = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return idx >= 0 ? path.slice(idx + 1) : path;
}

function installDir(path: string): string {
  const normalized = path.replace(/^\.minecraft\//, "minecraft/");
  const idx = normalized.lastIndexOf("/");
  return idx >= 0 ? normalized.slice(0, idx) : normalized;
}

const groupedMissing = computed(() => {
  const map = new Map<string, { info: CategoryInfo; files: ManifestFile[] }>();
  for (const f of missing.value) {
    const cat = getCategory(f.path);
    const g = map.get(cat.key);
    if (g) {
      g.files.push(f);
    } else {
      map.set(cat.key, { info: cat, files: [f] });
    }
  }
  return Array.from(map.values()).sort((a, b) => a.info.order - b.info.order);
});

const groupedOutdated = computed(() => {
  const map = new Map<string, { info: CategoryInfo; files: ManifestFile[] }>();
  for (const f of outdated.value) {
    const cat = getCategory(f.path);
    const g = map.get(cat.key);
    if (g) {
      g.files.push(f);
    } else {
      map.set(cat.key, { info: cat, files: [f] });
    }
  }
  return Array.from(map.values()).sort((a, b) => a.info.order - b.info.order);
});

const groupedOrphan = computed(() => {
  const map = new Map<string, { info: CategoryInfo; paths: string[] }>();
  for (const p of orphan.value) {
    const cat = getCategory(p);
    const g = map.get(cat.key);
    if (g) {
      g.paths.push(p);
    } else {
      map.set(cat.key, { info: cat, paths: [p] });
    }
  }
  return Array.from(map.values()).sort((a, b) => a.info.order - b.info.order);
});

type SubTabKey = "mods" | "resourcepacks" | "shaderpacks" | "other";
const activeSubTab = ref<SubTabKey>("mods");

const subTabs = [
  { key: "mods" as SubTabKey, label: "Mods" },
  { key: "resourcepacks" as SubTabKey, label: "Resource Packs" },
  { key: "shaderpacks" as SubTabKey, label: "Shader Packs" },
  { key: "other" as SubTabKey, label: "Other" },
];

const availableSubTabs = computed(() => {
  let keys: string[];
  if (activeTab.value === "missing") {
    keys = groupedMissing.value.map((g) => g.info.key);
  } else if (activeTab.value === "outdated") {
    keys = groupedOutdated.value.map((g) => g.info.key);
  } else {
    keys = groupedOrphan.value.map((g) => g.info.key);
  }
  const set = new Set(keys);
  return subTabs.filter((t) => set.has(t.key));
});

watch(
  availableSubTabs,
  (tabs) => {
    if (tabs.length > 0 && !tabs.find((t) => t.key === activeSubTab.value)) {
      activeSubTab.value = tabs[0].key;
    }
  },
  { immediate: true },
);

watch(activeTab, () => {
  const tabs = availableSubTabs.value;
  if (tabs.length > 0 && !tabs.find((t) => t.key === activeSubTab.value)) {
    activeSubTab.value = tabs[0].key;
  }
});
</script>

<template>
  <div class="h-full flex flex-col">
    <div
      v-if="loading"
      class="flex-1 flex flex-col items-center justify-center gap-6 text-neutral-400"
    >
      <div class="w-full max-w-xs space-y-3">
        <div
          v-for="(step, idx) in steps"
          :key="idx"
          class="flex items-center gap-3 transition-colors duration-300"
          :class="
            idx < currentStep
              ? 'text-neutral-400'
              : idx === currentStep
                ? 'text-white'
                : 'text-neutral-600'
          "
        >
          <div class="w-5 h-5 flex items-center justify-center">
            <Loader2
              v-if="idx === currentStep"
              class="w-4 h-4 animate-spin text-primary"
            />
            <CheckCircle
              v-else-if="idx < currentStep"
              class="w-4 h-4 text-emerald-400"
            />
            <component :is="step.icon" v-else class="w-4 h-4" />
          </div>
          <span class="text-sm">{{ step.label }}</span>
        </div>
      </div>
    </div>

    <div v-else-if="error" class="flex-1 flex items-center justify-center">
      <div class="text-center space-y-3 max-w-sm px-4">
        <div
          class="mx-auto w-12 h-12 rounded-full bg-red-900/30 flex items-center justify-center"
        >
          <AlertTriangle class="w-6 h-6 text-red-400" />
        </div>
        <div>
          <p class="text-red-200 font-medium">{{ error }}</p>
          <p class="text-neutral-500 text-sm mt-1">Please try again later.</p>
        </div>
      </div>
    </div>

    <div v-else class="flex-1 flex flex-col min-h-0">
      <div class="flex items-center justify-between mb-4 flex-shrink-0">
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
        class="flex-1 flex items-center justify-center text-center py-12 text-neutral-500"
      >
        <div>
          <CheckCircle class="w-10 h-10 mx-auto mb-2" />
          <p>Everything is up to date</p>
        </div>
      </div>

      <div v-else class="flex flex-col flex-1 min-h-0">
        <div class="flex gap-1 mb-4 p-1 bg-[#262626] rounded-lg shrink-0">
          <button
            v-for="tab in visibleTabs"
            :key="tab.key"
            class="flex-1 py-1.5 rounded-md text-sm font-medium transition-colors flex items-center justify-center gap-1.5"
            :class="
              activeTab === tab.key
                ? 'bg-primary text-white'
                : 'text-neutral-400 hover:text-white'
            "
            @click="activeTab = tab.key"
          >
            <component :is="tab.icon" class="w-4 h-4" :class="tab.color" />
            {{ tab.label }} ({{ tab.count }})
          </button>
        </div>

        <div class="flex gap-1 mb-3 p-1 bg-[#1a1a1a] rounded-lg shrink-0">
          <button
            v-for="sub in availableSubTabs"
            :key="sub.key"
            class="flex-1 py-1.5 rounded-md text-xs font-medium transition-colors"
            :class="
              activeSubTab === sub.key
                ? 'bg-[#333] text-white'
                : 'text-neutral-500 hover:text-neutral-300'
            "
            @click="activeSubTab = sub.key"
          >
            {{ sub.label }}
          </button>
        </div>

        <div class="flex-1 min-h-0 overflow-auto">
          <div v-if="activeTab === 'missing'" class="space-y-1">
            <div v-for="group in groupedMissing" :key="group.info.key">
              <div v-if="group.info.key === activeSubTab" class="space-y-1">
                <label
                  v-for="f in group.files"
                  :key="f.path"
                  class="flex items-center gap-3 p-2 rounded-lg bg-[#262626]/50 hover:bg-[#262626] cursor-pointer"
                  @click="toggle(f.path)"
                >
                  <div
                    class="w-5 h-5 rounded border flex items-center justify-center transition-colors flex-shrink-0"
                    :class="
                      selected.has(f.path)
                        ? 'bg-primary border-primary'
                        : 'border-neutral-600'
                    "
                  >
                    <Check
                      v-if="selected.has(f.path)"
                      class="w-3.5 h-3.5 text-white"
                    />
                  </div>
                  <div class="flex-1 min-w-0">
                    <div class="text-sm truncate">{{ basename(f.path) }}</div>
                    <div class="text-xs text-neutral-500 truncate">
                      {{ installDir(f.path) }}
                    </div>
                  </div>
                  <span class="text-xs text-neutral-500">{{
                    formatSize(f.size)
                  }}</span>
                </label>
              </div>
            </div>
          </div>

          <div v-if="activeTab === 'outdated'" class="space-y-1">
            <div v-for="group in groupedOutdated" :key="group.info.key">
              <div v-if="group.info.key === activeSubTab" class="space-y-1">
                <label
                  v-for="f in group.files"
                  :key="f.path"
                  class="flex items-center gap-3 p-2 rounded-lg bg-[#262626]/50 hover:bg-[#262626] cursor-pointer"
                  @click="toggle(f.path)"
                >
                  <div
                    class="w-5 h-5 rounded border flex items-center justify-center transition-colors flex-shrink-0"
                    :class="
                      selected.has(f.path)
                        ? 'bg-primary border-primary'
                        : 'border-neutral-600'
                    "
                  >
                    <Check
                      v-if="selected.has(f.path)"
                      class="w-3.5 h-3.5 text-white"
                    />
                  </div>
                  <div class="flex-1 min-w-0">
                    <div class="text-sm truncate">{{ basename(f.path) }}</div>
                    <div class="text-xs text-neutral-500 truncate">
                      {{ installDir(f.path) }}
                    </div>
                  </div>
                  <span class="text-xs text-neutral-500">{{
                    formatSize(f.size)
                  }}</span>
                </label>
              </div>
            </div>
          </div>

          <div v-if="activeTab === 'orphan'" class="space-y-1">
            <div v-for="group in groupedOrphan" :key="group.info.key">
              <div v-if="group.info.key === activeSubTab" class="space-y-1">
                <label
                  v-for="p in group.paths"
                  :key="p"
                  class="flex items-center gap-3 p-2 rounded-lg bg-[#262626]/50 hover:bg-[#262626] cursor-pointer"
                  @click="toggle(p)"
                >
                  <div
                    class="w-5 h-5 rounded border flex items-center justify-center transition-colors flex-shrink-0"
                    :class="
                      selected.has(p)
                        ? 'bg-primary border-primary'
                        : 'border-neutral-600'
                    "
                  >
                    <Check
                      v-if="selected.has(p)"
                      class="w-3.5 h-3.5 text-white"
                    />
                  </div>
                  <div class="flex-1 min-w-0">
                    <div class="text-sm truncate text-red-300">
                      {{ basename(p) }}
                    </div>
                    <div class="text-xs text-neutral-500 truncate">
                      {{ installDir(p) }}
                    </div>
                  </div>
                </label>
              </div>
            </div>
          </div>
        </div>

        <div class="flex-shrink-0 pt-4 w-full">
          <button
            v-if="!applying && !opRunning && selected.size > 0"
            class="w-full py-3 rounded-xl bg-primary hover:bg-primary/90 text-white font-medium transition-colors"
            @click="apply"
          >
            Apply {{ selected.size }} update(s)
          </button>

          <div
            v-else-if="opRunning"
            class="text-center text-sm text-neutral-500 py-3"
          >
            Another operation is in progress...
          </div>

          <div v-if="applying" class="space-y-2">
            <div class="flex items-center gap-2 text-sm text-neutral-300">
              <Loader2 class="w-4 h-4 animate-spin" />
              <span class="capitalize">{{ applyStatus }}</span>
            </div>
            <div
              v-if="applyTotal > 0"
              class="w-full bg-[#262626] rounded-full h-2"
            >
              <div
                class="bg-primary h-2 rounded-full transition-all"
                :style="{
                  width: `${Math.min(100, (applyProgress / applyTotal) * 100)}%`,
                }"
              ></div>
            </div>
            <p
              v-if="applyTotal > 0"
              class="text-xs text-neutral-500 text-right"
            >
              {{ Math.round(applyProgress / 1024 / 1024) }} /
              {{ Math.round(applyTotal / 1024 / 1024) }} MB
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
