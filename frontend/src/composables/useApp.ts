import { ref, onMounted, computed, onErrorCaptured } from "vue";
import {
  GetConfig,
  DiscoverAllLaunchers,
  SaveConfig,
  GetServers,
  GetUnlinkedInstances,
  SelectInstancesDir,
  RemoveServer,
  RunServer,
  UpdateServerURL,
  LinkInstance,
  RefreshServerInfo,
  OpenInstanceDir,
  CheckForUpdate,
  DownloadUpdate,
  RestartApp,
  // CancelUpdate is available but not used directly in UI
  GetVersion,
  HasPendingUpdate,
} from "../../wailsjs/go/main/App";
import { config } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime";

export type View = "setup" | "list" | "add" | "check";

export function useApp() {
  const currentView = ref<View>("setup");
  const instancesDir = ref("");
  const servers = ref<any[]>([]);
  const unlinkedServers = ref<any[]>([]);
  const showUnlinked = ref(false);
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
  const linkModal = ref(false);
  const linkTarget = ref("");
  const linkUrl = ref("");
  const linkError = ref("");
  const linkLoading = ref(false);
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

  async function loadUnlinkedServers() {
    try {
      const result = await GetUnlinkedInstances();
      unlinkedServers.value = result ?? [];
    } catch {
      unlinkedServers.value = [];
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
    const _existing = await GetConfig();
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
    const _existing = await GetConfig();
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

  function openLink(serverId: string) {
    linkTarget.value = serverId;
    linkUrl.value = "";
    linkError.value = "";
    linkLoading.value = false;
    linkModal.value = true;
  }

  async function confirmLink() {
    if (!linkUrl.value) return;
    linkLoading.value = true;
    linkError.value = "";
    try {
      await LinkInstance(linkTarget.value, linkUrl.value);
      linkModal.value = false;
      showUnlinked.value = false;
      await loadServers();
      await loadUnlinkedServers();
    } catch (e: any) {
      linkError.value = e?.toString?.() || "Failed to link server";
    } finally {
      linkLoading.value = false;
    }
  }

  function cancelLink() {
    linkModal.value = false;
    linkTarget.value = "";
    linkUrl.value = "";
    linkError.value = "";
    linkLoading.value = false;
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

  const displayServers = computed(() => {
    return showUnlinked.value ? unlinkedServers.value : servers.value;
  });

  const listMode = computed(() => {
    return showUnlinked.value ? "unlinked" : "linked";
  });

  const pageTitle = computed(() => {
    switch (currentView.value) {
      case "setup":
        return "Setup";
      case "list":
        return showUnlinked.value ? "Other Builds" : "Servers";
      case "add":
        return "Add Server";
      case "check":
        return selectedServer.value;
      default:
        return "";
    }
  });

  return {
    currentView,
    instancesDir,
    servers,
    unlinkedServers,
    showUnlinked,
    displayServers,
    listMode,
    selectedServer,
    detectedLaunchers,
    selectedLauncher,
    scanning,
    appError,
    deleteConfirm,
    deleteTarget,
    pendingUpdates,
    checkErrors,
    autoCheckRunning,
    editModal,
    editTarget,
    editUrl,
    editError,
    editLoading,
    linkModal,
    linkTarget,
    linkUrl,
    linkError,
    linkLoading,
    autoCheckInterval,
    updateInfo,
    updateChecking,
    updateError,
    updateDownloading,
    updateProgress,
    updateTotal,
    restartModal,
    selfUpdateModal,
    setupTab,
    appVersion,
    versionToast,
    pageTitle,
    loadServers,
    loadUnlinkedServers,
    scanLaunchers,
    selectLauncher,
    browseDir,
    saveDir,
    goAdd,
    openDeleteConfirm,
    confirmDelete,
    cancelDelete,
    handleRun,
    handleOpenDir,
    openEdit,
    confirmEdit,
    cancelEdit,
    openLink,
    confirmLink,
    cancelLink,
    goList,
    goCheck,
    goSetup,
    isSelected,
    launcherName,
    launcherType,
    handleLogoClick,
    checkUpdate,
    doUpdate,
    confirmRestart,
    cancelRestart,
    handleManualCheck,
    acceptSelfUpdate,
    dismissSelfUpdate,
  };
}
