import type { ReactNode } from 'react';

/** A caption's marks, as chapters.ts writes them: *word* for what it is about, !word! for a failure, #Name# for a store. */
export function marks(text: string): ReactNode[] {
	return text.split(/([*!#][^*!#]+[*!#])/).map((part, i) => {
		const kind = part[0];
		if (part.length < 3 || part.at(-1) !== kind || !'*!#'.includes(kind)) return part;
		const word = part.slice(1, -1);
		if (kind === '*') return <em key={i}>{word}</em>;
		if (kind === '!') return <strong key={i} className="hp-fail">{word}</strong>;
		return (
			<span key={i} className={`hp-brand hp-brand-${word.toLowerCase()}`}>
				{word}
			</span>
		);
	});
}
