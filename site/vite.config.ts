import { defineConfig } from 'vite';
import { codeSteps } from './lib/home/compile.ts';

// The stations' code is highlighted and keyed for the morph at build time, so the page ships tokens, not Shiki.
const CODE = 'virtual:code-steps';

export default defineConfig({
	plugins: [
		{
			name: 'code-steps',
			resolveId: (id) => (id === CODE ? `\0${CODE}` : undefined),
			load: async (id) => (id === `\0${CODE}` ? `export default ${JSON.stringify(await codeSteps())}` : undefined),
		},
	],
});
