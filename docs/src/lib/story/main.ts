import { syncTokenKeys, toKeyedTokens } from '@shikijs/magic-move/core';
import { MagicMoveRenderer } from '@shikijs/magic-move/renderer';
import type { KeyedTokensInfo } from '@shikijs/magic-move/types';
import CODE from 'virtual:code-steps';
import { STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { mountMap } from './map.ts';
import { STEPS, STOPS, STORY_TEXT, until, type Cut, type Step } from './steps.ts';
import { wheelSteps } from './wheel.ts';

// The home page opens on the intro, beside the whole map running in a loop, and scrolls as any page does. The tour button
// starts the story: the page then snaps a caption at a time, and each scroll plays the next caption of a step on the
// map, at the map's own pace, while the one code block morphs into that caption's code and lights the lines it is
// about. A scroll back up within a step plays its last caption backward; a step reached from another one plays from
// its start. Under reduced motion a caption shows its last frame. Skip, Esc, or a scroll on past the last step ends the
// tour, and the page is the intro and the sections under it again.

const MORPH = { duration: 700, stagger: 0.02, lineNumbers: false };
/** The snap positions: the intro, then one per caption (STOPS). */
const COUNT = STOPS.length + 1;
/**
 * The whole map's run, as the intro loops it: from STATIC_AT, where the last step's camera has pulled back, to that
 * step's end, where step 1's zoom begins; held AGAIN.again ms between runs.
 */
const RUN_END = STEPS[STEPS.length - 1].from + STEPS[STEPS.length - 1].ms;
const AGAIN = { again: 1500 };
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
const h1 = $('.st-intro h1');
const stepView = $('.st-step');
const codes = [codeBlock('.st-code-wide', 'wide'), codeBlock('.st-code-phone', 'phone')];
const count = $('.st-count');
const title = $('.st-title');
const notes = $('.st-notes');
const text = $('.st-text');
const source = $<HTMLAnchorElement>('.st-source a');
const track = $('.st-track');
const tour = $<HTMLButtonElement>('.st-tour');
const skip = $<HTMLButtonElement>('.st-skip');
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

/** Whether the reader is on the tour: the track holds its stops, and the page snaps through them. */
let touring = false;
/** The snap position shown: 0 for the intro, n + 1 for STOPS[n]. */
let at = 0;
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

/** Time t of the map's timeline as ms into step s. */
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

/**
 * Shows the intro, beside the whole map running as in the last step: from where its camera has pulled back, held a
 * moment once it is still, and run again, until the reader takes the tour. Back up from step 1, the map first plays
 * backward to the end of the run, then runs. Under reduced motion it shows its still frame, the one the HTML ships
 * (heromap/still.ts).
 */
function showIntro(back = false) {
	step = -1;
	caption = -1;
	root.dataset.at = 'intro';
	// Focus in the step, which is about to go, is endTour's to move.
	stepView.hidden = true;
	news = said = '';
	const run = () => map.play(STATIC_AT, RUN_END - STATIC_AT, AGAIN);
	if (reduce.matches) map.seek(STATIC_AT);
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
	if (reduce.matches) map.seek(s.from + until(s, c));
	else if (back) map.rewind(s.from + until(s, c), STEPS[k + 1]?.ms ?? s.ms);
	else map.play(s.from, until(s, c));
}

/**
 * Moves the shown step to its caption c: down, the map plays caption c; up, it plays caption c + 1 backward, or, if the
 * step is still playing up to there from its start, plays on to the end of caption c.
 */
function move(c: number, back: boolean) {
	const s = STEPS[step];
	limit = c;
	const left = until(s, c) - into(s, map.now());
	if (reduce.matches) map.seek(s.from + until(s, c));
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
	if (k === at) move(i, false);
	else go(k);
});

