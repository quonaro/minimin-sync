<script setup lang="ts">
import { Loader2, RefreshCw, Plus, AlertCircle } from "@lucide/vue";

interface Server {
  Name: string;
  Dir: string;
  Marker?: {
    ServerID: string;
    Token: string;
    BaseURL: string;
    LastSyncAt: string;
  };
}

const props = defineProps<{
  servers: Server[];
}>();

const emit = defineEmits<{
  (e: "check", serverId: string): void;
  (e: "add"): void;
}>();
</script>

<template>
  <div class="h-full relative">
    <div
      v-if="servers.length === 0"
      class="absolute inset-0 flex flex-col items-center justify-center text-neutral-500"
    >
      <AlertCircle class="w-12 h-12 mb-4" />
      <p class="text-lg font-medium text-neutral-300">No synced servers</p>
      <p class="text-sm mt-1 mb-6">Add a server to get started</p>
      <button
        class="px-5 py-2.5 rounded-xl bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors flex items-center gap-2"
        @click="emit('add')"
      >
        <Plus class="w-4 h-4" />
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
          <p class="font-medium text-white">{{ s.Name }}</p>
          <p class="text-xs text-neutral-400 mt-0.5">
            Last sync: {{ s.Marker?.LastSyncAt || "never" }}
          </p>
        </div>
        <button
          class="px-3 py-1.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors flex items-center gap-1.5"
          @click="emit('check', s.Name)"
        >
          <RefreshCw class="w-4 h-4" />
          Check
        </button>
      </div>
    </div>
  </div>
</template>
