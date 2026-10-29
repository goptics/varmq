/// <reference types="vite/client" />

declare module 'virtual:code-steps' {
	const steps: import('../lib/home/compile.ts').CodeSteps;
	export default steps;
}
