import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// The configuration and errors guides say they list every option and every exported error. These checks hold them to
// the Go source in the repository root, so a new With, Default or Err value fails the docs tests until it is listed.
const root = new URL('../../../', import.meta.url);
const go = readdirSync(root)
	.filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
	.map((f) => readFileSync(new URL(f, root), 'utf8'))
	.join('\n');
const page = (path: string) => readFileSync(new URL(`../content/docs/${path}`, import.meta.url), 'utf8');
const names = (text: string, re: RegExp) => {
	const found = [...new Set([...text.matchAll(re)].map((m) => m[1]))].sort();
	assert.ok(found.length > 0, `nothing matched ${re}`);
	return found;
};

describe('API tables', () => {
	it('configuration lists every exported With and Default function', () => {
		assert.deepEqual(
			names(page('guides/configuration.mdx'), /^\| \[`((?:With|Default)\w+)`\]/gm),
			names(go, /^func ((?:With|Default)\w+)\(/gm),
		);
	});

	it('errors lists every exported Err value', () => {
		assert.deepEqual(names(page('guides/errors.mdx'), /^\| \[?`(Err\w+)`/gm), names(go, /^\s*(Err\w+)\s*=/gm));
	});
});
