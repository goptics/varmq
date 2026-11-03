import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { pageMarkdown } from './page-markdown.ts';

const page = { title: 'Topics', description: 'One to many.' };
const source = (name: string) => `package main // ${name}\n`;

describe('pageMarkdown', () => {
	it('puts the title first and drops the frontmatter and imports', () => {
		const mdx = "---\ntitle: Topics\n---\n\nimport GoPlay from '../GoPlay.astro';\n\nText.\n";
		assert.equal(pageMarkdown(mdx, page, source), '# Topics\n\nOne to many.\n\nText.\n');
	});

	it('leaves out a description the page lacks', () => {
		assert.equal(pageMarkdown('Text.\n', { title: 'Topics' }, source), '# Topics\n\nText.\n');
	});

	it('turns a GoPlay into a Go fence with its program', () => {
		const out = pageMarkdown('<GoPlay example="topic-subscribe" />\n', page, source);
		assert.match(out, /```go\npackage main \/\/ topic-subscribe\n```/);
	});

	it('drops a scene, also one on several lines', () => {
		const out = pageMarkdown('Before.\n\n<PoolScene />\n\n<SetupSteps\n\tmode="redis"\n/>\n\nAfter.\n', page, source);
		assert.equal(out, '# Topics\n\nOne to many.\n\nBefore.\n\nAfter.\n');
	});

	it('makes site links and diagrams absolute', () => {
		const out = pageMarkdown('[a](/guides/queues/) ![b](../../../../../assets/diagrams/x.svg) [c](https://go.dev/)\n', page, source);
		assert.match(out, /\[a\]\(https:\/\/varmq\.goptics\.org\/guides\/queues\/\)/);
		assert.match(out, /!\[b\]\(https:\/\/raw\.githubusercontent\.com\/goptics\/varmq\/main\/assets\/diagrams\/x\.svg\)/);
		assert.match(out, /\[c\]\(https:\/\/go\.dev\/\)/);
	});

	it('fills in a page\'s expressions', () => {
		const mdx = "export const repo = 'https://x.test';\n\nBuilt {data.day}, commit <a href={`${repo}/c`}><code>{data.sha}</code></a>, in <a href={repo}>the repo</a>.\n";
		assert.equal(pageMarkdown(mdx, { title: 'T' }, source, { day: 'Monday', sha: 'abc' }), '# T\n\nBuilt Monday, commit [`abc`](https://x.test/c), in [the repo](https://x.test).\n');
	});

	it('leaves braces alone on a page without expressions', () => {
		assert.match(pageMarkdown('`/workers/{name}` and {name}\n', page, source), /`\/workers\/\{name\}` and \{name\}/);
	});

	it('drops a fence\'s fold meta', () => {
		assert.match(pageMarkdown('```go collapse={1-10}\nx\n```\n', page, source), /```go\nx\n```/);
	});

	it('leaves code fences alone', () => {
		const fence = '```go\nimport "fmt" from x\n<Foo />\n[a](/b)\n```';
		assert.ok(pageMarkdown(`${fence}\n`, page, source).includes(fence));
	});
});
