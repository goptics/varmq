import { codeToKeyedTokens, createMagicMoveMachine } from '@shikijs/magic-move/core';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import { createHighlighter } from 'shiki';
import { STATIONS } from './stations.ts';

export interface CodeSteps {
	wide: KeyedTokensInfo[];
	phone: KeyedTokensInfo[];
}

/** The code block's colours: GitHub's dark theme. */
const THEME = 'github-dark-default';

/**
 * The stations' code as magic-move steps, at build time: each token keyed so the one code block can morph from a
 * station's code to the next. Each station's code comes in order, then the code it changes to within its step
 * (`then`). Phones get each station's phone version.
 */
export async function codeSteps(): Promise<CodeSteps> {
	const shiki = await createHighlighter({ themes: [THEME], langs: ['go'] });
	const compile = (codes: string[]) => {
		const machine = createMagicMoveMachine((code) => codeToKeyedTokens(shiki, code, { lang: 'go', theme: THEME }));
		return codes.map((code) => machine.commit(code).current);
	};
	return {
		wide: compile(STATIONS.flatMap((s) => [s.code, ...(s.then ?? []).map((t) => t.code)])),
		phone: compile(STATIONS.flatMap((s) => [s.phone?.code ?? s.code, ...(s.then ?? []).map((t) => t.phone ?? t.code)])),
	};
}
