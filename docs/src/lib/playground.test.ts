import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
	compile,
	pins,
	replay,
	requestBody,
	share,
	type CompileResult,
	type OutKind,
} from './playground.ts';

describe('pins', () => {
	it('parses single-line require', () =>
		assert.deepEqual(pins('module x\n\ngo 1.24.0\n\nrequire github.com/goptics/varmq v1.10.0\n'), {
			go: '1.24.0',
			varmq: 'v1.10.0',
		}));
	it('parses block form', () =>
		assert.deepEqual(
			pins(
				'module x\n\ngo 1.24.0\n\nrequire (\n\tgithub.com/goptics/varmq v1.10.0\n\tgithub.com/x/y v1.0.0 // indirect\n)\n',
			),
			{ go: '1.24.0', varmq: 'v1.10.0' },
		));
	it('throws when varmq is not required', () =>
		assert.throws(() => pins('module x\n\ngo 1.24.0\n'), /varmq require not found/));
	it('throws when the go line is missing', () =>
		assert.throws(
			() => pins('module x\n\nrequire github.com/goptics/varmq v1.10.0\n'),
			/go directive not found/,
		));
});

describe('requestBody', () => {
	it('appends the pinned go.mod', () =>
		assert.equal(
			requestBody('package main\n', { go: '1.24.0', varmq: 'v1.10.0' }),
			'package main\n\n-- go.mod --\nmodule play.ground\n\ngo 1.24.0\n\nrequire github.com/goptics/varmq v1.10.0\n',
		));
});

describe('compile', () => {
	it('posts form-encoded version=2 and body', async () => {
		let url: RequestInfo | URL | undefined;
		let init: RequestInit | undefined;
		const stub: typeof fetch = async (u, i) => {
			url = u;
			init = i;
			return new Response('{"Errors":"","Events":[]}');
		};
		const res = await compile('B', stub);
		const form = init?.body as URLSearchParams;
		assert.equal(url, 'https://play.golang.org/compile');
		assert.equal(init?.method, 'POST');
		assert.equal(form.get('version'), '2');
		assert.equal(form.get('body'), 'B');
		assert.deepEqual(res, { Errors: '', Events: [] });
	});
	it('throws on non-200', async () =>
		assert.rejects(
			compile('B', async () => new Response('', { status: 503 })),
			/playground returned 503/,
		));
});

describe('share', () => {
	it('returns the go.dev snippet URL', async () =>
		assert.equal(
			await share('B', async () => new Response('abc123')),
			'https://go.dev/play/p/abc123',
		));
	it('throws on non-200', async () =>
		assert.rejects(
			share('B', async () => new Response('', { status: 500 })),
			/playground returned 500/,
		));
	it('sends a deadline signal', async () => {
		let signal: AbortSignal | null | undefined;
		await share('B', async (_url, init) => {
			signal = init?.signal;
			return new Response('abc123');
		});
		assert.ok(signal instanceof AbortSignal);
	});
});

describe('replay', () => {
	const run = async (res: CompileResult) => {
		const writes: [string, OutKind][] = [];
		const sleeps: number[] = [];
		await replay(
			res,
			(t, k) => writes.push([t, k]),
			async (ms) => {
				sleeps.push(ms);
			},
		);
		return { writes, sleeps };
	};
	it('replays events with their delays', async () => {
		const { writes, sleeps } = await run({
			Errors: '',
			Status: 0,
			Events: [
				{ Message: 'a\n', Kind: 'stdout', Delay: 0 },
				{ Message: 'b\n', Kind: 'stderr', Delay: 2e9 },
			],
		});
		assert.deepEqual(writes, [
			['a\n', 'stdout'],
			['b\n', 'stderr'],
			['\nProgram exited.\n', 'system'],
		]);
		assert.deepEqual(sleeps, [2000]);
	});
	it('shows compile errors only', async () => {
		const { writes } = await run({ Errors: './prog.go:3:1: syntax error', Events: null });
		assert.deepEqual(writes, [['./prog.go:3:1: syntax error', 'error']]);
	});
	it('reports non-zero status', async () => {
		const { writes } = await run({
			Errors: '',
			Status: 2,
			Events: [{ Message: 'panic: boom\n', Kind: 'stderr', Delay: 0 }],
		});
		assert.deepEqual(writes.at(-1), ['\nProgram exited: status 2.\n', 'system']);
	});
	it('handles null events', async () => {
		const { writes } = await run({ Errors: '', Events: null });
		assert.deepEqual(writes, [['\nProgram exited.\n', 'system']]);
	});
});
