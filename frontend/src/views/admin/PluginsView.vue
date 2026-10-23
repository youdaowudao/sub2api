<template>
  <AppLayout>
    <div class="space-y-6">
      <section
        class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between"
      >
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t("admin.plugins.title") }}
          </h2>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
            {{ t("admin.plugins.description") }}
          </p>
          <div
            class="mt-3 flex flex-wrap gap-2 text-xs text-gray-600 dark:text-gray-300"
          >
            <span class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">{{
              t("admin.plugins.pluginKinds")
            }}</span>
            <span class="rounded bg-gray-100 px-2 py-1 dark:bg-dark-700">{{
              t("admin.plugins.permissionNotice")
            }}</span>
          </div>
        </div>

        <div class="flex flex-shrink-0 items-center gap-2">
          <input
            ref="fileInput"
            class="hidden"
            type="file"
            accept=".s2plugin,application/zip"
            @change="handleFileSelected"
          />
          <button
            type="button"
            class="btn btn-primary"
            :disabled="uploading"
            @click="fileInput?.click()"
          >
            <Icon name="upload" size="sm" />
            {{ uploading ? t("common.processing") : t("admin.plugins.upload") }}
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadPlugins"
          >
            <Icon name="refresh" size="sm" />
            <span class="sr-only">{{ t("common.refresh") }}</span>
          </button>
        </div>
      </section>

      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t("admin.plugins.uploadHint") }}
      </p>

      <div
        class="border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-200"
      >
        <p>{{ t("admin.plugins.runtimeNotice") }}</p>
        <p class="mt-1">{{ t("admin.plugins.menuNotice") }}</p>
      </div>

      <div
        v-if="loading"
        class="flex min-h-48 items-center justify-center text-sm text-gray-500"
      >
        {{ t("common.loading") }}
      </div>

      <div
        v-else-if="plugins.length === 0"
        class="flex min-h-56 flex-col items-center justify-center border border-dashed border-gray-300 px-6 text-center dark:border-dark-600"
      >
        <Icon name="cube" size="xl" class="text-gray-400" />
        <p class="mt-3 font-medium text-gray-800 dark:text-gray-200">
          {{ t("admin.plugins.empty") }}
        </p>
        <p class="mt-1 max-w-lg text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.plugins.emptyHint") }}
        </p>
      </div>

      <div v-else class="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <article
          v-for="plugin in plugins"
          :key="plugin.id"
          class="card overflow-hidden border border-gray-200 dark:border-dark-700"
        >
          <div
            class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 p-5 dark:border-dark-700"
          >
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h3
                  class="truncate text-base font-semibold text-gray-900 dark:text-white"
                >
                  {{ plugin.name }}
                </h3>
                <span class="font-mono text-xs text-gray-500"
                  >v{{ plugin.version }}</span
                >
                <span class="rounded bg-gray-100 px-2 py-0.5 text-xs dark:bg-dark-700">{{ pluginKind(plugin) }}</span>
                <span
                  class="rounded px-2 py-0.5 text-xs font-medium"
                  :class="stateClass(plugin.state)"
                >
                  {{ t(`admin.plugins.${plugin.state}`) }}
                </span>
              </div>
              <p class="mt-1 text-xs text-gray-500">
                {{ plugin.plugin_key
                }}<span v-if="plugin.author"> · {{ plugin.author }}</span>
              </p>
              <p
                v-if="plugin.description"
                class="mt-2 text-sm text-gray-600 dark:text-gray-300"
              >
                {{ plugin.description }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              @click="openConfiguration(plugin)"
            >
              <Icon name="cog" size="sm" />
              {{ t("admin.plugins.configure") }}
            </button>
          </div>

          <div class="grid grid-cols-1 gap-x-6 gap-y-4 p-5 md:grid-cols-2">
            <div>
              <p class="text-xs font-medium uppercase text-gray-500">
                {{ t("admin.plugins.compatibility") }}
              </p>
              <div class="mt-2 flex items-center gap-2">
                <span
                  class="rounded px-2 py-0.5 text-xs font-medium"
                  :class="compatibilityClass(plugin.compatibility.status)"
                >
                  {{ t(`admin.plugins.${plugin.compatibility.status}`) }}
                </span>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{
                  plugin.compatibility.message
                }}</span>
              </div>
              <dl
                class="mt-3 grid grid-cols-[auto,1fr] gap-x-3 gap-y-1 text-xs"
              >
                <dt class="text-gray-500">
                  {{ t("admin.plugins.currentVersion") }}
                </dt>
                <dd class="font-mono text-gray-800 dark:text-gray-200">
                  {{ plugin.compatibility.current_sub2api_version }}
                </dd>
                <dt class="text-gray-500">
                  {{ t("admin.plugins.requiredVersion") }}
                </dt>
                <dd class="font-mono text-gray-800 dark:text-gray-200">
                  {{ plugin.compatibility.required_sub2api_version }}
                </dd>
                <dt class="text-gray-500">
                  {{ t("admin.plugins.recommendedVersion") }}
                </dt>
                <dd class="font-mono text-gray-800 dark:text-gray-200">
                  {{ plugin.compatibility.recommended_sub2api_version || "-" }}
                </dd>
              </dl>
            </div>

            <div>
              <p class="text-xs font-medium uppercase text-gray-500">
                {{ t("admin.plugins.runtime") }}
              </p>
              <div class="mt-2 flex flex-wrap gap-2 text-xs">
                <span
                  class="rounded px-2 py-0.5"
                  :class="
                    plugin.runtime_healthy || isFrontendOnly(plugin)
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
                  "
                >
                  {{
                    isFrontendOnly(plugin)
                      ? t("admin.plugins.noProcess")
                      : plugin.runtime_healthy
                      ? t("admin.plugins.healthy")
                      : t("admin.plugins.unhealthy")
                  }}
                </span>
                <span
                  class="rounded bg-gray-100 px-2 py-0.5 text-gray-600 dark:bg-dark-700 dark:text-gray-300"
                >
                  {{ t("admin.plugins.signature") }}:
                  {{ t(`admin.plugins.${plugin.signature_status}`) }}
                </span>
              </div>
              <p
                v-if="plugin.last_error"
                class="mt-3 break-words text-xs text-red-600 dark:text-red-400"
              >
                {{ plugin.last_error }}
              </p>
              <p
                v-else-if="plugin.runtime_message"
                class="mt-3 break-words text-xs text-gray-500"
              >
                {{ plugin.runtime_message }}
              </p>
            </div>

            <div v-if="plugin.manifest.schema_version === 2" class="md:col-span-2 space-y-1 text-xs text-gray-600 dark:text-gray-300">
              <p class="font-medium">{{ t('admin.plugins.permissions') }}</p>
              <p v-if="!plugin.manifest.capabilities.length">{{ t('admin.plugins.noHostPermissions') }}</p>
              <p v-for="capability in plugin.manifest.capabilities" :key="capability.id">{{ permissionLabel(capability.id) }}</p>
            </div>

            <div v-if="plugin.manifest.schema_version === 1" class="md:col-span-2">
              <label
                class="flex items-center justify-between gap-4 text-xs font-medium text-gray-600 dark:text-gray-300"
              >
                <span>{{ t("admin.plugins.rollout") }}</span>
                <span class="w-11 text-right font-mono"
                  >{{
                    rolloutValues[plugin.id] ?? currentRollout(plugin)
                  }}%</span
                >
              </label>
              <input
                :value="rolloutValues[plugin.id] ?? currentRollout(plugin)"
                type="range"
                min="1"
                max="100"
                step="1"
                class="mt-2 w-full accent-primary-600"
                :disabled="hasEnabledBinding(plugin)"
                @input="setRollout(plugin.id, $event)"
              />
            </div>
          </div>

          <div
            class="flex flex-wrap justify-end gap-2 border-t border-gray-100 px-5 py-4 dark:border-dark-700"
          >
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyID === plugin.id"
              @click="testPlugin(plugin)"
            >
              <Icon name="beaker" size="sm" />
              {{ t("admin.plugins.test") }}
            </button>
            <button
              v-if="hasEnabledBinding(plugin)"
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyID === plugin.id"
              @click="disablePlugin(plugin)"
            >
              <Icon name="ban" size="sm" />
              {{ t("admin.plugins.disable") }}
            </button>
            <button
              v-else
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="
                busyID === plugin.id ||
                plugin.state === 'starting' ||
                !plugin.compatibility.compatible
              "
              @click="enablePlugin(plugin)"
            >
              <Icon name="play" size="sm" />
              {{ t("admin.plugins.enable") }}
            </button>
            <button
              type="button"
              class="btn btn-danger btn-sm"
              :disabled="busyID === plugin.id || hasEnabledBinding(plugin)"
              @click="uninstallPlugin(plugin)"
            >
              <Icon name="trash" size="sm" />
              {{ t("admin.plugins.uninstall") }}
            </button>
          </div>
        </article>
      </div>

      <BaseDialog
        :show="configPlugin !== null"
        :title="
          t('admin.plugins.configTitle', { name: configPlugin?.name || '' })
        "
        width="full"
        @close="closeConfiguration"
      >
        <div v-if="configPlugin && !configPlugin.manifest.ui.entrypoint" class="space-y-4 p-4">
          <p class="text-sm text-gray-500">{{ t('admin.plugins.jsonConfigHint') }}</p>
          <p v-if="uiLoading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
          <p v-if="uiError" role="alert" class="text-sm text-red-600">{{ uiError }}</p>
          <textarea v-model="jsonConfig" :disabled="!jsonLoaded || jsonSaving" class="input min-h-72 w-full font-mono text-sm" :aria-label="t('admin.plugins.jsonConfigLabel')" spellcheck="false" />
          <button v-if="uiError && !jsonLoaded" type="button" class="btn btn-secondary" @click="openConfiguration(configPlugin)">{{ t('admin.plugins.retry') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!jsonLoaded || jsonSaving" @click="saveJSONConfiguration">{{ t('common.save') }}</button>
        </div>
        <div
          v-else
          class="relative min-h-[520px] overflow-hidden bg-gray-50 dark:bg-dark-900"
          :style="{ height: `${iframeHeight}px` }"
        >
          <div
            v-if="uiLoading"
            class="absolute inset-0 z-10 flex items-center justify-center text-sm text-gray-500"
          >
            {{ t("admin.plugins.loadingUI") }}
          </div>
          <div
            v-if="uiError"
            class="absolute inset-0 z-20 flex flex-col items-center justify-center p-8 text-center"
          >
            <Icon name="exclamationTriangle" size="xl" class="text-amber-500" />
            <p class="mt-3 font-medium text-gray-800 dark:text-gray-200">
              {{ t("admin.plugins.uiUnavailable") }}
            </p>
            <p class="mt-1 max-w-xl text-sm text-gray-500">{{ uiError }}</p>
          </div>
          <iframe
            v-if="uiSession"
            ref="pluginFrame"
            :src="uiSession.url"
            sandbox="allow-scripts"
            referrerpolicy="no-referrer"
            class="h-full w-full border-0 bg-white dark:bg-dark-900"
            :title="
              t('admin.plugins.configTitle', { name: configPlugin?.name || '' })
            "
            @load="handlePluginFrameLoad"
          />
        </div>
      </BaseDialog>

      <TotpStepUpDialog :controller="pluginStepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { createPluginBridge, type PluginBridgeMessage, type PluginBridgeRequest } from "@/components/plugins/bridge";
import { useI18n } from "vue-i18n";
import {
  adminAPI,
  type PluginInstallation,
  type PluginUISession,
} from "@/api/admin";
import { useAppStore } from "@/stores";
import { useAuthStore } from "@/stores/auth";
import AppLayout from "@/components/layout/AppLayout.vue";
import BaseDialog from "@/components/common/BaseDialog.vue";
import Icon from "@/components/icons/Icon.vue";
import TotpStepUpDialog from "@/components/auth/TotpStepUpDialog.vue";
import {
  isStepUpBlocked,
  isStepUpCancelled,
  stepUpBlockReason,
  useStepUp,
} from "@/composables/useStepUp";

const { t } = useI18n();
const appStore = useAppStore();
const auth = useAuthStore();
const pluginStepUp = useStepUp();
const plugins = ref<PluginInstallation[]>([]);
const loading = ref(false);
const uploading = ref(false);
const busyID = ref<number | null>(null);
const fileInput = ref<HTMLInputElement | null>(null);
const rolloutValues = ref<Record<number, number>>({});
const configPlugin = ref<PluginInstallation | null>(null);
const uiSession = ref<PluginUISession | null>(null);
const pluginFrame = ref<HTMLIFrameElement | null>(null);
const uiLoading = ref(false);
const uiError = ref("");
const iframeHeight = ref(640);
const pluginFrameLoaded = ref(false);
const jsonConfig = ref("{}");
const jsonSaving = ref(false);
const jsonLoaded = ref(false);
let configController: AbortController | undefined;
let uiLoadTimeout: number | undefined;
const bridge = createPluginBridge({
  frame: () => pluginFrame.value,
  token: () => uiSession.value?.bridge_token,
  version: () => uiSession.value?.ui_bridge_version || 1,
  accepts: type => ["sub2api.plugin.ready", "config.load", "config.save", "config.test", "plugin.status", "ui.resize", "ui.notify"].includes(type),
  expectsResponse: type => ["config.load", "config.save", "config.test", "plugin.status"].includes(type),
  handle: handleBridgeMessage,
  connected: () => {
    window.clearTimeout(uiLoadTimeout);
    uiLoading.value = false;
  },
});
let uiSessionVersion = 0;

function errorMessage(error: unknown): string {
  if (typeof error === "object" && error !== null && "message" in error) {
    return String(
      (error as { message?: unknown }).message || t("common.unknownError"),
    );
  }
  return t("common.unknownError");
}

function reportSensitiveActionError(error: unknown): void {
  if (isStepUpCancelled(error)) return;
  if (isStepUpBlocked(error)) {
    appStore.showError(
      stepUpBlockReason(error) === "STEP_UP_ADMIN_API_KEY_FORBIDDEN"
        ? t("stepUp.adminApiKeyForbidden")
        : t("stepUp.notEnabled"),
    );
    return;
  }
  appStore.showError(errorMessage(error));
}

async function loadPlugins(): Promise<void> {
  loading.value = true;
  try {
    plugins.value = await adminAPI.plugins.list();
    for (const plugin of plugins.value) {
      rolloutValues.value[plugin.id] = currentRollout(plugin);
    }
  } catch (error: unknown) {
    appStore.showError(errorMessage(error));
  } finally {
    loading.value = false;
  }
}

async function handleFileSelected(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  target.value = "";
  if (!file || !file.name.toLowerCase().endsWith(".s2plugin")) {
    appStore.showError(t("admin.plugins.fileRequired"));
    return;
  }
  uploading.value = true;
  try {
    await pluginStepUp.run(() => adminAPI.plugins.upload(file));
    appStore.showSuccess(t("admin.plugins.uploadSuccess"));
    await loadPlugins();
  } catch (error: unknown) {
    reportSensitiveActionError(error);
  } finally {
    uploading.value = false;
  }
}

function currentRollout(plugin: PluginInstallation): number {
  return (
    plugin.bindings.find(
      (binding) => binding.capability === "openai.oauth.outbound_transport.v1",
    )?.rollout_percent || 100
  );
}

function hasEnabledBinding(plugin: PluginInstallation): boolean {
  return plugin.manifest.schema_version === 2
    ? plugin.state === "enabled" || plugin.state === "starting"
    : plugin.bindings.some((binding) => binding.enabled);
}

function setRollout(id: number, event: Event): void {
  const value = Number((event.target as HTMLInputElement).value);
  rolloutValues.value[id] = Math.min(100, Math.max(1, value));
}

async function enablePlugin(plugin: PluginInstallation): Promise<void> {
  let acceptUntested = false;
  if (!plugin.compatibility.tested) {
    acceptUntested = window.confirm(t("admin.plugins.confirmUntested"));
    if (!acceptUntested) return;
  }
  busyID.value = plugin.id;
  try {
    await pluginStepUp.run(() =>
      adminAPI.plugins.enable(
        plugin.id,
        rolloutValues.value[plugin.id] || 100,
        acceptUntested,
      ),
    );
    appStore.showSuccess(t("admin.plugins.enableSuccess"));
    await loadPlugins();
  } catch (error: unknown) {
    reportSensitiveActionError(error);
  } finally {
    busyID.value = null;
  }
}

async function disablePlugin(plugin: PluginInstallation): Promise<void> {
  if (!window.confirm(t("admin.plugins.confirmDisable"))) return;
  busyID.value = plugin.id;
  try {
    await pluginStepUp.run(() => adminAPI.plugins.disable(plugin.id));
    appStore.showSuccess(t("admin.plugins.disableSuccess"));
    await loadPlugins();
  } catch (error: unknown) {
    reportSensitiveActionError(error);
  } finally {
    busyID.value = null;
  }
}

async function uninstallPlugin(plugin: PluginInstallation): Promise<void> {
  if (!window.confirm(t("admin.plugins.confirmUninstall"))) return;
  busyID.value = plugin.id;
  try {
    await pluginStepUp.run(() => adminAPI.plugins.remove(plugin.id));
    appStore.showSuccess(t("admin.plugins.uninstallSuccess"));
    await loadPlugins();
  } catch (error: unknown) {
    reportSensitiveActionError(error);
  } finally {
    busyID.value = null;
  }
}

async function testPlugin(plugin: PluginInstallation): Promise<void> {
  busyID.value = plugin.id;
  try {
    const result = await pluginStepUp.run(() =>
      adminAPI.plugins.test(plugin.id),
    );
    if (result.success)
      appStore.showSuccess(result.message || t("admin.plugins.testSuccess"));
    else appStore.showError(result.message || t("common.error"));
  } catch (error: unknown) {
    reportSensitiveActionError(error);
  } finally {
    busyID.value = null;
  }
}

function isFrontendOnly(plugin: PluginInstallation): boolean {
  return plugin.manifest.schema_version === 2 && !Object.keys(plugin.manifest.runtimes || {}).length;
}

function pluginKind(plugin: PluginInstallation): string {
  if (plugin.manifest.schema_version === 1) return t("admin.plugins.transportKind");
  if (isFrontendOnly(plugin)) return t("admin.plugins.frontendKind");
  return t(plugin.manifest.ui.entrypoint || plugin.manifest.ui.entries?.length ? "admin.plugins.combinedKind" : "admin.plugins.backendKind");
}

function permissionLabel(id: string): string {
  const labels: Record<string, string> = {
    "backend.invoke.v1": "invokePermission", "backend.background.v1": "backgroundPermission",
    "storage.read.v1": "storageReadPermission", "storage.write.v1": "storageWritePermission",
    "users.created.read.v1": "usersReadPermission", "users.email.read.v1": "emailReadPermission",
  };
  return labels[id] ? t(`admin.plugins.${labels[id]}`) : id;
}

async function openConfiguration(plugin: PluginInstallation): Promise<void> {
  const version = ++uiSessionVersion;
  configController?.abort();
  configController = new AbortController();
  const signal = configController.signal;
  configPlugin.value = plugin;
  window.clearTimeout(uiLoadTimeout);
  uiSession.value = null;
  pluginFrameLoaded.value = false;
  bridge.disconnect();
  pluginStepUp.onCancel?.();
  uiLoading.value = true;
  uiError.value = "";
  jsonConfig.value = "{}";
  jsonSaving.value = false;
  jsonLoaded.value = false;
  iframeHeight.value = 640;
  try {
    if (!plugin.manifest.ui.entrypoint) {
      const config = await adminAPI.plugins.getConfig(plugin.id, signal);
      if (version !== uiSessionVersion) return;
      jsonConfig.value = JSON.stringify(config, null, 2);
      jsonLoaded.value = true;
      uiLoading.value = false;
    } else {
      const session = await adminAPI.plugins.createUISession(plugin.id, signal);
      if (version !== uiSessionVersion) return;
      uiSession.value = session;
      if (session.ui_bridge_version === 2) uiLoadTimeout = window.setTimeout(configurationFrameFailed, 30_000);
    }
  } catch (error: unknown) {
    if (version !== uiSessionVersion) return;
    uiLoading.value = false;
    uiError.value = errorMessage(error);
  }
}

async function saveJSONConfiguration(): Promise<void> {
  const plugin = configPlugin.value;
  if (!plugin || !jsonLoaded.value || jsonSaving.value) return;
  let config: Record<string, unknown>;
  try {
    const parsed: unknown = JSON.parse(jsonConfig.value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
    config = parsed as Record<string, unknown>;
  } catch {
    uiError.value = t("admin.plugins.jsonObjectRequired");
    return;
  }
  const version = uiSessionVersion;
  const signal = configController?.signal;
  jsonSaving.value = true;
  uiError.value = "";
  try {
    const result = await pluginStepUp.run(() => {
      if (version !== uiSessionVersion || signal?.aborted) throw new Error("Request expired");
      return adminAPI.plugins.saveConfig(plugin.id, config, signal);
    });
    if (version !== uiSessionVersion) return;
    jsonConfig.value = JSON.stringify(result, null, 2);
    appStore.showSuccess(t("common.saved"));
  } catch (error) {
    if (version !== uiSessionVersion) return;
    if (!isStepUpCancelled(error)) uiError.value = errorMessage(error);
    if (isStepUpBlocked(error)) reportSensitiveActionError(error);
  } finally {
    if (version === uiSessionVersion) jsonSaving.value = false;
  }
}

function closeConfiguration(): void {
  uiSessionVersion++;
  configController?.abort();
  configController = undefined;
  bridge.disconnect();
  window.clearTimeout(uiLoadTimeout);
  pluginStepUp.onCancel?.();
  pluginFrameLoaded.value = false;
  configPlugin.value = null;
  uiSession.value = null;
  uiLoading.value = false;
  uiError.value = "";
  jsonSaving.value = false;
  jsonLoaded.value = false;
}

function handlePluginFrameLoad(): void {
  if (uiSession.value?.ui_bridge_version === 2) {
    pluginStepUp.onCancel?.();
    window.clearTimeout(uiLoadTimeout);
    uiLoadTimeout = window.setTimeout(configurationFrameFailed, 30_000);
    uiLoading.value = true;
    bridge.documentLoaded();
    return;
  }
  if (pluginFrameLoaded.value) {
    bridge.reset();
    pluginStepUp.onCancel?.();
  }
  pluginFrameLoaded.value = true;
  uiLoading.value = false;
}

function configurationFrameFailed(): void {
  bridge.disconnect();
  pluginStepUp.onCancel?.();
  window.clearTimeout(uiLoadTimeout);
  uiSession.value = null;
  uiLoading.value = false;
  uiError.value = t("admin.plugins.pageLoadFailed");
}

async function handleBridgeMessage(message: PluginBridgeMessage, request: PluginBridgeRequest): Promise<void> {
  const pluginID = configPlugin.value?.id;
  if (!pluginID) return;
  const sensitive = <T,>(action: () => Promise<T>) => {
    const cancelStepUp = () => pluginStepUp.onCancel?.();
    request.signal.addEventListener("abort", cancelStepUp, { once: true });
    return pluginStepUp.run(() => {
      if (!request.active()) throw new Error("Request expired");
      return action();
    }).finally(() => request.signal.removeEventListener("abort", cancelStepUp));
  };
  try {
    switch (message.type) {
      case "sub2api.plugin.ready":
        uiLoading.value = false;
        break;
      case "config.load": {
        const config = await adminAPI.plugins.getConfig(pluginID, request.signal);
        request.reply({ ok: true, config });
        break;
      }
      case "config.save": {
        if (!message.config || typeof message.config !== "object" || Array.isArray(message.config)) {
          throw new Error(t("admin.plugins.bridgeRejected"));
        }
        const config = await sensitive(() => adminAPI.plugins.saveConfig(pluginID, message.config as Record<string, unknown>, request.signal));
        if (request.reply({ ok: true, config })) appStore.showSuccess(t("common.saved"));
        break;
      }
      case "config.test": {
        const result = await sensitive(() => adminAPI.plugins.test(pluginID, request.signal));
        if (request.reply({ ok: result.success, result }) && !result.success) appStore.showError(result.message || t("common.error"));
        break;
      }
      case "plugin.status": {
        const result = await adminAPI.plugins.status(pluginID, request.signal);
        request.reply({ ok: true, result });
        break;
      }
      case "ui.resize": {
        const height = Number(message.height);
        if (Number.isFinite(height)) iframeHeight.value = Math.min(960, Math.max(520, Math.round(height)));
        break;
      }
      case "ui.notify": {
        const text = typeof message.message === "string" ? message.message.slice(0, 500) : "";
        if (!text) break;
        if (message.level === "error") appStore.showError(text);
        else if (message.level === "success") appStore.showSuccess(text);
        else appStore.showInfo(text);
        break;
      }
    }
  } catch (error: unknown) {
    if (!request.active()) return;
    if (isStepUpBlocked(error)) reportSensitiveActionError(error);
    request.reply({ ok: false, error: isStepUpCancelled(error) ? t("common.cancel") : errorMessage(error) });
  }
}

function stateClass(state: PluginInstallation["state"]): string {
  if (state === "enabled")
    return "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300";
  if (state === "error" || state === "incompatible")
    return "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300";
  if (state === "starting")
    return "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300";
  return "bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300";
}

function compatibilityClass(
  status: PluginInstallation["compatibility"]["status"],
): string {
  if (status === "compatible")
    return "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300";
  if (status === "untested")
    return "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300";
  return "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300";
}

watch(() => `${auth.token || ""}:${auth.user?.id || ""}:${auth.user?.role || ""}:${auth.user?.status || ""}`, () => {
  if (configPlugin.value?.manifest.schema_version === 2) closeConfiguration();
}, { flush: "sync" });

onMounted(() => {
  bridge.start();
  void loadPlugins();
});

onBeforeUnmount(() => {
  bridge.stop();
  closeConfiguration();
});
</script>
