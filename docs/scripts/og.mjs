// The social card, public/og.png (1200 × 630), drawn as one SVG in the dark theme: the logo and the install command,
// the home page's title in the map's colours and its lede, and under them the hero map's one pool in the map's own
// shapes and colours (lib/heromap/map.css): jobs waiting in the queue, the event loop, and a worker whose goroutines
// run jobs. The page margin's sprocket rails run down both sides. Headless Chrome renders it, with the site's fonts.
//
//   pnpm og        writes public/og.png
//   pnpm og --svg  also keeps the SVG, as public/og.svg

import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const root = new URL('..', import.meta.url).pathname;
const W = 1200;
const H = 630;

// The title and lede are the home page's hero fields (src/content/docs/index.mdx).
const mdx = readFileSync(join(root, 'src/content/docs/index.mdx'), 'utf8');
const field = (name) => mdx.match(new RegExp(`^\\s+${name}: (.+)$`, 'm'))[1].trim();
const title = field('title');
const tagline = field('tagline');

// The dark theme's tokens (src/styles/tokens.css) and the dark map's palette (map.css).
const C = {
	paper: '#0f1713',
	ink: '#e5f1eb',
	ink2: '#b0c7bb',
	ink3: '#8fa89c',
	rule: '#2a4137',
	panel: '#0a1410',
	panelRule: '#27413a',
	panelInk: '#e4f2ec',
	panelInk2: '#9db9ac',
	label: '#8592a3',
	dot: '#1e2a38',
	go: '#44bf9e',
	goFill: 'rgba(68, 191, 158, 0.1)',
	ghost: 'rgba(68, 191, 158, 0.3)',
	check: '#d6fff0',
	card: 'rgba(68, 191, 158, 0.04)',
	cardEdge: 'rgba(68, 191, 158, 0.45)',
	job: '#f2b33d',
	jobInk: '#3a2a00',
	chip: '#1e201f',
	chipEdge: 'rgba(242, 179, 61, 0.65)',
	lane: 'rgba(242, 179, 61, 0.05)',
	slot: 'rgba(242, 179, 61, 0.18)',
	loop: '#8b5cf6',
	loopFill: 'rgba(139, 92, 246, 0.1)',
	pulse: '#a78bfa',
	line: 'rgba(150, 175, 200, 0.35)',
	hole: '#0a110d',
	rim: 'rgba(68, 191, 158, 0.35)',
};

const esc = (s) => s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c]);
const logo = readFileSync(join(root, 'src/assets/logo-dark.svg'), 'base64');

// ---- The pool, along the foot of the card. Geometry follows the map's lane (scene.ts, LANE) at 1.4 times. ----

const Y = 474; // the row's middle
const S = 1.4;
const lane = { x: 210, w: 170 * S, h: 44 * S, slot: 22 * S, step: 32 * S, pad: 10 * S };
const slotX = (k) => lane.x + lane.w - lane.pad - lane.slot - lane.step * k; // slot 0 is the head, by the event loop
const loop = { x: 642, r: 34 };
const card = { x: 780, y: Y - 58, w: 323, h: 116 };
const go = (k) => card.x + 50 + k * 56;

const label = (x, y, text, anchor = 'start') =>
	`<text x="${x}" y="${y}" text-anchor="${anchor}" class="label">${text}</text>`;
const job = (x, y, size) =>
	`<rect x="${x}" y="${y}" width="${size}" height="${size}" rx="${size * 0.22}" fill="${C.job}" filter="url(#glow)"/>`;

