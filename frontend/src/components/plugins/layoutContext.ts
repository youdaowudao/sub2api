import type { ComputedRef, InjectionKey, ShallowRef } from 'vue'

export interface PluginLayoutAnchors {
  navigation: HTMLElement | null
  content: HTMLElement | null
}

// The current layout registers its computed ref during setup, before its DOM
// refs are filled. Replacing a page must not briefly remove the shell's owner.
export const pluginLayoutKey: InjectionKey<ShallowRef<ComputedRef<PluginLayoutAnchors> | null>> = Symbol('plugin-layout')
