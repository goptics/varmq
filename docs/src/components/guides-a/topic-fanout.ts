// Scene: a topic with two subscribed workers. Publish stores one copy of the payload in a shared log.
// Each worker reads the log at its own position and makes its own job of every payload.
import { caption, chip, flip, h, q, reduced, say, stepper, Timers } from './kit';
import { Topic, type TopicSub } from './model';

const JOB_MS: Record<string, number> = { email: 1500, stock: 700 };
const TONE: Record<string, '' | 'amber' | 'cobalt'> = { email: 'amber', stock: 'cobalt' };
const READ_DELAY = 380;
const FIRST_CAP = 'topic := varmq.NewTopic[Order]()';

interface Card {
	sub: TopicSub;
	root: HTMLElement;
	jobs: HTMLElement;
	state: HTMLElement;
	unread: HTMLElement;
	ran: HTMLElement;
	pause: HTMLButtonElement;
	toggle: HTMLButtonElement;
	running: number | null;
	done: number;
}

function init(root: HTMLElement) {
	const timers = new Timers();
	const logEl = q<HTMLElement>(root, '[data-log]');
	const pubEl = q<HTMLElement>(root, '[data-pub]');
	const whyEl = q<HTMLElement>(root, '[data-why]');
	const subsTag = q<HTMLElement>(root, '[data-subs]');
	const chips = new Map<string, HTMLElement>();

	let topic = new Topic(['email', 'stock']);
	let cards: Card[] = [];
	let touched = false;

	const makeCards = () => {
		cards = topic.subs.map((sub) => {
			const el = q<HTMLElement>(root, `[data-sub="${sub.name}"]`);
			return {
				sub,
				root: el,
				jobs: q(el, '[data-jobs]'),
				state: q(el, '[data-state]'),
				unread: q(el, '[data-unread]'),
				ran: q(el, '[data-ran]'),
				pause: q(el, '[data-pause]'),
				toggle: q(el, '[data-sub-toggle]'),
				running: null,
				done: 0,
			};
		});
	};

	const cap = stepper(q(root, '[data-cap-step]'), {
		min: 0,
		max: 4,
		value: 0,
		format: (v) => (v < 1 ? 'no limit' : String(v)),
		onChange: (v) => {
			topic.capacity = v;
			caption(root, v > 0 ? `topic := varmq.NewTopic[Order](varmq.WithTopicCapacity(${v}))` : FIRST_CAP);
			paint();
		},
	});

	const logChip = (n: number) => {
		const k = `log-${n}`;
		let c = chips.get(k);
		if (!c) {
			c = chip(`order ${n}`, '', k);
			c.classList.add('chip--log');
			chips.set(k, c);
		}
		const state = (s: TopicSub) => (s.read.has(n) ? 'on' : s.subscribed && s.from <= n ? 'off' : 'na');
		c.replaceChildren(
			h('span', { text: `order ${n}` }),
			h('span', { class: 'pips', attrs: { 'aria-hidden': 'true' } }, ...topic.subs.map((s) => h('i', { class: 'pip', attrs: { 'data-s': state(s) } }))),
		);
		c.setAttribute(
			'aria-label',
			`order ${n}. ${topic.subs.map((s) => `${s.name}: ${{ on: 'read', off: 'not read yet', na: 'not subscribed' }[state(s)]}`).join('. ')}`,
		);
		return c;
	};
	const jobChip = (card: Card, n: number) => {
		const k = `${card.sub.name}-${n}`;
		let c = chips.get(k);
		if (!c) {
			c = chip(`order ${n}`, TONE[card.sub.name], k);
			chips.set(k, c);
		}
		return c;
	};

	const fill = (el: HTMLElement, items: Node[]) => {
		const hint = el.querySelector<HTMLElement>('.ga-hint');
		el.replaceChildren(...(hint ? [hint] : []), ...items);
	};

	const paint = () => {
		fill(logEl, topic.log.map(logChip));
		const live = topic.subs.filter((s) => s.subscribed).length;
		subsTag.textContent = `${live} ${live === 1 ? 'subscriber' : 'subscribers'}`;
		for (const c of cards) {
			const s = c.sub;
			const mine = [...(c.running !== null ? [c.running] : []), ...s.jobs];
			fill(c.jobs, mine.map((n) => jobChip(c, n)));
			c.jobs.querySelectorAll('.chip').forEach((el, i) => el.classList.toggle('is-run', i === 0 && c.running !== null));
			c.state.textContent = !s.subscribed ? 'unsubscribed' : s.paused ? 'paused' : 'running';
			c.state.className = `ga-tag${!s.subscribed ? ' ga-tag--bad' : s.paused ? ' ga-tag--warn' : ' ga-tag--on'}`;
			c.unread.textContent = `${topic.unread(s).length} unread`;
			c.ran.textContent = `${c.done} ran`;
			c.pause.textContent = s.paused ? 'Resume' : 'Pause';
			c.pause.disabled = !s.subscribed && !s.jobs.length && c.running === null;
			c.toggle.textContent = s.subscribed ? 'Unsubscribe' : 'Subscribe';
			c.root.toggleAttribute('data-on', s.subscribed);
		}
	};
	const render = () => flip(root, paint);

	// A running worker starts the next job on its own. A paused one starts none.
	const pump = (c: Card) => {
		if (c.running !== null || c.sub.paused || !c.sub.jobs.length) return;
		c.running = c.sub.jobs.shift()!;
		const n = c.running;
		timers.after(JOB_MS[c.sub.name], () => {
			c.done++;
			c.running = null;
			chips.delete(`${c.sub.name}-${n}`);
			pump(c);
			render();
			say(root, `The ${c.sub.name} worker finished its job for order ${n}.`);
		});
	};

	const drainAll = () => {
		for (const c of cards) {
			topic.drain(c.sub);
			pump(c);
		}
	};

	const publish = (): boolean => {
		const r = topic.publish();
		if (r.ok) {
			caption(root, `topic.Publish(order${r.n})   // true`);
			pubEl.textContent = 'true';
			pubEl.dataset.tone = 'ok';
			whyEl.textContent = `Order ${r.n} is in the log once. Each running worker reads it and makes its own job.`;
			say(root, `Publish returned true for order ${r.n}.`);
			if (reduced()) {
				drainAll();
				render();
			} else {
				render();
				timers.after(READ_DELAY, () => {
					drainAll();
					render();
				});
			}
			return true;
		}
		caption(root, 'topic.Publish(order)   // false');
		pubEl.textContent = 'false';
		pubEl.dataset.tone = 'bad';
		whyEl.textContent =
			r.why === 'no subscribers'
				? 'No worker is subscribed, so the payload has nowhere to go. It is gone, and the next subscriber will not get it.'
				: `The topic is full. The slowest subscription has ${topic.capacity} unread payloads. The refused payload reaches no worker.`;
		say(root, `Publish returned false. ${r.why === 'full' ? 'The topic is full.' : 'Nobody is subscribed.'}`);
		paint();
		return false;
	};

	root.addEventListener('click', (e) => {
		const t = e.target as HTMLElement;
		const card = cards.find((c) => c.root.contains(t));
		if (t.closest('[data-publish]')) {
			touched = true;
			publish();
		} else if (card && t.closest('[data-pause]')) {
			touched = true;
			card.sub.paused = !card.sub.paused;
			caption(root, card.sub.paused ? `${card.sub.name}.Pause()` : `${card.sub.name}.Resume()`);
			if (!card.sub.paused) {
				topic.drain(card.sub);
				pump(card);
			}
			render();
			say(root, `The ${card.sub.name} worker is ${card.sub.paused ? 'paused. It reads nothing.' : 'running. It reads what is unread.'}`);
		} else if (card && t.closest('[data-sub-toggle]')) {
			touched = true;
			if (card.sub.subscribed) {
				topic.unsubscribe(card.sub);
				caption(root, `${card.sub.name}Sub.Unsubscribe()`);
				whyEl.textContent = 'Unsubscribed: the worker gets nothing more and drops what it had not read. A job it already took still runs.';
			} else {
				topic.subscribe(card.sub);
				topic.drain(card.sub);
				caption(root, `${card.sub.name}.WithTopic(topic)`);
				whyEl.textContent = 'Subscribed again: this worker gets only the payloads published from now on.';
			}
			render();
			say(root, `The ${card.sub.name} worker is ${card.sub.subscribed ? 'subscribed' : 'unsubscribed'}.`);
		} else if (t.closest('[data-reset]')) {
			reset();
		}
	});

	const reset = () => {
		timers.clear();
		chips.clear();
		const capacity = cap.get();
		topic = new Topic(['email', 'stock']);
		topic.capacity = capacity;
		makeCards();
		pubEl.textContent = 'not called';
		pubEl.dataset.tone = 'dim';
		whyEl.textContent = 'Publish an order. Then pause the email worker and publish again to see the log keep payloads.';
		caption(root, capacity > 0 ? `topic := varmq.NewTopic[Order](varmq.WithTopicCapacity(${capacity}))` : FIRST_CAP);
		paint();
		say(root, 'Reset.');
	};

	// On the first view the scene publishes one order, so the fan-out shows before the reader acts.
	const intro = () => {
		if (touched) return;
		publish();
		if (reduced()) {
			for (const c of cards) {
				c.done += c.sub.jobs.length + (c.running !== null ? 1 : 0);
				c.sub.jobs = [];
				c.running = null;
			}
			timers.clear();
			paint();
		}
	};

	reset();
	root.dataset.ready = '';
	const io = new IntersectionObserver(
		(entries) => {
			if (entries.some((e) => e.isIntersecting)) {
				io.disconnect();
				intro();
			}
		},
		{ threshold: 0.6 },
	);
	io.observe(root);
}

document.querySelectorAll<HTMLElement>('[data-ga="topic-fanout"]').forEach(init);
