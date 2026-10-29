import { syncTokenKeys, toKeyedTokens } from '@shikijs/magic-move/core';
import { MagicMoveRenderer } from '@shikijs/magic-move/renderer';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import CODE from 'virtual:code-steps';
import { STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { mountMap } from './map.ts';
import { STEPS, STORY_TEXT, type Cut, type Step } from './steps.ts';

// The home page opens on the intro, beside the whole map standing still. From there the page snaps a step at a time:
// each scroll brings the next step, which plays its chapter on the map once, at the map's own pace, while the one code
// block morphs into that chapter's code and lights the lines each caption is about. Under reduced motion a step shows
// its last frame. Past the last step the page scrolls on as usual, through the rest of the home page.

const MORPH = { duration: 700, stagger: 0.02, lineNumbers: false };
// A wheel turn moves one step once it has travelled THRESHOLD px; a pause of QUIET ms ends a turn, so each notch of a
// wheel turned notch by notch is a step. A wheel that keeps spinning, as a free-spinning mouse wheel does, moves on a
// step every COOLDOWN ms, about as long as the page takes to scroll one, for as long as it keeps half its speed; a
// swipe's momentum, which dies away, does not.
const COOLDOWN = 650;
const THRESHOLD = 40;
const QUIET = 200;
/** The snap positions: the intro, then one per step. */
const COUNT = STEPS.length + 1;
/** How far past the last step, in steps, the page stops snapping, and how far back above it it snaps again. */
const SLACK = 0.02;

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
	/** The compiled code it shows, or -1 before any. */
	at: number;
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
	el.querySelector('.st-rows')!.append(...rows);
	return { el, cut, renderer, steps: CODE[cut], previous: toKeyedTokens('', []), rows, at: -1 };
}

