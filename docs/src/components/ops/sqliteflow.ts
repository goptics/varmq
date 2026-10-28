// SQLite scene. The rules come from the SQLite page: a new row is pending; taking a job turns the row
// into processing in one statement; Acknowledge deletes the row (or marks it completed with
// WithRemoveOnComplete(false)) and only matches a processing row; the first time a process opens a
// queue, sqliteq sets every processing row back to pending, once per queue in each process.

import { h, job } from './ops';

type Status = 'pending' | 'processing' | 'completed';
interface Row {
	id: number;
	status: Status;
}
interface State {
	rows: Row[];
	inflight: number | null; // the job the first process runs
	down: boolean;
	runs: Record<number, number>;
	msg: string;
}

const root = document.querySelector<HTMLElement>('[data-sflow]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.ops-scene')!;
	const $ = <T extends HTMLElement>(s: string) => figure.querySelector<T>(s)!;
	const procs = $('[data-procs]');
	const body = $('[data-rows]');
	const out = $('[data-out]');
	const call = $('[data-call]');
	const keep = $<HTMLInputElement>('[data-keep]');
	const btn = (a: string) => $<HTMLButtonElement>(`[data-act="${a}"]`);

	const fresh = (): State => ({
		rows: [7, 8, 9, 10].map((id) => ({ id, status: 'pending' as Status })),
		inflight: null,
		down: false,
		runs: {},
		msg: 'Four jobs are pending. Press Take a job to see the one statement that claims a row.',
	});
	let s = fresh();

	function render() {
		const running = s.inflight !== null;
		procs.replaceChildren(
			h(
				'div',
				{ class: 'ack__box', ...(s.down ? { 'data-down': true } : {}) },
				h('div', { class: 'ack__head' }, h('span', { text: 'process 1' }), h('b', { text: s.down ? 'died' : running ? 'running a job' : 'idle' })),
				h('div', { class: 'ack__slot' }, running ? job(s.inflight!, s.down ? 'is-lost' : 'is-run') : null, h('small', { text: running ? (s.down ? 'gone with the process' : `process(job ${s.inflight})`) : s.down ? 'down' : 'no job' })),
			),
		);
		body.replaceChildren(
			...s.rows.map((r) =>
				h(
					'tr',
					{ class: `is-${r.status}` },
					h('th', { scope: 'row', text: String(r.id) }),
					h('td', {}, h('span', { class: `flow__pill flow__pill--${r.status}`, text: r.status })),
					h('td', {}, h('code', { text: `{"id":"","status":"Created","data":"task ${r.id}"}` })),
				),
			),
		);
		if (!s.rows.length) body.append(h('tr', {}, h('td', { colspan: 3, class: 'flow__empty', text: 'no rows' })));

		const processing = s.rows.some((r) => r.status === 'processing');
		const pending = s.rows.some((r) => r.status === 'pending');
		btn('take').disabled = s.down || s.inflight !== null || !pending;
		btn('ack').disabled = s.down || s.inflight === null;
		btn('kill').hidden = s.down;
		btn('restart').hidden = !s.down;
		btn('second').disabled = !processing && !running;
		call.textContent = s.down
			? '// the process died: the row is still processing'
			: s.inflight === null
				? 'pq, err := sdb.NewQueue("jobs")  // resets processing rows, once per queue in this process'
				: '// one statement turns the row into processing';

		const tally = Object.entries(s.runs)
			.map(([id, n]) => `job ${id} started ${n}×`)
			.join(', ');
		out.replaceChildren(h('span', { text: s.msg }), ' ', tally && h('span', { class: 'ack__count', text: tally + '.' }));
	}

	const rowOf = (id: number) => s.rows.find((r) => r.id === id);

	root.closest('.ops-scene')!.addEventListener('click', (e) => {
		const el = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!el || el.disabled) return;
		const act = el.dataset.act;
		if (act === 'take') {
			const r = s.rows.find((x) => x.status === 'pending')!;
			r.status = 'processing';
			s.inflight = r.id;
			s.runs[r.id] = (s.runs[r.id] ?? 0) + 1;
			s.msg = `One statement turned the row of job ${r.id} from pending into processing. A worker with only this queue claims as many rows as it has free slots in one statement.`;
		} else if (act === 'ack') {
			const id = s.inflight!;
			const r = rowOf(id);
			if (r && r.status === 'processing') {
				if (keep.checked) {
					r.status = 'completed';
					s.msg = `Acknowledge marked the row of job ${id} as completed, because you keep completed rows. The ID of a completed row still blocks a repeat Add with the same ID.`;
				} else {
					s.rows = s.rows.filter((x) => x.id !== id);
					s.msg = `Acknowledge deleted the row of job ${id}. Once the row is gone, the ID can be added again.`;
				}
			} else {
				s.msg = `Acknowledge returned false: it only matches a row that is processing, and the row of job ${id} is ${r ? r.status : 'gone'}. The worker reports ErrAcknowledgeJob on Errs(). The function had already run.`;
			}
			s.inflight = null;
		} else if (act === 'kill') {
			s.down = true;
			s.msg = s.inflight === null ? 'The process died while no job was running. The rows are as they were.' : `The process died. The row of job ${s.inflight} is still processing in the file.`;
		} else if (act === 'restart') {
			s.down = false;
			s.inflight = null;
			let back = 0;
			for (const r of s.rows) if (r.status === 'processing') (r.status = 'pending'), back++;
			s.msg = back
				? `The program opened the queue. sqliteq set ${back === 1 ? 'the processing row' : 'every processing row'} back to pending, once, as the first process that opens this queue. There is no timeout. Job ${s.rows.find((r) => r.status === 'pending')?.id} will run again.`
				: 'The program opened the queue. No row was processing.';
		} else if (act === 'second') {
			let back = 0;
			for (const r of s.rows) if (r.status === 'processing') (r.status = 'pending'), back++;
			s.msg = s.inflight !== null && !s.down
				? `A second process opened the queue on the same file. sqliteq cannot tell which process took a row, so it set job ${s.inflight} back to pending while process 1 still runs it. Another worker can take it, and it runs twice. Use one process for each database file.`
				: back
					? 'A process opened the queue and set the processing rows back to pending.'
					: 'A process opened the queue. No row was processing.';
		} else if (act === 'reset') {
			s = fresh();
		}
		render();
	});

	render();
}
