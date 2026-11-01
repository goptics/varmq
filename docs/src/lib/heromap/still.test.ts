import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CHAPTERS } from './chapters.ts';
import { CONNS, PIECES, REGIONS, RINGS, VIEW, frame } from './scene.ts';
import { StillDocument, stillMap } from './still.ts';

const count = (html, re) => (html.match(re) ?? []).length;

describe('still map', () => {
	const { html, style } = stillMap();

	it('is the same markup every build', () => {
		assert.equal(stillMap().html, html);
		assert.equal(stillMap().style, style);
	});
	it('is the grid svg, then the map svg holding the world at the finale camera', () => {
		assert.equal(count(html, /<svg /g), 2);
		assert.match(html, /^<svg class="vm-svg vm-under" viewBox="0 0 400 400">/);
		assert.match(html, /<\/svg><svg class="vm-svg" viewBox="0 0 400 400">/);
		const { x, y, s } = frame(REGIONS[REGIONS.length - 1]);
		const transform = `translate(${VIEW.w / 2 - x * s} ${VIEW.mapH / 2 - y * s}) scale(${s})`;
		assert.ok(html.includes(`<g class="vm-world" transform="${transform}" style="--vm-label: `), 'world');
		assert.ok(html.includes(`patternTransform="${transform}"`), 'grid');
		assert.match(style, /^--vm-feather: [\d.]+%;$/);
	});
	it('has every piece, chip, line and ring that render draws', () => {
		const chips = CHAPTERS.reduce((n, c) => n + (c.chips?.length ?? 0), 0);
		assert.equal(count(html, /class="vm-piece /g), PIECES.length + chips);
		assert.equal(count(html, /class="vm-conn /g), CONNS.length);
		const rings = Object.values(RINGS).reduce((n, places) => n + places.length, 0);
		assert.equal(count(html, /class="vm-ring"/g), rings);
		assert.equal(count(html, /class="vm-ghost"/g), rings);
	});
	it('escapes text and attribute values', () => {
		const el = new StillDocument().createElementNS('', 'text');
		el.setAttribute('data-x', 'a & b < c > d "e"');
		el.textContent = 'Tom & "Jerry" <3 >';
		assert.equal(el.outerHTML, '<text data-x="a &amp; b &lt; c &gt; d &quot;e&quot;">Tom &amp; "Jerry" &lt;3 &gt;</text>');
	});
});
