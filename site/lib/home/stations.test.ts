import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, it } from 'node:test';
import { CHAPTERS } from '../heromap/chapters.ts';
import { STATIONS } from './stations.ts';

const ROOT = join(import.meta.dirname, '../../..');
// varmq's exported Go API, and the adapters' calls as the persistence pages write them.
const go = readdirSync(ROOT)
	.filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
	.map((f) => readFileSync(join(ROOT, f), 'utf8'))
	.join('\n');
const pages = ['sqlite', 'redis']
	.map((p) => readFileSync(join(ROOT, `docs/src/content/docs/persistence/${p}.mdx`), 'utf8'))
	.join('\n');

// Each station's code as a phone shows it: its phone version where it has one.
const onPhone = (s: (typeof STATIONS)[number]) => s.phone ?? { code: s.code, lines: s.lines };
const width = (line: string, tab: number) => line.replaceAll('\t', ' '.repeat(tab)).length;
// Every code a station shows, wide and on a phone: its own, then each it changes to within the step.
const wide = (s: (typeof STATIONS)[number]) => [s.code, ...(s.then ?? []).map((t) => t.code)];
const narrow = (s: (typeof STATIONS)[number]) => [onPhone(s).code, ...(s.then ?? []).map((t) => t.phone ?? t.code)];

describe('home stations', () => {
	it('has a station per chapter and a line list per caption, in both versions', () => {
		assert.equal(STATIONS.length, CHAPTERS.length);
		STATIONS.forEach((s, i) => {
			for (const v of [s, onPhone(s)]) {
				const lines = v.code.split('\n').length;
				assert.equal(v.lines.length, CHAPTERS[i].captions.length, `station ${i + 1}`);
				for (const n of v.lines.flat()) assert.ok(n >= 1 && n <= lines, `station ${i + 1} line ${n}`);
			}
			// A code it changes to keeps the lines, so the line lists hold for it, and it changes at most once a caption.
			for (const codes of [wide(s), narrow(s)]) for (const code of codes) assert.equal(code.split('\n').length, codes[0].split('\n').length, `station ${i + 1}`);
			assert.ok((s.then?.length ?? 0) < CHAPTERS[i].captions.length, `station ${i + 1}`);
		});
	});

	it('fits 56 columns wide and 40 on a phone, and has a phone version only where it needs one', () => {
		for (const [i, s] of STATIONS.entries()) {
			for (const line of wide(s).join('\n').split('\n')) assert.ok(width(line, 4) <= 56, `station ${i + 1}: ${line}`);
			for (const line of narrow(s).join('\n').split('\n')) assert.ok(width(line, 2) <= 40, `station ${i + 1} on a phone: ${line}`);
			const fits = s.code.split('\n').every((line) => width(line, 2) <= 40);
			assert.equal(!!s.phone, !fits, `station ${i + 1}: a phone version ${fits ? 'it does not need' : 'is missing'}`);
		}
	});

	it('only calls names that varmq exports, or that the persistence pages use', () => {
		for (const [i, station] of STATIONS.entries()) {
			for (const code of [...wide(station), ...narrow(station)]) {
				const s = { code };
				for (const [, pkg, name] of s.code.matchAll(/\b(varmq|sqliteq|redisq)\.\s*([A-Z]\w*)/g)) {
					const found = pkg === 'varmq' ? new RegExp(`^(func|type|const|var)\\b.*\\b${name}\\b|^\\t${name}\\b`, 'm').test(go) : pages.includes(`${pkg}.${name}(`);
					assert.ok(found, `station ${i + 1}: ${pkg}.${name}`);
				}
				// Methods on the excerpt's own values; package functions, the standard library's included, are not methods.
				for (const [, name] of s.code.matchAll(/(?<!\b(?:varmq|sqliteq|redisq|fmt|strconv))\.([A-Z]\w*)\(/g)) {
					assert.ok(new RegExp(`^func \\([^)]*\\) ${name}\\b|^\\t${name}\\(`, 'm').test(go) || pages.includes(`.${name}(`), `station ${i + 1}: .${name}()`);
				}
			}
		}
	});
});
