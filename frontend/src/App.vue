<script setup lang="ts">
import { ref, onMounted, computed, onErrorCaptured } from "vue";
import {
  GetConfig,
  DiscoverAllLaunchers,
  SaveConfig,
  GetServers,
  SelectInstancesDir,
  RemoveServer,
  CheckUpdates as CheckUpdatesGo,
  RunServer,
  UpdateServerURL,
  RefreshServerInfo,
  OpenInstanceDir,
  CheckForUpdate,
  DownloadUpdate,
  RestartApp,
  CancelUpdate,
  GetVersion,
  HasPendingUpdate,
} from "../wailsjs/go/main/App";
import { config } from "../wailsjs/go/models";
import { Settings, Loader2, ArrowLeft } from "@lucide/vue";
import { EventsOn } from "../wailsjs/runtime";
import ServerList from "./components/ServerList.vue";
import AddServer from "./components/AddServer.vue";
import CheckUpdates from "./components/CheckUpdates.vue";

type View = "setup" | "list" | "add" | "check";

const currentView = ref<View>("setup");
const instancesDir = ref("");
const servers = ref<any[]>([]);
const selectedServer = ref("");
const detectedLaunchers = ref<string[]>([]);
const selectedLauncher = ref<string>("");
const scanning = ref(false);
const appError = ref<string>("");
const deleteConfirm = ref(false);
const deleteTarget = ref("");
const pendingUpdates = ref<Record<string, number>>({});
const checkErrors = ref<Record<string, string>>({});
const autoCheckRunning = ref(false);
const editModal = ref(false);
const editTarget = ref("");
const editUrl = ref("");
const editError = ref("");
const editLoading = ref(false);
const autoCheckInterval = ref(5);
const updateInfo = ref<Record<string, any> | null>(null);
const updateChecking = ref(false);
const updateError = ref("");
const updateDownloading = ref(false);
const updateProgress = ref(0);
const updateTotal = ref(0);
const restartModal = ref(false);
const selfUpdateModal = ref(false);
const setupTab = ref<"launcher" | "general">("launcher");
const appVersion = ref("");
const versionToast = ref(false);

if ((window as any).runtime) {
  EventsOn("servers:reload", () => {
    loadServers();
  });

  EventsOn("applyUpdates:done", (serverID: string) => {
    delete pendingUpdates.value[serverID];
  });

  EventsOn("updateSelf:progress", (d: number, t: number) => {
    updateProgress.value = d;
    updateTotal.value = t;
  });

  EventsOn("updateSelf:done", () => {
    updateDownloading.value = false;
    restartModal.value = true;
  });

  EventsOn("selfUpdate:available", (info: any) => {
    updateInfo.value = info;
    selfUpdateModal.value = true;
  });

  EventsOn("checkUpdates:result", (data: any) => {
    const total = (data.missingCount || 0) + (data.outdatedCount || 0);
    if (total > 0) {
      pendingUpdates.value[data.serverID] = total;
    } else {
      delete pendingUpdates.value[data.serverID];
    }
  });

  EventsOn("checkUpdates:error", (data: any) => {
    checkErrors.value[data.serverID] = data.error;
    delete pendingUpdates.value[data.serverID];
  });

  EventsOn("checkUpdates:ok", (data: any) => {
    delete checkErrors.value[data.serverID];
  });

  EventsOn("autoCheck:start", () => {
    autoCheckRunning.value = true;
  });

  EventsOn("autoCheck:done", () => {
    autoCheckRunning.value = false;
  });
}

onErrorCaptured((err) => {
  appError.value = String(err);
  console.error(err);
  return false;
});

onMounted(async () => {
  try {
    appVersion.value = await GetVersion();
  } catch {}
  try {
    if (await HasPendingUpdate()) {
      restartModal.value = true;
    }
  } catch {}

  const cfg = await GetConfig();
  autoCheckInterval.value = cfg.autoCheckIntervalMinutes || 5;
  if (cfg.instancesDir) {
    instancesDir.value = cfg.instancesDir;
    currentView.value = "list";
    await loadServers();
    for (const s of servers.value) {
      RefreshServerInfo(s.Name)
        .then(() => loadServers())
        .catch(() => {});
    }
  }
});

