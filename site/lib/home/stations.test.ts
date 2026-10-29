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

describe('home stations', () => {
	it('has a station per chapter and a line list per caption, in both versions', () => {
		assert.equal(STATIONS.length, CHAPTERS.length);
		STATIONS.forEach((s, i) => {
			for (const v of [s, onPhone(s)]) {
				const lines = v.code.split('\n').length;
				assert.equal(v.lines.length, CHAPTERS[i].captions.length, `station ${i + 1}`);
				for (const n of v.lines.flat()) assert.ok(n >= 1 && n <= lines, `station ${i + 1} line ${n}`);
			}
		});
	});

	it('fits 56 columns wide and 40 on a phone, and has a phone version only where it needs one', () => {
		for (const [i, s] of STATIONS.entries()) {
			for (const line of s.code.split('\n')) assert.ok(width(line, 4) <= 56, `station ${i + 1}: ${line}`);
			for (const line of onPhone(s).code.split('\n')) assert.ok(width(line, 2) <= 40, `station ${i + 1} on a phone: ${line}`);
			const fits = s.code.split('\n').every((line) => width(line, 2) <= 40);
			assert.equal(!!s.phone, !fits, `station ${i + 1}: a phone version ${fits ? 'it does not need' : 'is missing'}`);
		}
	});

	it('only calls names that varmq exports, or that the persistence pages use', () => {
		for (const [i, station] of STATIONS.entries()) {
			for (const s of [station, onPhone(station)]) {
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
