<script setup lang="ts">
import { ref } from "vue";
import { AddServer, PreviewServer } from "../../wailsjs/go/main/App";
import { Link, Loader2, CheckCircle, ArrowLeft, Server } from "@lucide/vue";
import { EventsOn } from "../../wailsjs/runtime";

const emit = defineEmits<{
  (e: "done"): void;
}>();

type Step = "input" | "preview" | "installing" | "done";

const url = ref("");
const step = ref<Step>("input");
const previewInfo = ref<any>(null);
const previewError = ref("");
const status = ref("");
const progress = ref(0);
const total = ref(0);
const error = ref("");
const done = ref(false);

if ((window as any).runtime) {
  EventsOn("addServer:status", (msg: string) => {
    status.value = msg;
  });

  EventsOn("addServer:progress", (d: number, t: number) => {
    progress.value = d;
    total.value = t;
  });

  EventsOn("addServer:error", (msg: string) => {
    error.value = msg;
    status.value = "";
  });

  EventsOn("addServer:done", (name: string) => {
    done.value = true;
    status.value = `Installed: ${name}`;
  });
}

async function preview() {
  if (!url.value) return;
  previewError.value = "";
  try {
    const info = await PreviewServer(url.value);
    previewInfo.value = info;
    step.value = "preview";
  } catch (e: any) {
    previewError.value = e?.toString?.() || "Failed to fetch server info";
  }
}

async function confirm() {
  error.value = "";
  done.value = false;
  progress.value = 0;
  status.value = "starting...";
  step.value = "installing";
  await AddServer(url.value);
}

function cancel() {
  step.value = "input";
  previewInfo.value = null;
}

function back() {
  emit("done");
}
</script>

<template>
  <div class="max-w-3xl mx-auto w-full">
    <div v-if="step === 'input'">
      <h2 class="text-xl font-bold mb-4">Add Server</h2>
      <p class="text-neutral-400 text-sm mb-6">
        Paste the Prism archive link from your minimin server
      </p>

      <div class="flex gap-2 mb-4">
        <input
          v-model="url"
          type="text"
          placeholder="https://host/api/client-archive/abc123"
          class="flex-1 px-4 py-2.5 rounded-lg bg-neutral-800 border border-neutral-700 text-sm focus:outline-none focus:border-primary"
        />
        <button
          class="px-4 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors flex items-center gap-1.5"
          :disabled="!url"
          @click="preview"
        >
          <Link class="w-4 h-4" />
          Preview
        </button>
      </div>

      <div
        v-if="previewError"
        class="p-4 rounded-lg bg-red-900/20 border border-red-800 text-red-300 text-sm"
      >
        {{ previewError }}
      </div>
    </div>

    <div v-else-if="step === 'preview' && previewInfo">
      <h2 class="text-xl font-bold mb-4 flex items-center gap-2">
        <Server class="w-5 h-5 text-primary" />
        Confirm Installation
      </h2>

      <div
        class="p-4 rounded-xl bg-neutral-800 border border-neutral-700 mb-6 space-y-2"
      >
        <div class="flex justify-between">
          <span class="text-neutral-400 text-sm">Server</span>
          <span class="text-white font-medium">{{
            previewInfo.serverName
          }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-neutral-400 text-sm">Formats</span>
          <span class="text-white">{{
            previewInfo.formats?.join?.(", ") || "prism"
          }}</span>
        </div>
        <div class="flex justify-between">
          <span class="text-neutral-400 text-sm">Expires</span>
          <span class="text-white">{{
            previewInfo.expiresAt
              ? new Date(previewInfo.expiresAt).toLocaleDateString()
              : "N/A"
          }}</span>
        </div>
      </div>

      <div class="flex gap-3">
        <button
          class="flex-1 py-2.5 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-sm font-medium transition-colors flex items-center justify-center gap-1.5"
          @click="cancel"
        >
          <ArrowLeft class="w-4 h-4" />
          Cancel
        </button>
        <button
          class="flex-1 py-2.5 rounded-lg bg-primary hover:bg-primary/90 text-white text-sm font-medium transition-colors"
          @click="confirm"
        >
          Confirm & Install
        </button>
      </div>
    </div>

    <div v-else-if="step === 'installing' || step === 'done'">
      <h2 class="text-xl font-bold mb-4">Installing</h2>

      <div v-if="status && !error && !done" class="space-y-2">
        <div class="flex items-center gap-2 text-sm text-neutral-300">
          <Loader2 class="w-4 h-4 animate-spin" />
          <span class="capitalize">{{ status }}</span>
        </div>
        <div v-if="total > 0" class="w-full bg-neutral-800 rounded-full h-2">
          <div
            class="bg-primary h-2 rounded-full transition-all"
            :style="{ width: `${Math.min(100, (progress / total) * 100)}%` }"
          ></div>
        </div>
        <p v-if="total > 0" class="text-xs text-neutral-500 text-right">
          {{ Math.round(progress / 1024 / 1024) }} /
          {{ Math.round(total / 1024 / 1024) }} MB
        </p>
      </div>

      <div
        v-else-if="error"
        class="p-4 rounded-lg bg-red-900/20 border border-red-800 text-red-300 text-sm"
      >
        {{ error }}
      </div>

      <div
        v-else-if="done"
        class="p-4 rounded-lg bg-emerald-900/20 border border-emerald-800 text-emerald-300 text-sm flex items-center gap-2"
      >
        <CheckCircle class="w-4 h-4" />
        {{ status }}
      </div>

      <button
        v-if="done || error"
        class="mt-4 px-4 py-2 rounded-lg bg-neutral-700 hover:bg-neutral-600 text-sm transition-colors"
        @click="back"
      >
        Back to servers
      </button>
    </div>
  </div>
</template>