// The stage is pinned under the header, and each snap position is one stage's height down the track. Off the tour the
// track is empty, and the page is at the intro.
const height = () => (track.firstElementChild as HTMLElement | null)?.offsetHeight ?? stage.offsetHeight;
/** Where the stage pins, under the header. */
const stageTop = () => parseFloat(getComputedStyle(stage).top);
const top = () => track.getBoundingClientRect().top + scrollY - stageTop();
/** Where the page is, in snap positions: 0 at the intro, COUNT - 1 at the last step, more past it. */
const position = () => (track.firstElementChild ? (scrollY - top()) / height() : 0);
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

let frameId = 0;
const tick = () => {
	frameId = 0;
	if (!touring) return;
	// Scrolled on past the last step, out of the story, the tour ends where the reader is; so it does at the foot of the
	// page below the last step, where a screen taller than what follows the story cannot scroll a whole stop past it.
	const p = position();
	const foot = scrollY >= document.documentElement.scrollHeight - innerHeight - 1;
	if (p >= COUNT || (foot && p > COUNT - 1 + SLACK)) return endTour(true);
	snapping();
	const k = nearest();
	// On the way to a stop further than the next, as a caption's button can go, the stops passed are not shown.
	if (k !== at && (target < 0 || k === target)) {
		// The next or the previous caption of the same step moves to it. A step reached going down plays from its start;
		// going up, the map plays back to where it stops, and holds.
		const was = STOPS[at - 1];
		const stop = STOPS[k - 1];
		// Back up at the intro, the tour ends there.
		if (!stop) return endTour(true, true);
		const up = k < at;
		at = k;
		if (was?.step === stop.step && Math.abs(was.caption - stop.caption) === 1) {
			move(stop.caption, stop.caption < was.caption);
		} else show(stop.step, stop.caption, up);
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
showIntro();

/** Scrolls to snap position k, snapping on the way if k is in the story above its last step. */
function go(k: number) {
	target = k;
	snapping();
	scrollTo({ top: top() + k * height(), behavior: motion() });
}

/** Starts the tour: the track takes its stops, and step 1 shows and plays. */
function startTour() {
	touring = true;
	root.dataset.tour = '';
	// From here on the text fades in as it changes (story.css); the first paint does not fade.
	root.dataset.moved = '';
	addEventListener('wheel', onWheel, { passive: false });
	// A reload mid-tour opens on the page without it, at its top, not at the tour's scroll position.
	history.scrollRestoration = 'manual';
	track.append(
		...Array.from({ length: COUNT }, () => Object.assign(document.createElement('div'), { className: 'st-stop' })),
	);
	// Step 1 shows at once, so focus can move to its title; the stage stays pinned while the page scrolls to its stop.
	at = 1;
	show(STOPS[0].step, STOPS[0].caption);
	skip.disabled = false;
	go(1);
	title.focus({ preventScroll: true });
}

/**
 * Ends the tour: the track gives up its stops, so the page is the intro and the sections under it again. Kept in place
 * (scrolled out at either end, or tabbed out), the page moves by what the track gave up, so what is in view stays
 * where it is, and focus left in the story moves to what is in view; otherwise (Skip, Esc) it goes to the first
 * section, with focus on its heading. Back up at the intro from step 1 (`back`), the map plays back to its run.
 */
function endTour(keep: boolean, back = false) {
	const row = document.querySelector('section.home-row');
	const was = row?.getBoundingClientRect().top ?? 0;
	const past = position() > COUNT - 1;
	const inside = root.contains(document.activeElement);
	touring = false;
	removeEventListener('wheel', onWheel);
	history.scrollRestoration = 'auto';
	delete root.dataset.tour;
	track.replaceChildren();
	snapping(false);
	target = -1;
	at = 0;
	skip.disabled = true;
	showIntro(back);
	// html scrolls smoothly (scroll-behavior), so these jumps say instant. The current scrollY already holds any
	// adjustment the browser's scroll anchoring made.
	const heading = document.getElementById('try-it');
	if (heading && !heading.hasAttribute('tabindex')) heading.tabIndex = -1;
	if (keep) {
		if (row) scrollTo({ top: scrollY + row.getBoundingClientRect().top - was, behavior: 'instant' });
		// Never to something out of view: past the end, the first section heading wholly between the header and the
		// screen's foot, if any.
		const seen = (el: Element) => {
			const r = el.getBoundingClientRect();
			return r.top >= stageTop() && r.bottom <= innerHeight;
		};
		const shown = past ? [...document.querySelectorAll<HTMLElement>('section.home-row h2')].find(seen) : h1;
		if (inside && shown) {
			if (!shown.hasAttribute('tabindex')) shown.tabIndex = -1;
			shown.focus({ preventScroll: true });
		}
		return;
	}
	heading?.focus({ preventScroll: true });
	heading?.scrollIntoView({ behavior: 'instant', block: 'start' });
}

tour.addEventListener('click', startTour);
skip.addEventListener('click', () => endTour(false));

// Keyboard and touch scroll natively and snap a stop at a time. The wheel is taken over, so that one turn is one stop
// (wheel.ts): snapping alone lets a short notch fall back to where it was. From the last step down, the page scrolls as
// usual with every input; on the way back up, the story takes over again at the last step.
// It listens only on the tour (startTour), so the page off the tour scrolls on the browser's own thread.
const turn = wheelSteps();
function onWheel(e: WheelEvent) {
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
}
// A scroll that stops short of where it was heading, as when the reader takes over, heads nowhere any more: show the
// stop it is at.
addEventListener('scrollend', () => {
	if (!touring) return;
	target = -1;
	snapping();
	schedule();
});

// Esc ends the tour, as Skip does. End goes to the foot of the page, so it stops the snapping first. From the last step
// down the keys scroll as usual, but the keys that scroll up go to the last step from just below it, and from it to the
// stop before, as the wheel does.
addEventListener('keydown', (e) => {
	if (!touring || e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
	const on = e.target as Element;
	// Esc in the search dialog closes the dialog, not the tour.
	if (on.closest('input, textarea, select, [contenteditable], dialog') || document.querySelector('dialog[open]')) return;
	if (e.key === 'Escape') return endTour(false);
	if (e.key === 'End') return snapping(false);
	if (e.key === ' ' && on.closest('button, a')) return;
	const up = e.key === 'ArrowUp' || e.key === 'PageUp' || (e.key === ' ' && e.shiftKey);
	const p = position();
	if (!up || p < COUNT - 1 - SLACK || p > COUNT + SLACK) return;
	e.preventDefault();
	go(p > COUNT - 1 + SLACK ? COUNT - 1 : COUNT - 2);
});

// Tabbing on out of the story, into what follows it, ends the tour where the reader is.
addEventListener('focusin', (e) => {
	const to = e.target as Element;
	if (!touring || root.contains(to) || !(root.compareDocumentPosition(to) & Node.DOCUMENT_POSITION_FOLLOWING)) return;
	endTour(true);
	// html scrolls smoothly, so the browser brings focus into view only after this; the scroll endTour made cancels it.
	to.scrollIntoView({ block: 'nearest', behavior: 'instant' });
});

// A reload mid-tour opened at the top (startTour); once loaded, the page keeps its scroll on a reload again.
if (history.scrollRestoration === 'manual') {
	addEventListener('load', () => requestAnimationFrame(() => (history.scrollRestoration = 'auto')), { once: true });
}

// Off the tour, the intro's run rests while the hero is out of view, under the header too, and runs again from its
// start once it is back.
new IntersectionObserver(
	([entry]) => {
		if (touring || reduce.matches) return;
		if (entry.isIntersecting) showIntro();
		else map.seek(map.now());
	},
	// A pixel more than the header, so a stage whose foot only touches the header's edge counts as out of view.
	{ rootMargin: `-${stageTop() + 1}px 0px 0px 0px` },
).observe(stage);
