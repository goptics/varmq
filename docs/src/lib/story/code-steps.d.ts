// The stations' code, highlighted and keyed for the morph at build time (compile.ts, astro.config.mjs).
declare module 'virtual:code-steps' {
	const steps: import('./compile.ts').CodeSteps;
	export default steps;
}