async function loadServers() {
  try {
    const result = await GetServers();
    servers.value = result ?? [];
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
  selectedLauncher.value = launcherType(dir);
  const existing = await GetConfig();
  await SaveConfig(
    new config.Config({
      instancesDir: dir,
      launcher: selectedLauncher.value,
      autoCheckIntervalMinutes: autoCheckInterval.value,
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
  selectedLauncher.value = launcherType(instancesDir.value);
  const existing = await GetConfig();
  await SaveConfig(
    new config.Config({
      instancesDir: instancesDir.value,
      launcher: selectedLauncher.value,
      autoCheckIntervalMinutes: autoCheckInterval.value,
    }),
  );
  currentView.value = "list";
  await loadServers();
}

function goAdd() {
  currentView.value = "add";
}

function openDeleteConfirm(serverId: string) {
  deleteTarget.value = serverId;
  deleteConfirm.value = true;
}

async function confirmDelete() {
  deleteConfirm.value = false;
  if (deleteTarget.value) {
    try {
      await RemoveServer(deleteTarget.value);
      await loadServers();
    } catch (e: any) {
      appError.value = e?.toString?.() || "Failed to delete server";
    }
  }
  deleteTarget.value = "";
}

function cancelDelete() {
  deleteConfirm.value = false;
  deleteTarget.value = "";
}

async function handleRun(serverId: string) {
  try {
    await RunServer(serverId);
  } catch (e: any) {
    appError.value = e?.toString?.() || "Failed to start server";
  }
}

async function handleOpenDir(serverId: string) {
  try {
    await OpenInstanceDir(serverId);
  } catch (e: any) {
    appError.value = e?.toString?.() || "Failed to open folder";
  }
}

function openEdit(serverId: string) {
  editTarget.value = serverId;
  editUrl.value = "";
  editError.value = "";
  editLoading.value = false;
  editModal.value = true;
}

async function confirmEdit() {
  if (!editUrl.value) return;
  editLoading.value = true;
  editError.value = "";
  try {
    await UpdateServerURL(editTarget.value, editUrl.value);
    editModal.value = false;
    await loadServers();
  } catch (e: any) {
    editError.value = e?.toString?.() || "Failed to update link";
  } finally {
    editLoading.value = false;
  }
}

function cancelEdit() {
  editModal.value = false;
  editTarget.value = "";
  editUrl.value = "";
  editError.value = "";
  editLoading.value = false;
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
  selectedLauncher.value = cfg.launcher || "";
  autoCheckInterval.value = cfg.autoCheckIntervalMinutes || 5;
  await scanLaunchers();
}

function isSelected(dir: string): boolean {
  return instancesDir.value === dir;
}

function launcherName(dir: string): string {
  const parts = dir.split(/[/\\]/);
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i].toLowerCase();
    if (p.includes("elyprismlauncher")) return "ElyPrism Launcher";
    if (p.includes("prismlauncher")) return "Prism Launcher";
    if (p.includes("multimc")) return "MultiMC";
  }
  return "Unknown Launcher";
}

function launcherType(dir: string): string {
  const parts = dir.split(/[/\\]/);
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i].toLowerCase();
    if (p.includes("elyprismlauncher")) return "elyprismlauncher";
    if (p.includes("prismlauncher")) return "prismlauncher";
    if (p.includes("multimc")) return "multimc";
  }
  return "";
}

let logoClickCount = 0;
let logoClickTimer: ReturnType<typeof setTimeout> | null = null;

function handleLogoClick() {
  logoClickCount++;
  if (logoClickCount === 1) {
    logoClickTimer = setTimeout(() => {
      logoClickCount = 0;
    }, 500);
  }
  if (logoClickCount >= 3) {
    logoClickCount = 0;
    if (logoClickTimer) clearTimeout(logoClickTimer);
    versionToast.value = true;
    setTimeout(() => {
      versionToast.value = false;
    }, 2000);
  }
}

async function checkUpdate() {
  updateChecking.value = true;
  updateError.value = "";
  try {
    updateInfo.value = await CheckForUpdate();
  } catch (e: any) {
    updateError.value = e?.toString?.() || "Failed to check for updates";
    updateInfo.value = null;
  } finally {
    updateChecking.value = false;
  }
}

async function doUpdate() {
  updateError.value = "";
  updateDownloading.value = true;
  updateProgress.value = 0;
  updateTotal.value = 0;
  try {
    await DownloadUpdate();
  } catch (e: any) {
    updateError.value = e?.toString?.() || "Update failed";
    updateDownloading.value = false;
  }
}

async function confirmRestart() {
  try {
    await RestartApp();
  } catch (e: any) {
    updateError.value = e?.toString?.() || "Restart failed";
  }
}

