// Ambient module declarations for packages without bundled TypeScript types.

declare module 'alpinejs' {
  const Alpine: {
    plugin: (plugin: any) => void
    data: (name: string, factory: (...args: any[]) => any) => void
    start: () => void
    reactive: <T>(obj: T) => T
    $data: <T = unknown>(el: Element) => T
    [key: string]: any
  }
  export default Alpine
  export type Alpine = typeof Alpine
}

declare module '@alpinejs/focus' {
  const focus: any
  export default focus
}

declare module '@alpinejs/focus/dist/module.cjs.js' {
  const focus: any
  export default focus
}

declare module 'libsodium-wrappers' {
  const sodium: {
    ready: Promise<void>
    crypto_box_seal: (message: Uint8Array, publicKey: Uint8Array) => Uint8Array
    [key: string]: any
  }
  export default sodium
}

declare module 'libsodium-wrappers/dist/modules-esm/libsodium-wrappers.mjs' {
  const sodium: {
    ready: Promise<void>
    crypto_box_seal: (message: Uint8Array, publicKey: Uint8Array) => Uint8Array
    [key: string]: any
  }
  export default sodium
}

// CSS side-effect imports from @fontsource
declare module '@fontsource/poppins/*.css'

// Generic CSS module declaration
declare module '*.css'
