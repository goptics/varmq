import { syncTokenKeys, toKeyedTokens } from '@shikijs/magic-move/core';
import { MagicMoveRenderer } from '@shikijs/magic-move/renderer';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import CODE from 'virtual:code-steps';
import { STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { mountMap } from './map.ts';
import { STEPS, STOPS, STORY_TEXT, until, type Cut, type Step } from './steps.ts';
import { wheelSteps } from './wheel.ts';

// The home page opens on the intro, beside the whole map running as in the last step. From there the page snaps a
// caption at a time: each scroll plays the next caption of a step on the map, at the map's own pace, while the one code
// block morphs into that caption's code and lights the lines it is about. A scroll back up within a step plays its last
// caption backward; a step reached from another one plays from its start. Under reduced motion a caption shows its last
// frame. Past the last step the page scrolls on as usual, through the rest of the home page.

const MORPH = { duration: 700, stagger: 0.02, lineNumbers: false };
/** The snap positions: the intro, then one per caption (STOPS). */
const COUNT = STOPS.length + 1;
/**
 * The whole map's run, as the intro and the last step loop it: from STATIC_AT, where the last step's camera has pulled
 * back, to that step's end, where step 1's zoom begins; held AGAIN.again ms between runs.
 */
const RUN_END = STEPS[STEPS.length - 1].from + STEPS[STEPS.length - 1].ms;
const AGAIN = { again: 1500 };
/** How far past the last step, in steps, the page stops snapping, and how far back above it it snaps again. */
const SLACK = 0.02;

const reduce = matchMedia('(prefers-reduced-motion: reduce)');
/** Whether the reader has paused the map with its Pause button. */
let paused = false;
/** Whether the map holds still: a step shows its last frame, as under reduced motion, rather than playing. */
const still = () => reduce.matches || paused;
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
const h1 = $('.st-intro h1');
const stepView = $('.st-step');
const codes = [codeBlock('.st-code-wide', 'wide'), codeBlock('.st-code-phone', 'phone')];
const count = $('.st-count');
const title = $('.st-title');
const notes = $('.st-notes');
const text = $('.st-text');
const source = $<HTMLAnchorElement>('.st-source a');
const track = $('.st-track');
const skip = $<HTMLButtonElement>('.st-skip');
const pause = $<HTMLButtonElement>('.st-pause');
const live = $('.st-live');
/** The skip's outline: a segment per step, each a track and the fill over it. */
const segments = STEPS.map((_, n) => {
	const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
	g.style.setProperty('--n', String(n));
	g.innerHTML = '<circle cx="24" cy="24" r="22" pathLength="100"/><circle class="st-fill" cx="24" cy="24" r="22" pathLength="100"/>';
	return g;
});
$('.st-skip-ring').append(...segments);
$('.st-all').insertAdjacentHTML('beforeend', STORY_TEXT);
track.append(...Array.from({ length: COUNT }, () => Object.assign(document.createElement('div'), { className: 'st-stop' })));

/** The snap position shown: 0 for the intro, n + 1 for STOPS[n]. */
let at = -1;
let step = -1;
let caption = -1;
/** The caption the step stops at: the map may run up to its end, and no further. */
let limit = 0;
let settled = 0;

/** A caption's point: a ring whose arc fills while the caption plays, and whose core shows once it is done. */
const POINT =
	'<svg class="st-point" viewBox="0 0 14 14" aria-hidden="true"><circle class="st-ring" cx="7" cy="7" r="6"/>' +
	'<circle class="st-arc" cx="7" cy="7" r="6" pathLength="1"/><circle class="st-core" cx="7" cy="7" r="3"/></svg>';

/**
 * Marks a caption done or not, for the eye (data-done) and for screen readers, whose description of its button then
 * says it has played.
 */
function done(li: Element, on: boolean) {
	if (li.hasAttribute('data-done') === on) return;
	li.toggleAttribute('data-done', on);
	li.firstElementChild!.setAttribute('aria-describedby', on ? 'st-done st-does' : 'st-does');
}

function setCaption(i: number) {
	if (i === caption) return;
	caption = i;
	[...notes.children].forEach((li, n) => {
		li.toggleAttribute('data-on', n === i);
		if (n === i) li.firstElementChild!.setAttribute('aria-current', 'step');
		else li.firstElementChild!.removeAttribute('aria-current');
		done(li, n < i);
		if (n !== i) (li as HTMLElement).style.setProperty('--p', n < i ? '1' : '0');
	});
	// Where only the lit caption shows (story.css), a focused caption that goes out of sight hands focus to the lit one.
	const focused = document.activeElement;
	if (focused && notes.contains(focused) && getComputedStyle(focused).visibility === 'hidden') {
		notes.querySelector<HTMLElement>('[data-on] button')?.focus({ preventScroll: true });
	}
	for (const code of codes) {
		// A step's code may change with its caption, as chapter 5's names each strategy in turn.
		if (STEPS[step].code[i] !== code.at) morph(code, STEPS[step].code[i]);
		light(code, STEPS[step], i);
		follow(code, STEPS[step], i);
	}
}

/** Time t of the loop as ms into step s. */
const into = (s: Step, t: number) => (((t - s.from) % TOTAL) + TOTAL) % TOTAL;

const map = mountMap($('varmq-map'), (t) => {
	if (step < 0) return;
	const s = STEPS[step];
	const local = into(s, t);
	const i = Math.min(limit, Math.max(0, s.captions.findLastIndex((at) => at <= local)));
	setCaption(i);
	// The lit caption's ring fills from its start to the next caption's start, or to the step's end, where it is done.
	const p = Math.min(1, Math.max(0, (local - s.captions[i]) / (until(s, i) - s.captions[i])));
	const li = notes.children[i] as HTMLElement | undefined;
	li?.style.setProperty('--p', String(p));
	if (li) done(li, p >= 1);
	// A step's segment fills once its last caption is done.
	const played = i === s.captions.length - 1 && p >= 1;
	segments.forEach((g, n) => g.toggleAttribute('data-done', n < step || (n === step && played)));
});

/** The step to say once the page rests on it, and the step said last. */
let news = '';
let said = '';
let saying = 0;
/** Says the step once the page has rested on it a moment, so a scroll that passes several steps says only the last. */
function rest() {
	if (!news) return;
	clearTimeout(saying);
	saying = window.setTimeout(() => {
		if (news !== said) live.textContent = said = news;
		news = '';
	}, 400);
}
function tell(text: string) {
	news = text;
	rest();
}

/** Plays the map on from where Pause held it, as the intro or step shown would have. */
function resume() {
	const t = map.now();
	const loop = { ...AGAIN, restart: STATIC_AT };
	if (step < 0) {
		// The intro's run, or the rewind from step 1 on its way back to it.
		if (t >= STATIC_AT && t <= RUN_END) map.play(t, RUN_END - t, loop);
		else map.rewind(RUN_END, STEPS[0].ms, () => map.play(STATIC_AT, RUN_END - STATIC_AT, AGAIN));
		return;
	}
	const s = STEPS[step];
	const left = until(s, limit) - into(s, t);
	if (step === STEPS.length - 1) {
		// Out of view below the story, the loop rests (tick).
		if (!away) map.play(t, Math.max(0, left), loop);
	} else if (left > 0) map.play(t, left);
	else if (left < 0) map.rewind(s.from + until(s, limit), -left);
}

pause.addEventListener('click', () => {
	paused = !paused;
	pause.setAttribute('aria-pressed', String(paused));
	pause.textContent = paused ? 'Play' : 'Pause';
	root.toggleAttribute('data-paused', paused);
	if (paused) map.seek(map.now());
	else resume();
});

/**
 * Shows the intro, beside the whole map. The map runs as in the last step, from where its camera has pulled back, holds
 * a moment once it is still, and runs again, until the reader scrolls on. Back up from step 1, the map first plays
 * step 1 backward to the run's end, and the run starts again from there at once. Under reduced motion it stands still.
 */
function showIntro(back = false) {
	step = -1;
	caption = -1;
	root.dataset.at = 'intro';
	// Focus in the step, which is about to go, moves to the h1.
	const focused = stepView.contains(document.activeElement);
	stepView.hidden = true;
	if (focused) h1.focus({ preventScroll: true });
	news = said = '';
	const run = () => map.play(STATIC_AT, RUN_END - STATIC_AT, AGAIN);
	if (still()) map.seek(STATIC_AT);
	else if (back) map.rewind(RUN_END, STEPS[0].ms, run);
	else run();
}

/**
 * Shows step k: its text, its code, and its chapter playing on the map from its start, every ring empty, up to the end
 * of caption c. Going back up from a later step, the map plays its way back to the end of caption c, at most the next
 * step's length of it, and holds there, every ring full. Under reduced motion it shows that frame at once.
 */
function show(k: number, c: number, back = false) {
	step = k;
	caption = -1;
	limit = c;
	// The intro's h1 stays, visually hidden (story.css), so the page keeps its h1 on every step.
	root.dataset.at = 'step';
	stepView.hidden = false;
	// Focus on the h1, now hidden, moves to the step's title.
	if (document.activeElement === h1) title.focus({ preventScroll: true });
	const s = STEPS[k];
	count.innerHTML = `<span>${k + 1}</span> / ${STEPS.length}`;
	title.textContent = s.title;
	tell(`Step ${k + 1} of ${STEPS.length}: ${s.title}`);
	const focused = notes.contains(document.activeElement);
	notes.innerHTML = s.notes
		.map((note, i) => `<li><button type="button" data-i="${i}" aria-describedby="st-does">${POINT}${note}</button></li>`)
		.join('');
	source.textContent = s.source.label;
	source.href = s.source.href;
	const first = back ? c : 0;
	for (const code of codes) morph(code, s.code[first]);
	setCaption(first);
	// Where the text column scrolls (story.css, 400% zoom), a step opens at its top, or reached going up, at its end.
	if (getComputedStyle(text).overflowY === 'auto') text.scrollTop = back ? text.scrollHeight : 0;
	// Focus on a caption that was just replaced moves to the new step's caption shown: its first, or going up its last.
	if (focused) notes.querySelector<HTMLElement>('[data-on] button')!.focus({ preventScroll: true });
	// The code block reaches its new height when the morph ends: keep the lit lines in view then too.
	clearTimeout(settled);
	settled = window.setTimeout(() => codes.forEach((code) => follow(code, s, caption)), MORPH.duration);
	if (still()) map.seek(s.from + until(s, c));
	else if (back) map.rewind(s.from + until(s, c), STEPS[k + 1]?.ms ?? s.ms);
	else map.play(s.from, until(s, c), k === STEPS.length - 1 ? { ...AGAIN, restart: STATIC_AT } : undefined);
}

/**
 * Moves the shown step to its caption c: down, the map plays caption c; up, it plays caption c + 1 backward, or, if the
 * step is still playing up to there from its start, plays on to the end of caption c.
 */
function move(c: number, back: boolean) {
	const s = STEPS[step];
	limit = c;
	const left = until(s, c) - into(s, map.now());
	if (still()) map.seek(s.from + until(s, c));
	else if (back && left > 0) map.play(map.now(), left);
	// From wherever it is in the step: a rewind still on its way back from the caption below goes on from there.
	else if (back) map.rewind(s.from + until(s, c), s.ms);
	else map.play(s.from + s.captions[c], until(s, c) - s.captions[c]);
}

// A caption is a button: it scrolls to that caption's stop, or plays it again if the page is there.
notes.addEventListener('click', (e) => {
	const i = Number((e.target as Element).closest('button')?.dataset.i);
	if (step < 0 || Number.isNaN(i)) return;
	const k = STOPS.findIndex((stop) => stop.step === step && stop.caption === i) + 1;
	// The last step's one caption plays its loop again, as show() starts it.
	if (k === at) step === STEPS.length - 1 ? show(step, i) : move(i, false);
	else go(k);
});

// The stage is pinned under the header, and each snap position is one stage's height down the track.
const height = () => (track.firstElementChild as HTMLElement).offsetHeight;
const top = () => track.getBoundingClientRect().top + scrollY - parseFloat(getComputedStyle(stage).top);
/** Where the page is, in snap positions: 0 at the intro, COUNT - 1 at the last step, more past it. */
const position = () => (scrollY - top()) / height();
const nearest = () => Math.min(COUNT - 1, Math.max(0, Math.round(position())));

/** The position a wheel or key scroll is heading to, until it gets there, so a turn made on the way goes on from it. */
let target = -1;

/**
 * The page snaps only while the reader is in the story above its last step. From the last step down it scrolls as
 * usual, but for the way back up from there into the story.
 */
function snapping(on?: boolean) {
	on ??= position() < COUNT - 1 - SLACK || (target >= 0 && target < COUNT - 1);
	document.documentElement.classList.toggle('st-snap', on);
}

/** Whether the stage has scrolled out of view below the story. */
let away = false;
let frameId = 0;
const tick = () => {
	frameId = 0;
	snapping();
	const k = nearest();
	// On the way to a stop further than the next, as a skip goes, the stops passed are not shown.
	if (k !== at && (target < 0 || k === target)) {
		// The next or the previous caption of the same step moves to it. A step reached going down plays from its start;
		// going up, the map plays back to where it stops, and holds.
		const was = STOPS[at - 1];
		const stop = STOPS[k - 1];
		const up = k < at;
		at = k;
		if (!stop) showIntro(up);
		else if (was?.step === stop.step && Math.abs(was.caption - stop.caption) === 1) {
			move(stop.caption, stop.caption < was.caption);
		} else show(stop.step, stop.caption, up);
		// The skip shows from step 1 to the step before the last.
		skip.disabled = !stop || stop.step === STEPS.length - 1;
		// Focus on the skip, which has just gone, moves to the last step's title, as a skip lands, or to the intro's h1.
		if (skip.disabled && document.activeElement === skip) (stop ? title : h1).focus({ preventScroll: true });
	}
	// Out of view below the story, the last step's loop rests on its end; back in view, the step plays from its start.
	if (away !== position() >= COUNT) {
		away = !away;
		if (at === COUNT - 1 && !still()) {
			if (away) map.seek(RUN_END);
			else show(STEPS.length - 1, 0);
		}
	}
	if (Math.abs(position() - target) < 1 / height()) target = -1;
};
const schedule = () => {
	frameId ||= requestAnimationFrame(tick);
};
addEventListener('scroll', schedule, { passive: true });
addEventListener('scroll', rest, { passive: true });
addEventListener('resize', () => {
	schedule();
	if (step >= 0) for (const code of codes) follow(code, STEPS[step], caption);
});
tick();

/** Scrolls to snap position k, snapping on the way if k is in the story above its last step. */
function go(k: number) {
	target = k;
	snapping();
	scrollTo({ top: top() + k * height(), behavior: motion() });
}

// Keyboard and touch scroll natively and snap a stop at a time. The wheel is taken over, so that one turn is one stop
// (wheel.ts): snapping alone lets a short notch fall back to where it was. From the last step down, the page scrolls as
// usual with every input; on the way back up, the story takes over again at the last step.
const turn = wheelSteps();
addEventListener(
	'wheel',
	(e) => {
		if (e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
		const p = position();
		const delta = e.deltaY * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? innerHeight : 1);
		// Where the text column scrolls on its own, as at 400% zoom (story.css), the wheel scrolls it to its end first.
		const room = delta > 0 ? text.scrollHeight - text.clientHeight - text.scrollTop : text.scrollTop;
		if (room > 1 && text.contains(e.target as Node)) return;
		// From the last step down, the wheel is the reader's, both ways, until a turn up would pass the last step.
		if (p >= COUNT - 1 - SLACK && (delta > 0 || p + delta / height() > COUNT - 1 + SLACK)) return;
		const from = target >= 0 ? target : p > COUNT - 1 + SLACK ? COUNT : nearest();
		const next = from + Math.sign(delta);
		if (next < 0) return;
		e.preventDefault();
		// A turn down on the way to the last step waits until the page is there.
		if (turn(e.timeStamp, delta) && next < COUNT) go(next);
	},
	{ passive: false },
);
// A scroll that stops short of where it was heading, as when the reader takes over, heads nowhere any more: show the
// stop it is at.
addEventListener('scrollend', () => {
	target = -1;
	snapping();
	schedule();
});

skip.addEventListener('click', () => go(COUNT - 1));
// End goes to the foot of the page, so it stops the snapping first. From the last step down the keys scroll as usual,
// but the keys that scroll up go to the last step from just below it, and from it to the stop before, as the wheel
// does.
addEventListener('keydown', (e) => {
	if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
	const on = e.target as Element;
	if (on.closest('input, textarea, select, [contenteditable]')) return;
	if (e.key === 'End') return snapping(false);
	if (e.key === ' ' && on.closest('button, a')) return;
	const up = e.key === 'ArrowUp' || e.key === 'PageUp' || (e.key === ' ' && e.shiftKey);
	const p = position();
	if (!up || p < COUNT - 1 - SLACK || p > COUNT + SLACK) return;
	e.preventDefault();
	go(p > COUNT - 1 + SLACK ? COUNT - 1 : COUNT - 2);
});