async function cancelRestart() {
  restartModal.value = false;
}

async function handleManualCheck() {
  if (autoCheckRunning.value) return;
  try {
    const fn = (window as any).go?.main?.App?.RunManualCheck;
    if (fn) {
      await fn();
    }
  } catch (e: any) {
    console.error("Manual check failed:", e);
  }
}

function acceptSelfUpdate() {
  selfUpdateModal.value = false;
  doUpdate();
}

function dismissSelfUpdate() {
  selfUpdateModal.value = false;
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
      return selectedServer.value;
    default:
      return "";
  }
});
</script>

<template>
  <div class="h-screen w-screen bg-[#0F0F10] text-neutral-100 flex flex-col">
    <div
      v-if="appError"
      class="absolute inset-0 z-50 bg-black/90 flex flex-col items-center justify-center p-8"
    >
      <p class="text-red-400 font-bold mb-2">Runtime Error</p>
      <pre class="text-red-300 text-xs whitespace-pre-wrap max-w-full">{{
        appError
      }}</pre>
      <button
        class="mt-4 px-4 py-2 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm"
        @click="appError = ''"
      >
        Dismiss
      </button>
    </div>

    <header
      class="px-6 py-4 border-b border-neutral-700 flex items-center relative"
    >
      <div class="flex items-center gap-1.5 w-1/3 relative">
        <div class="relative" @click="handleLogoClick">
          <div class="flex items-center gap-1.5 cursor-default select-none">
            <img src="/img/MiniMin_L.avif" alt="MiniMin" class="h-7 w-auto" />
            <img
              src="/img/MiniMin_T_light.avif"
              alt="MiniMin"
              class="h-6 w-auto"
            />
          </div>
          <div
            v-if="versionToast"
            class="absolute top-full left-0 mt-2 px-3 py-1.5 rounded-lg bg-[#262626] border border-neutral-700 text-xs text-neutral-300 whitespace-nowrap z-50"
          >
            {{ appVersion }}
          </div>
        </div>
      </div>

      <div class="flex-1 text-center flex items-center justify-center gap-2">
        <span class="text-sm font-medium text-neutral-300">{{
          pageTitle
        }}</span>
        <Loader2
          v-if="currentView === 'list'"
          class="w-4 h-4 text-primary cursor-pointer transition-all"
          :class="
            autoCheckRunning
              ? 'animate-spin'
              : 'opacity-50 hover:opacity-100 hover:scale-110'
          "
          @click="handleManualCheck"
        />
      </div>

      <div class="flex items-center gap-3 w-1/3 justify-end">
        <button
          v-if="
            currentView === 'setup' ||
            currentView === 'add' ||
            currentView === 'check'
          "
          class="px-3 py-1.5 rounded-lg bg-red-600 hover:bg-red-500 text-white text-sm font-medium transition-colors flex items-center gap-1.5"
          @click="goList"
        >
          <ArrowLeft class="w-4 h-4" />
          Back
        </button>
        <button
          v-else
          class="p-2 rounded-lg bg-[#262626] hover:bg-[#262626] transition-colors group"
          @click="goSetup"
        >
          <Settings
            class="w-4 h-4 transition-transform duration-200 group-hover:scale-110 group-hover:rotate-90"
          />
        </button>
      </div>
    </header>

    <main class="flex-1 p-6 flex flex-col">
      <div
        v-if="currentView === 'setup'"
        class="max-w-md mx-auto w-full flex flex-col h-full"
      >
        <div class="flex gap-1 mb-4 p-1 bg-[#262626] rounded-lg shrink-0">
          <button
            class="flex-1 py-1.5 rounded-md text-sm font-medium transition-colors"
            :class="
              setupTab === 'launcher'
                ? 'bg-primary text-white'
                : 'text-neutral-400 hover:text-white'
            "
            @click="setupTab = 'launcher'"
          >
            Launcher
          </button>
          <button
            class="flex-1 py-1.5 rounded-md text-sm font-medium transition-colors"
            :class="
              setupTab === 'general'
                ? 'bg-primary text-white'
                : 'text-neutral-400 hover:text-white'
            "
            @click="setupTab = 'general'"
          >
            General
          </button>
        </div>

        <div class="flex-1 flex flex-col min-h-0">
          <div v-if="setupTab === 'launcher'" class="flex flex-col gap-4">
            <div
              v-if="scanning"
              class="flex items-center justify-center gap-2 text-neutral-400 text-sm py-8"
            >
              <Loader2 class="w-4 h-4 animate-spin" />
              Scanning...
            </div>

            <div v-else-if="detectedLaunchers.length > 0" class="space-y-2">
              <div
                v-for="dir in detectedLaunchers"
                :key="dir"
                class="flex items-center justify-between p-3 rounded-lg border transition-colors"
                :class="
                  isSelected(dir)
                    ? 'bg-primary/10 border-primary'
                    : 'bg-[#262626] border-neutral-700'
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

            <div class="relative">
              <div class="absolute inset-0 flex items-center">
                <div class="w-full border-t border-neutral-700"></div>
              </div>
              <div class="relative flex justify-center text-sm">
                <span class="bg-[#0F0F10] px-2 text-neutral-500"
                  >or enter manually</span
                >
              </div>
            </div>

            <div class="flex gap-2">
              <input
                v-model="instancesDir"
                type="text"
                placeholder="/path/to/instances"
                class="flex-1 px-4 py-2.5 rounded-lg bg-[#262626] border border-neutral-700 text-sm focus:outline-none focus:border-primary"
              />
              <button
                class="px-4 py-2.5 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm font-medium transition-colors"
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

          <div v-else class="flex flex-col gap-4">
            <div>
              <label class="block text-sm text-neutral-400 mb-1">
                Check interval (minutes, 0 = disabled)
              </label>
              <input
                v-model.number="autoCheckInterval"
                type="number"
                min="0"
                class="w-full px-4 py-2.5 rounded-lg bg-[#262626] border border-neutral-700 text-sm focus:outline-none focus:border-primary"
              />
            </div>

            <div class="space-y-3">
              <div
                v-if="updateChecking"
                class="flex items-center gap-2 text-sm text-neutral-400 py-2"
              >
                <Loader2 class="w-4 h-4 animate-spin" />
                Checking for updates...
              </div>
              <div
                v-else-if="updateError"
                class="p-3 rounded-lg bg-red-900/20 border border-red-800 text-red-300 text-sm"
              >
                {{ updateError }}
              </div>
              <div v-else-if="updateInfo" class="space-y-2">
                <p class="text-sm text-neutral-300">
                  Current:
                  <span class="font-mono text-xs">{{
                    updateInfo.current
                  }}</span>
                </p>
                <p
                  v-if="updateInfo.available"
                  class="text-sm text-emerald-400 font-medium"
                >
                  New version available: {{ updateInfo.version }}
                </p>
                <p v-else class="text-sm text-neutral-500">
                  Up to date ({{ updateInfo.version }})
                </p>
              </div>

              <div v-if="updateDownloading" class="space-y-2">
                <div class="flex items-center gap-2 text-sm text-neutral-300">
                  <Loader2 class="w-4 h-4 animate-spin" />
                  <span>Downloading update...</span>
                </div>
                <div class="w-full bg-[#262626] rounded-full h-2">
                  <div
                    class="bg-primary h-2 rounded-full transition-all"
                    :style="{
                      width: `${updateTotal > 0 ? Math.min(100, (updateProgress / updateTotal) * 100) : 0}%`,
                    }"
                  ></div>
                </div>
                <p class="text-xs text-neutral-500 text-right">
                  {{ Math.round(updateProgress / 1024 / 1024) }} /
                  {{ Math.round(updateTotal / 1024 / 1024) }} MB
                </p>
              </div>

              <button
                v-if="
                  !updateChecking &&
                  !updateDownloading &&
                  (!updateInfo || !updateInfo.available)
                "
                class="w-full py-2 rounded-lg bg-[#262626] hover:bg-[#262626] border border-neutral-700 text-sm text-neutral-300 transition-colors"
                @click="checkUpdate"
              >
                Check for Update
              </button>
              <button
                v-if="updateInfo?.available && !updateDownloading"
                class="w-full py-2 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors"
                @click="doUpdate"
              >
                Update Now
              </button>
            </div>
          </div>
        </div>
      </div>

      <div
        v-else-if="currentView === 'list'"
        class="flex-1 flex flex-col overflow-auto"
      >
        <ServerList
          :servers="servers"
          :pending-updates="pendingUpdates"
          :check-errors="checkErrors"
          @run="handleRun"
          @check="goCheck"
          @add="goAdd"
          @delete="openDeleteConfirm"
          @edit="openEdit"
          @open-dir="handleOpenDir"
        />
      </div>
      <AddServer v-else-if="currentView === 'add'" @done="goList" />
      <CheckUpdates
        v-else-if="currentView === 'check'"
        class="flex-1 flex flex-col"
        :server-id="selectedServer"
        @back="goList"
      />
      <div
        v-if="deleteConfirm"
        class="absolute inset-0 z-40 bg-black/70 flex items-center justify-center p-6"
      >
        <div
          class="max-w-sm w-full p-6 rounded-xl bg-[#262626] border border-neutral-700"
        >
          <h3 class="text-lg font-bold mb-2">Delete Server</h3>
          <p class="text-neutral-400 text-sm mb-6">
            Are you sure you want to delete
            <span class="text-white font-medium">{{ deleteTarget }}</span
            >? This cannot be undone.
          </p>
          <div class="flex gap-3">
            <button
              class="flex-1 py-2.5 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm font-medium transition-colors"
              @click="cancelDelete"
            >
              Cancel
            </button>
            <button
              class="flex-1 py-2.5 rounded-lg bg-red-600 hover:bg-red-500 text-white text-sm font-medium transition-colors"
              @click="confirmDelete"
            >
              Delete
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="editModal"
        class="absolute inset-0 z-40 bg-black/70 flex items-center justify-center p-6"
      >
        <div
          class="max-w-sm w-full p-6 rounded-xl bg-[#262626] border border-neutral-700"
        >
          <h3 class="text-lg font-bold mb-2">Edit Server Link</h3>
          <p class="text-neutral-400 text-sm mb-4">
            Update the archive link for
            <span class="text-white font-medium">{{ editTarget }}</span>
          </p>
          <input
            v-model="editUrl"
            type="text"
            placeholder="https://host/api/client-archive/abc123"
            class="w-full px-4 py-2.5 rounded-lg bg-[#262626] border border-neutral-700 text-sm focus:outline-none focus:border-primary mb-4"
          />
          <div
            v-if="editError"
            class="p-3 rounded-lg bg-red-900/20 border border-red-800 text-red-300 text-sm mb-4"
          >
            {{ editError }}
          </div>
          <div class="flex gap-3">
            <button
              class="flex-1 py-2.5 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm font-medium transition-colors"
              @click="cancelEdit"
            >
              Cancel
            </button>
            <button
              class="flex-1 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              :disabled="!editUrl || editLoading"
              @click="confirmEdit"
            >
              <span
                v-if="editLoading"
                class="flex items-center justify-center gap-2"
              >
                <Loader2 class="w-4 h-4 animate-spin" />
                Saving...
              </span>
              <span v-else>Save</span>
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="selfUpdateModal"
        class="absolute inset-0 z-40 bg-black/70 flex items-center justify-center p-6"
      >
        <div
          class="max-w-sm w-full p-6 rounded-xl bg-[#262626] border border-neutral-700 text-center"
        >
          <h3 class="text-lg font-bold mb-2">Update Available</h3>
          <p class="text-neutral-400 text-sm mb-1">
            A new version of MiniMin Sync is available.
          </p>
          <p class="text-sm text-neutral-300 mb-4">
            Current:
            <span class="font-mono text-xs">{{ updateInfo?.current }}</span>
            <br />
            Latest:
            <span class="font-mono text-xs text-emerald-400">{{
              updateInfo?.version
            }}</span>
          </p>
          <div class="flex gap-3">
            <button
              class="flex-1 py-2.5 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm font-medium transition-colors"
              @click="dismissSelfUpdate"
            >
              Later
            </button>
            <button
              class="flex-1 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors"
              @click="acceptSelfUpdate"
            >
              Download & Update
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="restartModal"
        class="absolute inset-0 z-40 bg-black/70 flex items-center justify-center p-6"
      >
        <div
          class="max-w-sm w-full p-6 rounded-xl bg-[#262626] border border-neutral-700 text-center"
        >
          <h3 class="text-lg font-bold mb-2">Update Ready</h3>
          <p class="text-neutral-400 text-sm mb-6">
            A new version has been downloaded. Restart the app to apply the
            update.
          </p>
          <div class="flex gap-3">
            <button
              class="flex-1 py-2.5 rounded-lg bg-[#262626] hover:bg-[#262626] text-sm font-medium transition-colors"
              @click="cancelRestart"
            >
              Later
            </button>
            <button
              class="flex-1 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors"
              @click="confirmRestart"
            >
              Restart Now
            </button>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>
