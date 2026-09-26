// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
  namespace App {}
}

interface ImportMetaEnv {
  /** The development core's URL, when not given as ?core= */
  readonly VITE_NCD_CORE?: string;
}

export {};
