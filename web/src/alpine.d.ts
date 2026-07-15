// Type declarations for Alpine.js magic properties and component context.
// Alpine injects these onto every component's `this` at runtime.

/** Alpine's $watch callback. */
type WatchCallback<T = any> = (newValue: T, oldValue: T) => void

interface AlpineMagic {
  $watch: (name: string, callback: WatchCallback) => void
  $nextTick: (callback: () => void) => Promise<void>
  $dispatch: (name: string, detail?: unknown) => void
  $refs: Record<string, HTMLElement>
  $el: HTMLElement
  $data: <T = unknown>(el: HTMLElement) => T
  $store: Record<string, unknown>
}

/**
 * Base type for Alpine component `this` context.
 * Component factories should use `this: AlpineComponent<ReturnType>`
 * so TypeScript knows about magic properties AND the returned object's
 * own properties/methods.
 */
type AlpineComponent<T> = T & AlpineMagic
