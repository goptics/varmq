import '@shikijs/magic-move/style.css';
import './style.css';
import { syncTokenKeys, toKeyedTokens } from '@shikijs/magic-move/core';
import { MagicMoveRenderer } from '@shikijs/magic-move/renderer';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import CODE from 'virtual:code-steps';
import { TOTAL } from '../lib/heromap/chapters.ts';
import { mountMap } from './map.ts';
import { STEPS, STORY_TEXT, type Cut, type Step } from './steps.ts';

// The page snaps a step at a time: each scroll brings the next step, which plays its chapter on the map once, at the
// map's own pace, while the one code block morphs into that chapter's code and lights the lines each caption is about.
// Under reduced motion a step shows its last frame.

const MORPH = { duration: 700, stagger: 0.02, lineNumbers: false };
// A wheel turn moves one step once it has travelled THRESHOLD px; a pause of QUIET ms ends a turn, so each notch of a
// wheel turned notch by notch is a step. A wheel that keeps spinning, as a free-spinning mouse wheel does, moves on a
// step every COOLDOWN ms, about as long as the page takes to scroll one, for as long as it keeps half its speed; a
// swipe's momentum, which dies away, does not.
const COOLDOWN = 650;
const THRESHOLD = 40;
const QUIET = 200;

const reduce = matchMedia('(prefers-reduced-motion: reduce)');
const motion = (): ScrollBehavior => (reduce.matches ? 'auto' : 'smooth');
const $ = <T extends HTMLElement = HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

/** One of the two code blocks: the wide cut, or the phone cut. CSS shows the one that fits the room. */
interface Code {
	el: HTMLElement;
	cut: Cut;
	renderer: MagicMoveRenderer;
	steps: KeyedTokensInfo[];
	previous: KeyedTokensInfo;
	/** One per line, over the code: lit or plain. */
	rows: HTMLElement[];
}

function codeBlock(selector: string, cut: Cut): Code {
	const el = $(selector);
	const renderer = new MagicMoveRenderer(el.querySelector('pre')!);
	Object.assign(renderer.options, MORPH);
	const rows = Array.from({ length: Math.max(...STEPS.map((s) => s.count[cut])) }, (_, n) => {
		const row = document.createElement('i');
		row.style.setProperty('--n', String(n));
		return row;
	});
	el.querySelector('.rows')!.append(...rows);
	return { el, cut, renderer, steps: CODE[cut], previous: toKeyedTokens('', []), rows };
}

/** Moves a code block to step k's code: a morph, or at once on the first show and under reduced motion. */
function morph(code: Code, k: number, animate: boolean) {
	const { from, to } = syncTokenKeys(code.previous, code.steps[k], MORPH);
	code.previous = to;
	if (!animate) return code.renderer.replace(to);
	code.renderer.replace(from);
	void code.renderer.render(to);
}

/** Lights the lines the caption is about. */
function light(code: Code, step: Step, caption: number) {
	const lit = step.lines[code.cut][caption] ?? [];
	code.rows.forEach((row, n) => {
		row.dataset.state = n >= step.count[code.cut] ? 'none' : lit.includes(n + 1) ? 'lit' : '';
	});
}

/**
 * Short of room, a code block shows only some of its lines: keep the lit ones in view, with the line above them for
 * context when it fits.
 */
function follow(code: Code, step: Step, caption: number) {
	const lines = step.lines[code.cut][caption];
	if (!code.el.offsetHeight || !lines?.length) return;
	const style = getComputedStyle(code.el);
	const lh = parseFloat(style.getPropertyValue('--lh'));
	const shows = (code.el.clientHeight - 2 * parseFloat(style.getPropertyValue('--pad'))) / lh;
	const first = Math.min(...lines);
	const context = Math.max(...lines) - first + 1 < shows ? 1 : 0;
	code.el.scrollTo({ top: (first - 1 - context) * lh, behavior: motion() });
}

const codes = [codeBlock('.code-wide', 'wide'), codeBlock('.code-phone', 'phone')];
const count = $('.count');
const title = $('.title');
const notes = $('.notes');
const source = $<HTMLAnchorElement>('.source a');
const track = $('.track');
$('.story-text').innerHTML = STORY_TEXT;
track.append(...STEPS.map(() => Object.assign(document.createElement('div'), { className: 'snap' })));

let step = -1;
let caption = -1;
let settled = 0;

/** A caption's point: a ring whose arc fills while the caption plays, and whose core shows once it is done. */
const POINT =
	'<svg class="point" viewBox="0 0 14 14" aria-hidden="true"><circle class="track" cx="7" cy="7" r="6"/>' +
	'<circle class="arc" cx="7" cy="7" r="6" pathLength="1"/><circle class="core" cx="7" cy="7" r="3"/></svg>';

