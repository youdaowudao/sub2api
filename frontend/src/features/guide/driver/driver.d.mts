import type { Config, Driver } from 'driver.js'
export function driver(config?: Config & { onS2oError?: () => void }): Driver
export function getCleanupStats(): { listeners: number; frames: number; targets: number }