// A goroutine: a ring; running, with its job inside and its progress arc; done, with a check; or idle, a dashed ghost.
function goroutine(k, state, progress = 0) {
	const cx = go(k);
	const r = 17;
	if (state === 'idle') return `<circle cx="${cx}" cy="${Y}" r="${r}" fill="none" stroke="${C.ghost}" stroke-width="2" stroke-dasharray="3 4"/>`;
	let out = `<circle cx="${cx}" cy="${Y}" r="${r}" fill="${C.goFill}" stroke="${C.go}" stroke-width="2.5" filter="url(#glow)"/>`;
	if (state === 'done')
		out += `<path d="M${cx - 7} ${Y + 0.5}l4.5 4.5l9-9.5" fill="none" stroke="${C.check}" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/>`;
	if (state === 'run') {
		const R = r + 6;
		const a = progress * 2 * Math.PI;
		const ex = cx + R * Math.sin(a);
		const ey = Y - R * Math.cos(a);
		out += job(cx - 7, Y - 7, 14);
		out += `<path d="M${cx} ${Y - R}A${R} ${R} 0 ${progress > 0.5 ? 1 : 0} 1 ${ex.toFixed(1)} ${ey.toFixed(1)}" fill="none" stroke="${C.go}" stroke-width="3" stroke-linecap="round"/>`;
	}
	return out;
}

const pool = [
	// Add() and its line into the queue's tail.
	`<rect x="97" y="${Y - 15}" width="82" height="30" rx="15" fill="${C.chip}" stroke="${C.chipEdge}" stroke-width="1.5"/>`,
	`<text x="138" y="${Y + 5}" text-anchor="middle" class="chip">Add()</text>`,
	`<path d="M179 ${Y}H${lane.x}" stroke="${C.line}" stroke-width="2"/>`,
	// The queue: five slots, three jobs waiting at its head.
	label(lane.x, Y - lane.h / 2 - 14, 'QUEUE'),
	`<rect x="${lane.x}" y="${Y - lane.h / 2}" width="${lane.w}" height="${lane.h}" rx="12" fill="${C.lane}" stroke="url(#lane-edge)" stroke-width="2"/>`,
	...[0, 1, 2, 3, 4].map(
		(k) =>
			`<rect x="${slotX(k)}" y="${Y - lane.slot / 2}" width="${lane.slot}" height="${lane.slot}" rx="7" fill="none" stroke="${C.slot}" stroke-width="1.5" stroke-dasharray="3 3"/>`,
	),
	...[0, 1, 2].map((k) => job(slotX(k), Y - lane.slot / 2, lane.slot)),
	// The event loop, between the queue and the worker.
	`<path d="M${lane.x + lane.w} ${Y}H${loop.x - loop.r}M${loop.x + loop.r} ${Y}H${card.x}" stroke="${C.line}" stroke-width="2"/>`,
	`<circle cx="${loop.x}" cy="${Y}" r="${loop.r + 11}" fill="none" stroke="${C.pulse}" stroke-opacity="0.3" stroke-width="2"/>`,
	`<circle cx="${loop.x}" cy="${Y}" r="${loop.r}" fill="${C.loopFill}" stroke="${C.loop}" stroke-width="3" filter="url(#glow)"/>`,
	label(loop.x, Y + loop.r + 34, 'EVENT LOOP', 'middle'),
	// The worker: five goroutines, three running jobs, one done, one idle.
	label(card.x, card.y - 14, 'WORKER'),
	`<rect x="${card.x}" y="${card.y}" width="${card.w}" height="${card.h}" rx="16" fill="${C.card}" stroke="${C.cardEdge}" stroke-width="1.5"/>`,
	goroutine(0, 'run', 0.72),
	goroutine(1, 'done'),
	goroutine(2, 'run', 0.4),
	goroutine(3, 'run', 0.15),
	goroutine(4, 'idle'),
].join('\n\t\t');

// ---- The card ----

const titleSpans = esc(title)
	.replace('Worker pools', `<tspan fill="${C.go}">Worker pools</tspan>`)
	.replace('job queues', `<tspan fill="${C.job}">job queues</tspan>`);