function setCaption(i: number) {
	if (i === caption) return;
	caption = i;
	[...notes.children].forEach((li, n) => {
		li.toggleAttribute('data-on', n === i);
		li.toggleAttribute('data-done', n < i);
		if (n !== i) (li as HTMLElement).style.setProperty('--p', n < i ? '1' : '0');
	});
	for (const code of codes) {
		light(code, STEPS[step], i);
		follow(code, STEPS[step], i);
	}
}

const map = mountMap($('varmq-map'), (t) => {
	if (step < 0) return;
	const s = STEPS[step];
	const local = (((t - s.from) % TOTAL) + TOTAL) % TOTAL;
	const i = Math.max(0, s.captions.findLastIndex((at) => at <= local));
	setCaption(i);
	// The lit caption's ring fills from its start to the next caption's, or to the step's end, where it is done too.
	const start = s.captions[i];
	const p = Math.min(1, Math.max(0, (local - start) / ((s.captions[i + 1] ?? s.ms) - start)));
	const li = notes.children[i] as HTMLElement | undefined;
	li?.style.setProperty('--p', String(p));
	li?.toggleAttribute('data-done', p >= 1);
});

/** Shows step k: its text, its code, and its chapter playing on the map, or its last frame under reduced motion. */
function show(k: number) {
	const first = step < 0;
	step = k;
	caption = -1;
	const s = STEPS[k];
	count.innerHTML = `<span>${k + 1}</span> / ${STEPS.length}`;
	title.textContent = s.title;
	notes.innerHTML = s.notes.map((note) => `<li>${POINT}${note}</li>`).join('');
	source.textContent = s.source.label;
	source.href = s.source.href;
	for (const code of codes) morph(code, k, !first && !reduce.matches);
	setCaption(0);
	// The code block reaches its new height when the morph ends: keep the lit lines in view then too.
	clearTimeout(settled);
	settled = window.setTimeout(() => codes.forEach((code) => follow(code, s, caption)), MORPH.duration);
	if (reduce.matches) map.seek(s.from + s.ms);
	else map.play(s.from, s.ms);
}

const height = () => (track.firstElementChild as HTMLElement).offsetHeight;
const top = () => track.getBoundingClientRect().top + scrollY;
const stepAt = () => Math.min(STEPS.length - 1, Math.max(0, Math.round((scrollY - top()) / height())));

/** The step a wheel scroll is heading to, until it gets there, so a turn made on the way goes on from it. */
let target = -1;

let frameId = 0;
const tick = () => {
	frameId = 0;
	const k = stepAt();
	if (k !== step) show(k);
	if (Math.abs(scrollY - top() - target * height()) < 1) target = -1;
};
const schedule = () => {
	frameId ||= requestAnimationFrame(tick);
};
addEventListener('scroll', schedule, { passive: true });
addEventListener('resize', () => {
	schedule();
	if (step >= 0) for (const code of codes) follow(code, STEPS[step], caption);
});
tick();

// Keyboard and touch scroll natively and snap a step at a time. The wheel is taken over, so that one turn is one step:
// snapping alone lets a short notch fall back to where it was.
let stepped = -Infinity; // when the wheel last moved a step
let wheeled = -Infinity; // its last event
let travel = 0; // how far it has turned since the last step
let speed = 0; // how fast it turns, smoothed over a few events
let peak = 0; // the fastest it has turned since it last moved a step
addEventListener(
	'wheel',
	(e) => {
		if (e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
		const next = (target < 0 ? stepAt() : target) + Math.sign(e.deltaY);
		if (next < 0 || next >= STEPS.length) return;
		e.preventDefault();
		const delta = e.deltaY * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? innerHeight : 1);
		const fresh = e.timeStamp - wheeled > QUIET;
		wheeled = e.timeStamp;
		if (fresh || Math.sign(delta) !== Math.sign(travel)) travel = 0;
		speed = fresh ? Math.abs(delta) : speed * 0.7 + Math.abs(delta) * 0.3;
		peak = fresh ? speed : Math.max(peak, speed);
		if (!fresh && (e.timeStamp - stepped < COOLDOWN || speed < peak / 2)) return;
		travel += delta;
		if (Math.abs(travel) < THRESHOLD) return;
		travel = 0;
		peak = speed;
		stepped = e.timeStamp;
		target = next;
		scrollTo({ top: top() + next * height(), behavior: motion() });
	},
	{ passive: false },
);
addEventListener('scrollend', () => {
	target = -1;
});
