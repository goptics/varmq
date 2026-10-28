export const PLAYGROUND = 'https://play.golang.org';

export type PlayEvent = { Message: string; Kind: 'stdout' | 'stderr'; Delay: number }; // Delay in ns
export type CompileResult = { Errors: string; Events: PlayEvent[] | null; Status?: number };
export type OutKind = 'stdout' | 'stderr' | 'error' | 'system';
export type Pins = { go: string; varmq: string };

/** Read the `go` directive and the varmq version out of a go.mod. */
export function pins(goMod: string): Pins {
	const go = /^go[ \t]+(\S+)/m.exec(goMod)?.[1];
	if (!go) throw new Error('go directive not found in go.mod');
	// Matches both `require github.com/goptics/varmq vX` and the block-form line.
	const varmq = /github\.com\/goptics\/varmq[ \t]+(v\S+)/.exec(goMod)?.[1];
	if (!varmq) throw new Error('varmq require not found in go.mod');
	return { go, varmq };
}

/** The playground takes a txtar archive: the program followed by its go.mod. */
export function requestBody(source: string, p: Pins): string {
	return `${source}\n-- go.mod --\nmodule play.ground\n\ngo ${p.go}\n\nrequire github.com/goptics/varmq ${p.varmq}\n`;
}

async function ok(res: Response): Promise<Response> {
	if (!res.ok) throw new Error(`playground returned ${res.status}`);
	return res;
}

export async function compile(
	body: string,
	fetchFn: typeof fetch = fetch,
	signal?: AbortSignal,
): Promise<CompileResult> {
	// A URLSearchParams body keeps this a CORS simple request: no custom headers, no preflight.
	const res = await ok(
		await fetchFn(`${PLAYGROUND}/compile`, {
			method: 'POST',
			body: new URLSearchParams({ version: '2', body }),
			signal,
		}),
	);
	return (await res.json()) as CompileResult;
}

/** Store the snippet and return its shareable go.dev URL. */
export async function share(body: string, fetchFn: typeof fetch = fetch): Promise<string> {
	// A deadline, so a stalled request rejects and the caller can close its blank tab.
	const signal = AbortSignal.timeout(15_000);
	const res = await ok(await fetchFn(`${PLAYGROUND}/share`, { method: 'POST', body, signal }));
	return `https://go.dev/play/p/${(await res.text()).trim()}`;
}

/** Play a compile result back the way the playground does: events in order, honouring their delays. */
export async function replay(
	res: CompileResult,
	write: (text: string, kind: OutKind) => void,
	sleep: (ms: number) => Promise<void>,
): Promise<void> {
	if (res.Errors) {
		write(res.Errors, 'error');
		return;
	}
	for (const ev of res.Events ?? []) {
		if (ev.Delay > 0) await sleep(ev.Delay / 1e6);
		write(ev.Message, ev.Kind);
	}
	write(res.Status ? `\nProgram exited: status ${res.Status}.\n` : '\nProgram exited.\n', 'system');
}
