// Frames for the three small scenes. A cell is [column, row, opacity, state].
// State is 'next', 'run' or 'dim', with an optional order badge such as 'dim:2'.
export type Cell = [c: number, r: number, o: number, state?: string];
export interface Frame {
	chips: Record<string, Cell>;
	cap: string;
	w?: string;
}
export interface SceneDef {
	frames: Frame[];
}

const priority: Frame[] = [
	{
		cap: 'varmq.NewWorker(fn, varmq.WithAutoRun(false))',
		chips: { r: [0, 0, 0], a: [1, 0, 0], d: [2, 0, 0] },
	},
	{ cap: 'queue.Add("report", 10)', chips: { r: [0, 0, 1], a: [1, 0, 0], d: [2, 0, 0] } },
	{ cap: 'queue.Add("alert", 1)', chips: { r: [0, 0, 1], a: [1, 0, 1], d: [2, 0, 0] } },
	{ cap: 'queue.Add("digest", 10)', chips: { r: [0, 0, 1], a: [1, 0, 1], d: [2, 0, 1] } },
	{
		cap: 'worker.Start()',
		chips: { a: [0, 0, 1, 'next'], r: [1, 0, 1], d: [2, 0, 1] },
	},
	{
		cap: '// alert has the lowest number',
		chips: { a: [0, 2, 1, 'run'], r: [0, 0, 1, 'next'], d: [1, 0, 1] },
	},
	{
		cap: '// report and digest tie: first added runs first',
		chips: { a: [1, 2, 1, 'dim'], r: [0, 2, 1, 'run'], d: [0, 0, 1, 'next'] },
	},
	{
		cap: '// digest runs last',
		chips: { a: [1, 2, 1, 'dim'], r: [2, 2, 1, 'dim'], d: [0, 2, 1, 'run'] },
	},
	{
		cap: '// ran: alert, report, digest',
		chips: { a: [1, 2, 1, 'dim'], r: [2, 2, 1, 'dim'], d: [3, 2, 1, 'dim'] },
	},
];

// Taken in turn: alert 1, report 1, email 1, alert 2, email 2, email 3.
const order = ['a1', 'r1', 'e1', 'a2', 'e2', 'e3'];
const home: Record<string, [number, number]> = {
	a1: [0, 0],
	a2: [1, 0],
	r1: [0, 1],
	e1: [0, 2],
	e2: [1, 2],
	e3: [2, 2],
};
const labels: Record<string, string> = {
	a1: 'alert 1',
	a2: 'alert 2',
	r1: 'report 1',
	e1: 'email 1',
	e2: 'email 2',
	e3: 'email 3',
};
const turnCaps = [
	'varmq.WithStrategy(varmq.RoundRobin)',
	'// turn 1: alerts',
	'// turn 2: reports',
	'// turn 3: emails',
	'// back to alerts',
	'// reports is empty, so it is skipped',
	'// only emails are left',
	'// one worker, three queues, in turn',
];
const strategy: Frame[] = turnCaps.map((cap, step) => {
	const chips: Record<string, Cell> = {};
	for (const k of order) {
		const idx = order.indexOf(k) + 1;
		const [c, r] = home[k];
		if (step === 0 || idx > step) chips[k] = [c, r, 1];
		else if (idx === step && step < 7) chips[k] = [c, r, 1, `run:${idx}`];
		else chips[k] = [c, r, 1, `dim:${idx}`];
	}
	const cur = order[step - 1];
	return { cap, chips, w: step >= 1 && step < 7 ? labels[cur] : '' };
});

const topic: Frame[] = [
	{ cap: 'topic := varmq.NewTopic[int]()', chips: { p: [0, 1, 0], e: [0, 1, 0], s: [0, 1, 0] } },
	{ cap: 'topic.Publish(7)', chips: { p: [0, 1, 1], e: [0, 1, 0], s: [0, 1, 0] } },
	{
		cap: '// each worker gets its own job',
		chips: { p: [0, 1, 1, 'dim'], e: [3, 0, 1, 'run'], s: [3, 2, 1, 'run'] },
	},
	{
		cap: '// email and stock both ran order 7',
		chips: { p: [0, 1, 1, 'dim'], e: [3, 0, 1, 'dim'], s: [3, 2, 1, 'dim'] },
	},
];

export const SCENES: Record<string, SceneDef> = {
	priority: { frames: priority },
	strategy: { frames: strategy },
	topic: { frames: topic },
};

export const finalFrame = (key: string): Frame => {
	const f = SCENES[key].frames;
	return f[f.length - 1];
};