// The lede in two lines, broken before "or".
const [lede1, lede2] = tagline.split(/,\s(?=or )/);

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,400..800&amp;family=JetBrains+Mono:wght@400;600&amp;display=block');
		.title { font: 700 72px 'Bricolage Grotesque'; font-variation-settings: 'opsz' 96; letter-spacing: -0.035em; fill: ${C.ink}; }
		.lede { font: 400 22px 'Bricolage Grotesque'; fill: ${C.ink2}; }
		.mono { font: 400 16px 'JetBrains Mono'; }
		.label { font: 600 14px 'JetBrains Mono'; letter-spacing: 0.14em; fill: ${C.label}; }
		.chip { font: 600 14px 'JetBrains Mono'; fill: ${C.job}; }
	</style>
	<defs>
		<filter id="glow" x="-80%" y="-80%" width="260%" height="260%">
			<feGaussianBlur stdDeviation="3" result="blur"/>
			<feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
		</filter>
		<linearGradient id="lane-edge" x1="0" x2="1">
			<stop offset="0" stop-color="${C.job}" stop-opacity="0.12"/>
			<stop offset="1" stop-color="${C.job}" stop-opacity="0.9"/>
		</linearGradient>
		<pattern id="dots" width="14" height="14" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r="1" fill="${C.dot}"/></pattern>
		<radialGradient id="dot-fade" cx="600" cy="${Y}" r="560" gradientUnits="userSpaceOnUse">
			<stop offset="0.3" stop-color="#fff"/>
			<stop offset="1" stop-color="#fff" stop-opacity="0"/>
		</radialGradient>
		<mask id="dot-mask"><rect width="${W}" height="${H}" fill="url(#dot-fade)"/></mask>
		<pattern id="holes" width="40" height="48" patternUnits="userSpaceOnUse" y="15">
			<circle cx="20" cy="24" r="5.4" fill="${C.hole}" stroke="${C.rim}" stroke-width="1"/>
		</pattern>
	</defs>

	<rect width="${W}" height="${H}" fill="${C.paper}"/>
	<rect y="330" width="${W}" height="300" fill="url(#dots)" mask="url(#dot-mask)"/>

	<rect width="40" height="${H}" fill="url(#holes)"/>
	<rect x="${W - 40}" width="40" height="${H}" fill="url(#holes)"/>
	<path d="M40.5 0V${H}M${W - 40.5} 0V${H}" stroke="${C.rule}" stroke-dasharray="4 4"/>

	<image href="data:image/svg+xml;base64,${logo}" x="97" y="52" width="165" height="40"/>
	<rect x="755.5" y="52.5" width="347" height="40" rx="12" fill="${C.panel}" stroke="${C.panelRule}"/>
	<text x="772" y="78" class="mono"><tspan fill="${C.panelInk2}">$</tspan><tspan fill="${C.panelInk}"> go get github.com/goptics/varmq</tspan></text>

	<text x="94" y="196" class="title">${titleSpans}</text>
	<text x="97" y="252" class="lede">${esc(lede1)},</text>
	<text x="97" y="283" class="lede">${esc(lede2)}</text>

	<g>
		${pool}
	</g>

	<text x="${W - 69}" y="602" text-anchor="end" class="mono" fill="${C.ink3}">varmq.goptics.org</text>
</svg>
`;

const dir = mkdtempSync(join(tmpdir(), 'varmq-og-'));
const file = join(dir, 'og.svg');
writeFileSync(file, svg);
if (process.argv.includes('--svg')) writeFileSync(join(root, 'public/og.svg'), svg);
execFileSync(
	'google-chrome',
	[
		'--headless=new',
		'--hide-scrollbars',
		'--force-device-scale-factor=1',
		`--window-size=${W},${H}`,
		'--virtual-time-budget=5000',
		`--screenshot=${join(root, 'public/og.png')}`,
		`file://${file}`,
	],
	{ stdio: 'ignore' },
);
rmSync(dir, { recursive: true });
console.log('wrote public/og.png');
