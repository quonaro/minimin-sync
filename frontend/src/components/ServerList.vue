<script setup lang="ts">
import {
  Loader2,
  RefreshCw,
  Plus,
  AlertCircle,
  Trash2,
  Play,
  Pencil,
  FolderOpen,
} from "@lucide/vue";
import { ref, onMounted, onUnmounted } from "vue";

interface Marker {
  serverId: string;
  token: string;
  baseUrl: string;
  lastSyncAt: string;
  lastCheckAt: string;
  expiresAt?: string;
}

interface Server {
  Name: string;
  Dir: string;
  Marker?: Marker;
}

const props = defineProps<{
  servers: Server[];
  pendingUpdates?: Record<string, number>;
}>();

const emit = defineEmits<{
  (e: "run", serverId: string): void;
  (e: "check", serverId: string): void;
  (e: "add"): void;
  (e: "delete", serverId: string): void;
  (e: "edit", serverId: string): void;
  (e: "open-dir", serverId: string): void;
}>();

const now = ref(Date.now());
let timer: ReturnType<typeof setInterval>;

onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now();
  }, 1000);
});

onUnmounted(() => {
  clearInterval(timer);
});

function isExpired(expiresAt?: string): boolean {
  if (!expiresAt) return false;
  return now.value > new Date(expiresAt).getTime();
}

function formatTimeLeft(expiresAt?: string): string {
  if (!expiresAt) return "";
  const diff = new Date(expiresAt).getTime() - now.value;
  if (diff <= 0) return "Expired";
  const days = Math.floor(diff / 86400000);
  const hours = Math.floor((diff % 86400000) / 3600000);
  const minutes = Math.floor((diff % 3600000) / 60000);
  const seconds = Math.floor((diff % 60000) / 1000);
  if (days > 0) return `${days}d ${hours}h ${minutes}m`;
  if (hours > 0) return `${hours}h ${minutes}m ${seconds}s`;
  return `${minutes}m ${seconds}s`;
}

function timeLeftClass(expiresAt?: string): string {
  if (!expiresAt) return "";
  const diff = new Date(expiresAt).getTime() - now.value;
  if (diff <= 0) return "text-red-400";
  if (diff < 3600000) return "text-red-400";
  if (diff < 86400000) return "text-amber-400";
  return "text-emerald-400";
}

function formatDateTime(iso?: string): string {
  if (!iso) return "never";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
</script>

<template>
  <div class="h-full relative">
    <div
      v-if="!servers || servers.length === 0"
      class="absolute inset-0 flex flex-col items-center justify-center text-neutral-500"
    >
      <AlertCircle class="w-12 h-12 mb-4" />
      <p class="text-lg font-medium text-neutral-300">No synced servers</p>
      <p class="text-sm mt-1 mb-6">Add a server to get started</p>
      <button
        class="px-5 py-2.5 rounded-xl bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors flex items-center gap-2 group"
        @click="emit('add')"
      >
        <Plus
          class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
        />
        Add Server
      </button>
    </div>

    <div v-else class="grid gap-4 max-w-2xl mx-auto">
      <div
        v-for="s in servers"
        :key="s.Name"
        class="p-4 rounded-xl bg-neutral-800 border border-neutral-700 flex items-center justify-between"
      >
        <div>
          <div class="flex items-center gap-2">
            <p class="font-medium text-white">{{ s.Name }}</p>
            <span
              v-if="pendingUpdates && pendingUpdates[s.Name]"
              class="px-1.5 py-0.5 rounded-full bg-red-500 text-white text-[10px] font-bold"
            >
              {{ pendingUpdates[s.Name] }}
            </span>
          </div>
          <p class="text-xs text-neutral-400 mt-0.5">
            Last sync: {{ formatDateTime(s.Marker?.lastSyncAt) }} · Last check:
            {{ formatDateTime(s.Marker?.lastCheckAt) }}
          </p>
          <p
            v-if="s.Marker?.expiresAt && !isExpired(s.Marker.expiresAt)"
            class="text-xs mt-0.5 font-medium"
            :class="timeLeftClass(s.Marker.expiresAt)"
          >
            Link expires in {{ formatTimeLeft(s.Marker.expiresAt) }}
          </p>
          <p
            v-else-if="s.Marker?.expiresAt && isExpired(s.Marker.expiresAt)"
            class="text-xs text-red-400 mt-0.5 font-medium"
          >
            Link expired — update required
          </p>
          <p v-else class="text-xs text-neutral-500 mt-0.5 font-medium">
            Link expiry unknown
          </p>
        </div>
        <div class="flex items-center gap-2">
          <template v-if="!isExpired(s.Marker?.expiresAt)">
            <button
              class="p-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white transition-colors group"
              :title="'Run ' + s.Name"
              @click="emit('run', s.Name)"
            >
              <Play
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
              />
            </button>
            <button
              class="px-3 py-1.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors flex items-center gap-1.5 group"
              @click="emit('check', s.Name)"
            >
              <RefreshCw
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110 group-hover:rotate-180"
              />
              {{
                pendingUpdates && pendingUpdates[s.Name] ? "Update" : "Check"
              }}
            </button>
            <button
              class="p-2 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-neutral-300 hover:text-white transition-colors group"
              :title="'Open folder'"
              @click="emit('open-dir', s.Name)"
            >
              <FolderOpen
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
              />
            </button>
            <button
              class="p-2 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-neutral-300 hover:text-white transition-colors group"
              :title="'Edit link'"
              @click="emit('edit', s.Name)"
            >
              <Pencil
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
              />
            </button>
            <button
              class="p-2 rounded-lg bg-neutral-700 hover:bg-red-600 text-neutral-400 hover:text-white transition-colors group"
              @click="emit('delete', s.Name)"
            >
              <Trash2
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
              />
            </button>
          </template>
          <template v-else>
            <button
              class="px-3 py-1.5 rounded-lg bg-amber-600 hover:bg-amber-500 text-white text-sm font-medium transition-colors flex items-center gap-1.5 group"
              @click="emit('edit', s.Name)"
            >
              <Pencil
                class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
              />
              Edit link
            </button>
          </template>
        </div>
      </div>

      <div class="flex justify-center pt-2">
        <button
          class="px-5 py-2.5 rounded-xl bg-neutral-800 hover:bg-neutral-700 border border-neutral-700 text-white text-sm font-medium transition-colors flex items-center gap-2 group"
          @click="emit('add')"
        >
          <Plus
            class="w-4 h-4 transition-transform duration-200 group-hover:scale-110"
          />
          Add Server
        </button>
      </div>
    </div>
  </div>
</template>
