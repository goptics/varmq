import { test } from 'node:test';
import assert from 'node:assert/strict';
import { highlightGo, highlightShell, highlightJson, escapeHtml } from './go.ts';

test('highlightGo marks keywords, calls, types, strings, numbers and comments', () => {
	const html = highlightGo('func f() { x := varmq.NewWorker(fn, 4) // go\n return "a" }');
	assert.match(html, /<span class="tk-k">func<\/span>/);
	assert.match(html, /<span class="tk-f">NewWorker<\/span>/);
	assert.match(html, /<span class="tk-n">4<\/span>/);
	assert.match(html, /<span class="tk-c">\/\/ go<\/span>/);
	assert.match(html, /<span class="tk-s">"a"<\/span>/);
	assert.match(highlightGo('var j varmq.Job'), /<span class="tk-t">Job<\/span>/);
});

test('highlightGo escapes markup', () => {
	assert.equal(escapeHtml('<a & b>'), '&lt;a &amp; b&gt;');
	assert.ok(!highlightGo('a < b').includes('< b'));
});

test('highlightShell marks the command, flags and comments', () => {
	const html = highlightShell('# note\ngo get -u "pkg"');
	assert.match(html, /<span class="tk-c"># note<\/span>/);
	assert.match(html, /<span class="tk-f">go<\/span>/);
	assert.match(html, /<span class="tk-b">-u<\/span>/);
	assert.match(html, /<span class="tk-s">"pkg"<\/span>/);
});

test('highlightJson marks keys, strings, numbers and literals', () => {
	const html = highlightJson('{"a": 1, "b": "x", "c": true}');
	assert.match(html, /tk-t/);
	assert.match(html, /<span class="tk-n">1<\/span>/);
	assert.match(html, /<span class="tk-k">true<\/span>/);
});