/** Moves a code block to compiled code k: a morph, or at once on its first code and under reduced motion. */
function morph(code: Code, k: number) {
	const animate = code.at >= 0 && !reduce.matches;
	const { from, to } = syncTokenKeys(code.previous, code.steps[k], MORPH);
	code.previous = to;
	code.at = k;
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

const root = $('.st');
const stage = $('.st-stage');
const intro = $('.st-intro');
const stepView = $('.st-step');
const codes = [codeBlock('.st-code-wide', 'wide'), codeBlock('.st-code-phone', 'phone')];
const count = $('.st-count');
const title = $('.st-title');
const notes = $('.st-notes');
const source = $<HTMLAnchorElement>('.st-source a');
const track = $('.st-track');
$('.st-all').innerHTML = STORY_TEXT;
track.append(...Array.from({ length: COUNT }, () => Object.assign(document.createElement('div'), { className: 'st-snap' })));

/** The snap position shown: 0 for the intro, k + 1 for step k. */
let at = -1;
let step = -1;
let caption = -1;
let settled = 0;

/** A caption's point: a ring whose arc fills while the caption plays, and whose core shows once it is done. */
const POINT =
	'<svg class="st-point" viewBox="0 0 14 14" aria-hidden="true"><circle class="st-ring" cx="7" cy="7" r="6"/>' +
	'<circle class="st-arc" cx="7" cy="7" r="6" pathLength="1"/><circle class="st-core" cx="7" cy="7" r="3"/></svg>';

function setCaption(i: number) {
	if (i === caption) return;
	caption = i;
	[...notes.children].forEach((li, n) => {
		li.toggleAttribute('data-on', n === i);
		li.toggleAttribute('data-done', n < i);
		if (n !== i) (li as HTMLElement).style.setProperty('--p', n < i ? '1' : '0');
	});
	for (const code of codes) {
		// A step's code may change with its caption, as chapter 5's names each strategy in turn.
		if (STEPS[step].code[i] !== code.at) morph(code, STEPS[step].code[i]);
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

/** Shows the intro, beside the whole map standing still. */
function showIntro() {
	step = -1;
	caption = -1;
	root.dataset.at = 'intro';
	intro.hidden = false;
	stepView.hidden = true;
	map.seek(STATIC_AT);
}

/** Shows step k: its text, its code, and its chapter playing on the map, or its last frame under reduced motion. */
function show(k: number) {
	step = k;
	caption = -1;
	root.dataset.at = 'step';
	intro.hidden = true;
	stepView.hidden = false;
	const s = STEPS[k];
	count.innerHTML = `<span>${k + 1}</span> / ${STEPS.length}`;
	title.textContent = s.title;
	notes.innerHTML = s.notes.map((note, i) => `<li><button type="button" data-i="${i}">${POINT}${note}</button></li>`).join('');
	source.textContent = s.source.label;
	source.href = s.source.href;
	for (const code of codes) morph(code, s.code[0]);
	setCaption(0);
	// The code block reaches its new height when the morph ends: keep the lit lines in view then too.
	clearTimeout(settled);
	settled = window.setTimeout(() => codes.forEach((code) => follow(code, s, caption)), MORPH.duration);
	if (reduce.matches) map.seek(s.from + s.ms);
	else map.play(s.from, s.ms);
}

// A caption is a button: it plays the step again from that caption to the step's end, and the code, the lit lines and
// the points follow the map's clock there, as when the step first played. Under reduced motion it shows the caption's
// last frame.
notes.addEventListener('click', (e) => {
	const i = Number((e.target as Element).closest('button')?.dataset.i);
	if (step < 0 || Number.isNaN(i)) return;
	const s = STEPS[step];
	if (reduce.matches) map.seek(s.from + (s.captions[i + 1] ?? s.ms) - 1);
	else map.play(s.from + s.captions[i], s.ms - s.captions[i]);
});

// The stage is pinned under the header, and each snap position is one stage's height down the track.
const height = () => (track.firstElementChild as HTMLElement).offsetHeight;
const top = () => track.getBoundingClientRect().top + scrollY - parseFloat(getComputedStyle(stage).top);
/** Where the page is, in snap positions: 0 at the intro, COUNT - 1 at the last step, more past it. */
const position = () => (scrollY - top()) / height();
const nearest = () => Math.min(COUNT - 1, Math.max(0, Math.round(position())));

/**
 * The page snaps only while the reader is in the story. Past the last step it scrolls freely, and it snaps again once
 * the reader comes back above it. Between the two it keeps what it had.
 */
function snapping(on?: boolean) {
	const p = position();
	on ??= p < COUNT - 1 - SLACK ? true : p > COUNT - 1 + SLACK ? false : document.documentElement.classList.contains('st-snap');
	document.documentElement.classList.toggle('st-snap', on);
}

/** The position a wheel scroll is heading to, until it gets there, so a turn made on the way goes on from it. */
let target = -1;

/** Which way the page last moved: 1 down, -1 up. */
let way = 0;
let lastY = scrollY;

let frameId = 0;
const tick = () => {
	frameId = 0;
	if (scrollY !== lastY) way = Math.sign(scrollY - lastY);
	lastY = scrollY;
	snapping();
	const k = nearest();
	if (k !== at) {
		at = k;
		if (k === 0) showIntro();
		else show(k - 1);
	}
	if (Math.abs(position() - target) < 1 / height()) target = -1;
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

/** Leaves the story downward: snapping stops first, so the page can scroll past the last step at all. */
function leave() {
	snapping(false);
	target = -1;
	scrollTo({ top: top() + COUNT * height(), behavior: motion() });
}

// Keyboard and touch scroll natively and snap a step at a time. The wheel is taken over, so that one turn is one step:
// snapping alone lets a short notch fall back to where it was. At the last step, the wheel and the keys that scroll
// down leave the story.
let stepped = -Infinity; // when the wheel last moved a step
let wheeled = -Infinity; // its last event
let travel = 0; // how far it has turned since the last step
let speed = 0; // how fast it turns, smoothed over a few events
let peak = 0; // the fastest it has turned since it last moved a step
addEventListener(
	'wheel',
	(e) => {
		if (e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
		const p = position();
		// Below the last step, the page is the reader's to scroll, except for the turn back up into the story.
		const below = p > COUNT - 1 + SLACK;
		if (below && !(e.deltaY < 0 && p <= COUNT + SLACK)) return;
		const from = target >= 0 ? target : below ? COUNT : nearest();
		const next = from + Math.sign(e.deltaY);
		if (next < 0) return;
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
		if (next >= COUNT) return leave();
		snapping(true);
		target = next;
		scrollTo({ top: top() + next * height(), behavior: motion() });
	},
	{ passive: false },
);
// A scroll that ends between the last step and the page below it, with the stage half gone, goes on to the one it was
// heading for.
addEventListener('scrollend', () => {
	target = -1;
	const p = position();
	if (p <= COUNT - 1 + SLACK || p >= COUNT - SLACK) return;
	if (way < 0) snapping(true);
	scrollTo({ top: top() + (way < 0 ? COUNT - 1 : COUNT) * height(), behavior: motion() });
});
// End goes to the foot of the page, so it stops the snapping first. The keys that scroll down leave the story at its
// last step, and the keys that scroll up, just below it, go back to the last step, as the wheel does.
addEventListener('keydown', (e) => {
	if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
	const on = e.target as Element;
	if (on.closest('input, textarea, select, [contenteditable]')) return;
	if (e.key === 'End') return snapping(false);
	if (e.key === ' ' && on.closest('button, a')) return;
	const down = e.key === 'ArrowDown' || e.key === 'PageDown' || (e.key === ' ' && !e.shiftKey);
	const up = e.key === 'ArrowUp' || e.key === 'PageUp' || (e.key === ' ' && e.shiftKey);
	const p = position();
	if (down && Math.abs(p - (COUNT - 1)) <= SLACK) {
		e.preventDefault();
		leave();
	} else if (up && p > COUNT - 1 + SLACK && p <= COUNT + SLACK) {
		e.preventDefault();
		snapping(true);
		target = COUNT - 1;
		scrollTo({ top: top() + (COUNT - 1) * height(), behavior: motion() });
	}
});
