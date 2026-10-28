// Runs every example through the Go Playground once, the way the Run button does.
//
// The playground caches builds by request body, so the first visitor to run an
// example pays for a cold compile. Sending the exact body GoPlay sends warms
// that cache, and it flags an example that runs locally but not in the sandbox.
//
//   node docs/scripts/warm-playground.ts   (Node 22.18+ strips the types itself)
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { compile, pins, requestBody, type CompileResult } from '../src/lib/playground.ts';

const TIMEOUT_MS = 60_000; // same ceiling as the Run button

const examples = new URL('../examples/', import.meta.url);
const p = pins(readFileSync(new URL('go.mod', examples), 'utf8'));

const names = readdirSync(examples, { withFileTypes: true })
	.filter((d) => d.isDirectory() && existsSync(new URL(`${d.name}/main.go`, examples)))
	.map((d) => d.name)
	.sort();
if (names.length === 0) throw new Error('no examples found');

/** Why a compile result counts as a failure, or undefined if the example ran cleanly. */
function problem(res: CompileResult): string | undefined {
	if (res.Errors) return res.Errors.trim();
	const events = res.Events ?? [];
	if (res.Status) {
		const stderr = events.filter((e) => e.Kind === 'stderr').map((e) => e.Message).join('').trim();
		return `exited with status ${res.Status}${stderr ? `\n${stderr}` : ''}`;
	}
	if (!events.some((e) => e.Kind === 'stdout' && e.Message.trim())) return 'printed nothing';
	return undefined;
}

let failed = 0;
for (const name of names) {
	try {
		// Read the file raw: this has to match what GoPlay puts in its request byte for byte.
		const source = readFileSync(new URL(`${name}/main.go`, examples), 'utf8');
		const res = await compile(requestBody(source, p), fetch, AbortSignal.timeout(TIMEOUT_MS));
		const why = problem(res);
		if (why === undefined) {
			console.log(`ok ${name}`);
			continue;
		}
		console.log(`fail ${name}: ${why}`);
	} catch (err) {
		console.log(`fail ${name}: ${err instanceof Error ? err.message : String(err)}`);
	}
	failed++;
}
process.exitCode = failed ? 1 : 0;
