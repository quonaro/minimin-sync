<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import {
  GetConfig,
  DiscoverAllLaunchers,
  SaveConfig,
  GetServers,
  SelectInstancesDir,
} from "../wailsjs/go/main/App";
import { config } from "../wailsjs/go/models";
import { Settings, Loader2 } from "@lucide/vue";
import ServerList from "./components/ServerList.vue";
import AddServer from "./components/AddServer.vue";
import CheckUpdates from "./components/CheckUpdates.vue";

type View = "setup" | "list" | "add" | "check";

const currentView = ref<View>("setup");
const instancesDir = ref("");
const servers = ref<any[]>([]);
const selectedServer = ref("");
const detectedLaunchers = ref<string[]>([]);
const scanning = ref(false);

onMounted(async () => {
  const cfg = await GetConfig();
  if (cfg.instancesDir) {
    instancesDir.value = cfg.instancesDir;
    currentView.value = "list";
    await loadServers();
  }
});

async function loadServers() {
  try {
    servers.value = await GetServers();
  } catch {
    servers.value = [];
  }
}

async function scanLaunchers() {
  scanning.value = true;
  detectedLaunchers.value = await DiscoverAllLaunchers();
  scanning.value = false;
}

async function selectLauncher(dir: string) {
  instancesDir.value = dir;
  const existing = await GetConfig();
  await SaveConfig(
    new config.Config({
      instancesDir: dir,
      servers: existing.servers,
    }),
  );
  currentView.value = "list";
  await loadServers();
}

async function browseDir() {
  const dir = await SelectInstancesDir();
  if (dir) {
    instancesDir.value = dir;
  }
}

async function saveDir() {
  if (!instancesDir.value) return;
  const existing = await GetConfig();
  await SaveConfig(
    new config.Config({
      instancesDir: instancesDir.value,
      servers: existing.servers,
    }),
  );
  currentView.value = "list";
  await loadServers();
}

function goAdd() {
  currentView.value = "add";
}

function goList() {
  currentView.value = "list";
  loadServers();
}

function goCheck(serverId: string) {
  selectedServer.value = serverId;
  currentView.value = "check";
}

async function goSetup() {
  currentView.value = "setup";
  const cfg = await GetConfig();
  instancesDir.value = cfg.instancesDir || "";
  await scanLaunchers();
}

function isSelected(dir: string): boolean {
  return instancesDir.value === dir;
}

function launcherName(dir: string): string {
  const parts = dir.split("/");
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i].toLowerCase();
    if (p.includes("elyprismlauncher")) return "ElyPrism Launcher";
    if (p.includes("prismlauncher")) return "Prism Launcher";
    if (p.includes("multimc")) return "MultiMC";
  }
  return "Unknown Launcher";
}

const pageTitle = computed(() => {
  switch (currentView.value) {
    case "setup":
      return "Setup";
    case "list":
      return "Servers";
    case "add":
      return "Add Server";
    case "check":
      return "Check Updates";
    default:
      return "";
  }
});
</script>

<template>
  <div class="h-screen w-screen bg-neutral-900 text-neutral-100 flex flex-col">
    <header
      class="px-6 py-4 border-b border-neutral-700 flex items-center relative"
    >
      <div class="flex items-center gap-1.5 w-1/3">
        <img src="/img/MiniMin_L.avif" alt="MiniMin" class="h-7 w-auto" />
        <img src="/img/MiniMin_T_light.avif" alt="MiniMin" class="h-6 w-auto" />
      </div>

      <div class="flex-1 text-center">
        <span class="text-sm font-medium text-neutral-300">{{
          pageTitle
        }}</span>
      </div>

      <div class="flex gap-3 w-1/3 justify-end">
        <button
          v-if="currentView !== 'list'"
          class="px-3 py-1.5 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-sm transition-colors"
          @click="goList"
        >
          Servers
        </button>
        <button
          v-if="currentView !== 'add'"
          class="px-3 py-1.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors"
          @click="goAdd"
        >
          Add Server
        </button>
        <button
          v-if="currentView !== 'setup'"
          class="p-2 rounded-lg bg-neutral-700 hover:bg-neutral-600 transition-colors"
          @click="goSetup"
        >
          <Settings class="w-4 h-4" />
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-auto p-6 flex flex-col">
      <div v-if="currentView === 'setup'" class="max-w-md mx-auto mt-8">
        <h2 class="text-xl font-bold mb-1 text-center">Select Launcher</h2>
        <p class="text-neutral-400 text-center text-sm mb-6">
          Choose your Prism Launcher instances directory
        </p>

        <div
          v-if="scanning"
          class="flex items-center justify-center gap-2 text-neutral-400 text-sm py-8"
        >
          <Loader2 class="w-4 h-4 animate-spin" />
          Scanning...
        </div>

        <div v-else-if="detectedLaunchers.length > 0" class="space-y-2 mb-6">
          <div
            v-for="dir in detectedLaunchers"
            :key="dir"
            class="flex items-center justify-between p-3 rounded-lg border transition-colors"
            :class="
              isSelected(dir)
                ? 'bg-primary/10 border-primary'
                : 'bg-neutral-800 border-neutral-700'
            "
          >
            <div class="min-w-0">
              <p
                class="text-sm font-medium"
                :class="isSelected(dir) ? 'text-primary' : 'text-white'"
              >
                {{ launcherName(dir) }}
              </p>
              <p class="text-xs text-neutral-500 truncate">{{ dir }}</p>
            </div>
            <button
              v-if="!isSelected(dir)"
              class="ml-3 px-3 py-1 rounded-md bg-primary hover:bg-primary/90 text-white text-xs font-medium transition-colors flex-shrink-0"
              @click="selectLauncher(dir)"
            >
              Select
            </button>
            <span
              v-else
              class="ml-3 px-3 py-1 rounded-md bg-primary/20 text-primary text-xs font-medium flex-shrink-0"
            >
              Selected
            </span>
          </div>
        </div>

        <div v-else class="text-center py-8 text-neutral-500 text-sm">
          No launchers found
        </div>

        <div class="relative mb-4">
          <div class="absolute inset-0 flex items-center">
            <div class="w-full border-t border-neutral-700"></div>
          </div>
          <div class="relative flex justify-center text-sm">
            <span class="bg-neutral-900 px-2 text-neutral-500"
              >or enter manually</span
            >
          </div>
        </div>

        <div class="flex gap-2 mb-4">
          <input
            v-model="instancesDir"
            type="text"
            placeholder="/path/to/instances"
            class="flex-1 px-4 py-2.5 rounded-lg bg-neutral-800 border border-neutral-700 text-sm focus:outline-none focus:border-primary"
          />
          <button
            class="px-4 py-2.5 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-sm font-medium transition-colors"
            @click="browseDir"
          >
            Browse
          </button>
          <button
            class="px-4 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-sm font-medium transition-colors"
            @click="saveDir"
          >
            Save
          </button>
        </div>

        <button
          class="w-full py-2 rounded-lg text-sm text-neutral-400 hover:text-white transition-colors"
          @click="scanLaunchers"
        >
          Rescan
        </button>
      </div>

      <ServerList
        v-else-if="currentView === 'list'"
        :servers="servers"
        @check="goCheck"
        @add="goAdd"
      />
      <AddServer v-else-if="currentView === 'add'" @done="goList" />
      <CheckUpdates
        v-else-if="currentView === 'check'"
        :server-id="selectedServer"
        @back="goList"
      />
    </main>
  </div>
</template>
