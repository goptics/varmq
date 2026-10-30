// Draws a WorkerSim: the queue, one lane per goroutine slot, and the finished jobs.
// One square is one job. One lane is one goroutine.

import type { Job, SimEvent, WorkerSim } from './sim';
import { setText } from './scene';

export interface PoolViewOptions {
	/** Most lanes to draw. A busy goroutine in a slot above it is not drawn. */
	maxLanes?: number;
	/** Most squares to draw in the queue before "+n". */
	showQueue?: number;
	/** Draw the "done" column. */
	showDone?: boolean;
	/** Label of the lane column. */
	laneLabel?: string;
	/** Kinds of done square to explain under the tally. */
	legend?: Array<'ok' | 'fail' | 'early' | 'closed'>;
	/** Draw only the live goroutines, one block each, instead of one lane per slot. */
	blocks?: boolean;
}

const LEGEND = { ok: 'finished', fail: 'failed', early: 'returned early', closed: 'closed, never ran' } as const;

const DOTS = 36;

interface LaneEl {
	row: HTMLElement;
	word: HTMLElement;
	job: HTMLElement;
	held: HTMLElement;
	id: HTMLElement;
	sig: string;
}

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
	private lanes: LaneEl[] = [];
	private blockEls = new Map<number, HTMLElement>();
	private ended: Array<'ok' | 'fail' | 'early' | 'closed'> = [];
	private qSig = '';
	private pSig = '';
	private dSig = '';

	constructor(root: HTMLElement, sim: WorkerSim, opts: PoolViewOptions = {}) {
		this.root = root;
		this.sim = sim;
		this.o = { maxLanes: 12, showQueue: 16, showDone: true, laneLabel: 'goroutines', legend: [], blocks: false, ...opts };
		this.build();
	}

	reset(sim: WorkerSim) {
		this.sim = sim;
		this.ended = [];
		this.qSig = this.pSig = this.dSig = '';
		for (const l of this.lanes) l.sig = '';
		for (const b of this.blockEls.values()) b.remove();
		this.blockEls.clear();
	}

	/** Feed the events of the sim so the done column knows how each job ended. */
	record(e: SimEvent) {
		if (e.type === 'end') this.ended.push(e.job.early ? 'early' : e.job.fail ? 'fail' : 'ok');
		else if (e.type === 'close') for (let i = 0; i < e.jobs.length; i++) this.ended.push('closed');
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
				<span class="slabel">${this.o.laneLabel}</span>
				<div class="${this.o.blocks ? 'gb-blocks' : 'gb-lanes'}" data-lanes></div>
			</div>
			${
				this.o.showDone
					? `<div class="gb-col gb-col--out">
				<span class="slabel">done</span>
				<div class="gb-dots" data-dots></div>
				<p class="gb-tally" data-tally></p>
				${
					this.o.legend.length
						? `<ul class="gb-legend">${this.o.legend.map((k) => `<li><i class="${k}"></i>${LEGEND[k]}</li>`).join('')}</ul>`
						: ''
				}
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

		if (this.o.blocks) this.drawBlocks();
		else this.drawLanes();

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

	/** One row per slot that is alive or below the limit. */
	private drawLanes() {
		const sim = this.sim;
		let rows = Math.min(sim.limit, this.o.maxLanes);
		for (const g of sim.gors) rows = Math.max(rows, g.slot + 1);
		rows = Math.min(rows, this.o.maxLanes);
		while (this.lanes.length < rows) this.addLane();
		while (this.lanes.length > rows) this.lanes.pop()!.row.remove();

		for (let i = 0; i < rows; i++) {
			const g = sim.gors.find((x) => x.slot === i);
			const l = this.lanes[i];
			const state = !g ? 'none' : g.busy ? 'busy' : 'idle';
			const job = g?.busy ? g.batch[g.i] : undefined;
			const held = g?.busy ? g.batch.slice(g.i + 1) : [];
			const over = state === 'busy' && i >= sim.limit;
			const sig = `${state}:${job?.id ?? 0}:${held.map((h) => h.id).join(',')}:${over}:${i < sim.limit}`;
			if (sig !== l.sig) {
				l.sig = sig;
				l.row.dataset.s = state;
				l.row.dataset.over = over ? 'true' : 'false';
				l.row.dataset.off = i >= sim.limit && state === 'none' ? 'true' : 'false';
				l.id.textContent = over ? `g${i + 1} over` : `g${i + 1}`;
				l.word.textContent = state === 'idle' ? 'idle goroutine' : state === 'none' ? 'no goroutine' : '';
				l.job.textContent = job ? String(job.id) : '';
				l.job.dataset.fail = job?.fail ?? '';
				const nodes: HTMLElement[] = [];
				if (held.length) {
					const tag = document.createElement('span');
					tag.className = 'gb-lane__tag';
					tag.textContent = 'batch';
					nodes.push(tag);
				}
				nodes.push(...held.slice(0, 6).map((h) => this.chip(h, 'is-held')));
				if (held.length > 6) nodes.push(this.more(held.length - 6));
				l.held.replaceChildren(...nodes);
			}
			if (job) l.row.style.setProperty('--p', String(Math.max(0, Math.min(1, 1 - job.left / job.dur)).toFixed(3)));
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

	private addLane() {
		const row = document.createElement('div');
		row.className = 'gb-lane';
		row.innerHTML = `<span class="gb-lane__id"></span>
			<div class="gb-lane__track"><span class="gb-lane__fill"></span><span class="gb-job gb-lane__job"></span><span class="gb-lane__word"></span></div>
			<div class="gb-lane__held"></div>`;
		this.lanesEl.append(row);
		this.lanes.push({
			row,
			id: row.querySelector('.gb-lane__id')!,
			word: row.querySelector('.gb-lane__word')!,
			job: row.querySelector('.gb-lane__job')!,
			held: row.querySelector('.gb-lane__held')!,
			sig: '',
		});
	}
}
