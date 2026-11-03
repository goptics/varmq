// Draws a WorkerSim: the queue, one block per live goroutine, and the finished jobs.
// One square is one job.

import type { Job, SimEvent, WorkerSim } from './sim';
import { setText } from './scene';

export interface PoolViewOptions {
	/** Most squares to draw in the queue before "+n". */
	showQueue?: number;
	/** Draw the "done" column. */
	showDone?: boolean;
}

const DOTS = 36;

export class PoolView {
	sim: WorkerSim;
	private root: HTMLElement;
	private o: Required<PoolViewOptions>;
	private queueEl!: HTMLElement;
	private parkedEl!: HTMLElement;
	private parkedWrap!: HTMLElement;
	private lanesEl!: HTMLElement;
	private doneCol!: HTMLElement;
	private dotsEl!: HTMLElement;
	private tallyEl!: HTMLElement;
	private qLen!: HTMLElement;
	private blockEls = new Map<number, HTMLElement>();
	private ended: Array<'ok' | 'fail'> = [];
	private qSig = '';
	private pSig = '';
	private dSig = '';

	constructor(root: HTMLElement, sim: WorkerSim, opts: PoolViewOptions = {}) {
		this.root = root;
		this.sim = sim;
		this.o = { showQueue: 16, showDone: true, ...opts };
		this.build();
	}

	reset(sim: WorkerSim) {
		this.sim = sim;
		this.ended = [];
		this.qSig = this.pSig = this.dSig = '';
		for (const b of this.blockEls.values()) b.remove();
		this.blockEls.clear();
	}

	/** Feed the events of the sim so the done column knows how each job ended. */
	record(e: SimEvent) {
		if (e.type === 'end') this.ended.push(e.job.fail ? 'fail' : 'ok');
		if (this.ended.length > 400) this.ended.splice(0, this.ended.length - DOTS);
	}

	private build() {
		this.root.classList.add('gb-pool');
		this.root.innerHTML = `
			<div class="gb-col gb-col--queue">
				<span class="slabel">queue <b data-q-len></b></span>
				<div class="gb-jobs" data-queue></div>
				<div data-parked-wrap hidden>
					<span class="slabel gb-sub">parked on the worker</span>
					<div class="gb-jobs" data-parked></div>
				</div>
			</div>
			<div class="gb-col gb-col--lanes">
				<span class="slabel">goroutines</span>
				<div class="gb-blocks" data-lanes></div>
			</div>
			${
				this.o.showDone
					? `<div class="gb-col gb-col--out">
				<span class="slabel">done</span>
				<div class="gb-dots" data-dots></div>
				<p class="gb-tally" data-tally></p>
			</div>`
					: ''
			}`;
		const $ = <T extends HTMLElement>(s: string) => this.root.querySelector<T>(s)!;
		this.queueEl = $('[data-queue]');
		this.parkedEl = $('[data-parked]');
		this.parkedWrap = $('[data-parked-wrap]');
		this.lanesEl = $('[data-lanes]');
		this.qLen = $('[data-q-len]');
		if (this.o.showDone) {
			this.doneCol = $('.gb-col--out');
			this.dotsEl = $('[data-dots]');
			this.tallyEl = $('[data-tally]');
			for (let i = 0; i < DOTS; i++) this.dotsEl.append(document.createElement('i'));
		}
	}

	private chip(j: Job, cls = '') {
		const s = document.createElement('span');
		s.className = 'gb-job' + (cls ? ' ' + cls : '');
		s.textContent = String(j.id);
		return s;
	}

	private more(n: number) {
		const s = document.createElement('span');
		s.className = 'gb-job gb-job--more';
		s.textContent = `+${n}`;
		return s;
	}

	update() {
		const sim = this.sim;

		// Queue
		const q = sim.queue;
		const qSig = q.length + ':' + (q[0]?.id ?? 0) + ':' + (q[q.length - 1]?.id ?? 0);
		if (qSig !== this.qSig) {
			this.qSig = qSig;
			const nodes = q.slice(0, this.o.showQueue).map((j, i) => this.chip(j, i === 0 ? 'is-next' : ''));
			if (q.length > this.o.showQueue) nodes.push(this.more(q.length - this.o.showQueue));
			this.queueEl.replaceChildren(...nodes);
			this.queueEl.dataset.empty = q.length ? '' : 'true';
		}
		setText(this.qLen, `Len() ${q.length}`);

		// Parked
		const p = sim.parked;
		const pSig = p.length + ':' + (p[0]?.id ?? 0);
		if (pSig !== this.pSig) {
			this.pSig = pSig;
			this.parkedWrap.hidden = p.length === 0;
			this.parkedEl.replaceChildren(...p.slice(0, 12).map((j) => this.chip(j, 'is-parked')));
		}

		this.drawBlocks();

		// Done
		if (this.o.showDone) {
			const total = this.ended.length;
			const tail = this.ended.slice(-DOTS);
			const dSig = total + ':' + tail.length;
			if (dSig !== this.dSig) {
				this.dSig = dSig;
				const dots = this.dotsEl.children;
				for (let i = 0; i < DOTS; i++) {
					const k = tail[i];
					(dots[i] as HTMLElement).className = k ?? '';
				}
				setText(this.tallyEl, `${total} ended`);
			}
		}
	}

	/**
	 * One block per live goroutine, in slot order: a goroutine that exits leaves the DOM. A busy block shows
	 * its job, "+n" for the jobs it still holds as a batch, and an amber edge when it runs above the limit.
	 */
	private drawBlocks() {
		const live = [...this.sim.gors].sort((x, y) => x.slot - y.slot);
		const slots = new Set(live.map((g) => g.slot));
		for (const [slot, el] of this.blockEls) {
			if (slots.has(slot)) continue;
			el.remove();
			this.blockEls.delete(slot);
		}
		let added = false;
		let busy = 0;
		for (const g of live) {
			let el = this.blockEls.get(g.slot);
			if (!el) {
				el = document.createElement('span');
				el.className = 'gb-block';
				el.innerHTML = `<b>g${g.slot + 1}</b><span class="gb-block__job"></span><span class="gb-block__held"></span>`;
				this.blockEls.set(g.slot, el);
				added = true;
			}
			const job = g.busy ? g.batch[g.i] : undefined;
			el.dataset.s = job ? 'busy' : 'idle';
			el.dataset.over = String(!!job && ++busy > this.sim.limit);
			setText(el.children[1] as HTMLElement, job ? String(job.id) : '');
			const held = job ? g.batch.length - g.i - 1 : 0;
			setText(el.children[2] as HTMLElement, held ? `+${held}` : '');
			(el.children[1] as HTMLElement).dataset.fail = job?.fail ?? '';
			el.style.setProperty('--p', job ? Math.max(0, Math.min(1, 1 - job.left / job.dur)).toFixed(3) : '0');
		}
		if (added) this.lanesEl.replaceChildren(...live.map((g) => this.blockEls.get(g.slot)!));
	}
}
