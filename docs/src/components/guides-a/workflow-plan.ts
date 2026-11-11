// The workflows the workflow scene runs. `col` places a task in the drawing; the rest is a TaskPlan (wfmachine.ts).

export interface SceneTask {
	name: string;
	deps?: string[];
	always?: boolean;
	/** The undo job's data: the task has an undo. */
	undo?: string;
	/** What the task's error says when the reader picks it to fail. */
	error: string;
	/** What a result worker returns; none on an error worker. */
	result?: string;
	col: number;
}

export interface Example {
	key: string;
	label: string;
	tasks: SceneTask[];
	/** The Go calls that build it, shown before Submit. */
	call: string;
	/** The task that fails at first: 'none' or a task name. */
	fail: string;
	/** Whether the failure controls show: the task that fails, WithFailFast and Always. */
	options: boolean;
	note: string;
}

export const EXAMPLES: Example[] = [
	{
		// The smallest workflow: three tasks in a chain (docs/examples/workflows-chain, the page's first steps).
		key: 'simple',
		label: 'Simple',
		tasks: [
			{ name: 'download', error: 'not found', col: 0 },
			{ name: 'resize', deps: ['download'], error: 'resize failed', col: 1 },
			{ name: 'save', deps: ['resize'], error: 'disk full', col: 2 },
		],
		call: [
			'download := wf.Add("download", steps, "download photo.png")',
			'resize := wf.Add("resize", steps, "resize to 64px").After(download)',
			'wf.Add("save", steps, "save to disk").After(resize)',
		].join('\n'),
		fail: 'none',
		options: false,
		note: 'Press Submit. resize waits for download. save waits for resize. This is the workflow of the steps above.',
	},
	{
		key: 'undo',
		label: 'Failure and undo',
		tasks: [
			{ name: 'reserve', undo: 'release stock', error: 'out of stock', col: 0 },
			{ name: 'charge', deps: ['reserve'], undo: 'refund', error: 'card declined', col: 1 },
			{ name: 'label', deps: ['reserve'], error: 'printer offline', col: 1 },
			{ name: 'ship', deps: ['charge', 'label'], error: 'no courier', col: 2 },
			{ name: 'notify', deps: ['ship'], always: true, error: 'mail bounced', col: 3 },
		],
		call: [
			'r := wf.Add("reserve", stock, sku).Undo(undos, "release stock")',
			'c := wf.Add("charge", pay, 100).After(r).Undo(undos, "refund")',
			'l := wf.Add("label", print, sku).After(r)',
			's := wf.Add("ship", ship, sku).After(c, l)',
			'wf.Add("notify", mail, user).After(s).Always()',
		].join('\n'),
		fail: 'charge',
		options: true,
		note: 'charge fails, so ship is cancelled. notify still runs, because it has Always. Then the undo job of reserve, release stock, runs. Pick ship to see how Always absorbs a failure. Turn on WithFailFast and notify is cancelled too.',
	},
];
