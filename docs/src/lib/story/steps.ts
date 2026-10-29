import { CAMERA, CHAPTERS, TOTAL, plain, starts, type Caption } from '../heromap/chapters.ts';
import { STATIONS } from './stations.ts';

// Step 1 also plays the zoom from the whole map into chapter 1, which closes the loop (the finale's last CAMERA ms);
// the finale holds the whole map up to where that zoom starts. So every step opens on a camera move CAMERA ms long.

/** A code block's cut: the wide one, or the phone one with its long lines broken. */
export type Cut = 'wide' | 'phone';

export interface Step {
	/** The step's stretch of the map's loop: where it starts, and how long it plays. */
	from: number;
	ms: number;
	/** When each of its captions starts, in ms from `from`. */
	captions: number[];
	/** The code each caption shows, as its place in the compiled list (`compile.ts`): the same in both cuts. */
	code: number[];
	/** The code lines each caption lights, and how many lines the code has, in each cut. */
	lines: Record<Cut, number[][]>;
	count: Record<Cut, number>;
	title: string;
	/** Each caption as HTML, its marks turned into elements. */
	notes: string[];
	/** The full program the code comes from. */
	source: { label: string; href: string };
}

const escape = (s: string) => s.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);
const words = (c: Caption) => (c.swap ? `${c.text} to ${c.swap.text}` : c.text);

/** A caption's marks, as chapters.ts writes them: *word* for what it is about, !word! for a failure, #Name# for a store. */
export function marks(text: string): string {
	return text
		.split(/([*!#][^*!#]+[*!#])/)
		.map((part) => {
			const kind = part[0];
			if (part.length < 3 || part.at(-1) !== kind || !'*!#'.includes(kind)) return escape(part);
			const word = escape(part.slice(1, -1));
			if (kind === '*') return `<em>${word}</em>`;
			if (kind === '!') return `<strong class="fail">${word}</strong>`;
			return `<span class="brand-${word.toLowerCase()}">${word}</span>`;
		})
		.join('');
}

// Where each station's first code sits in the compiled list, which holds every station's code and then its `then`.
const firsts = STATIONS.map((_, i) => STATIONS.slice(0, i).reduce((n, s) => n + 1 + (s.then?.length ?? 0), 0));

/** Each chapter as a step of the story: its stretch of the loop, its captions' times, and the code lines they light. */
export const STEPS: Step[] = CHAPTERS.map((chapter, i) => {
	const station = STATIONS[i];
	const phone = station.phone ?? station;
	const lead = i === 0 ? CAMERA : 0;
	return {
		from: i === 0 ? TOTAL - CAMERA : starts()[i],
		ms: lead + chapter.duration - (i === CHAPTERS.length - 1 ? CAMERA : 0),
		captions: chapter.captions.map((c) => lead + c.at),
		code: chapter.captions.map((_, c) => firsts[i] + Math.min(c, station.then?.length ?? 0)),
		lines: { wide: station.lines, phone: phone.lines },
		count: { wide: station.code.split('\n').length, phone: phone.code.split('\n').length },
		title: chapter.title,
		notes: chapter.captions.map((c) => marks(words(c))),
		source: station.source,
	};
});

/** The story as text: each step's title, captions and code, in order. */
export const STORY_TEXT = `<ol>${CHAPTERS.map(
	(chapter, i) =>
		`<li><h2>${escape(chapter.title)}</h2><p>${escape(chapter.captions.map((c) => plain(words(c))).join('. '))}.</p>` +
		`<pre><code>${escape(STATIONS[i].code)}</code></pre></li>`,
).join('')}</ol>`;
