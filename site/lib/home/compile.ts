import { codeToKeyedTokens, createMagicMoveMachine } from '@shikijs/magic-move/core';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import { createHighlighter } from 'shiki';
import { codeTheme } from './code-theme.ts';
import { STATIONS } from './stations.ts';

export interface CodeSteps {
	wide: KeyedTokensInfo[];
	phone: KeyedTokensInfo[];
}

/**
 * The stations' code as magic-move steps, at build time: each token keyed so the one code block can morph from a
 * station's code to the next. Phones get each station's phone version.
 */
export async function codeSteps(): Promise<CodeSteps> {
	const shiki = await createHighlighter({ themes: [codeTheme], langs: ['go'] });
	const compile = (codes: string[]) => {
		const machine = createMagicMoveMachine((code) => codeToKeyedTokens(shiki, code, { lang: 'go', theme: codeTheme.name! }));
		return codes.map((code) => machine.commit(code).current);
	};
	return {
		wide: compile(STATIONS.map((s) => s.code)),
		phone: compile(STATIONS.map((s) => s.phone?.code ?? s.code)),
	};
}
