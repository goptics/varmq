// The hero map's still frame as markup, for the page's HTML: the frame `mountMap` (story/map.ts) draws first, there
// at first paint rather than once the story's code has loaded. `renderMap` runs here against a small document of
// its own, whose elements hold only what it uses (createElementNS, setAttribute, getAttribute, textContent, append,
// replaceChildren and style's setProperty and getPropertyValue), and write themselves out as markup. The client's
// `renderMap` replaces this markup with live nodes of the same frame.

import { renderMap } from './render.ts';
import { REGIONS, frame } from './scene.ts';

const escape = (s: string, attr: boolean) =>
	s.replace(attr ? /[&<>"]/g : /[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!);

export class StillElement {
	readonly attrs = new Map<string, string>();
	children: (StillElement | string)[] = [];
	private props = new Map<string, string>();
	readonly style = {
		setProperty: (name: string, value: string) => {
			this.props.set(name, value);
			this.attrs.set('style', [...this.props].map(([n, v]) => `${n}: ${v};`).join(' '));
		},
		getPropertyValue: (name: string) => this.props.get(name) ?? '',
	};

	readonly ownerDocument: StillDocument;
	readonly tag: string;
	constructor(ownerDocument: StillDocument, tag: string) {
		this.ownerDocument = ownerDocument;
		this.tag = tag;
	}

	setAttribute(name: string, value: string) {
		this.attrs.set(name, String(value));
	}
	getAttribute(name: string): string | null {
		return this.attrs.get(name) ?? null;
	}
	set textContent(text: string) {
		this.children = [text];
	}
	append(...nodes: (StillElement | string)[]) {
		this.children.push(...nodes);
	}
	replaceChildren(...nodes: (StillElement | string)[]) {
		this.children = nodes;
	}
	/** The element's children as markup. */
	get innerHTML(): string {
		return this.children.map((c) => (typeof c === 'string' ? escape(c, false) : c.outerHTML)).join('');
	}
	get outerHTML(): string {
		const attrs = [...this.attrs].map(([n, v]) => ` ${n}="${escape(v, true)}"`).join('');
		return `<${this.tag}${attrs}>${this.innerHTML}</${this.tag}>`;
	}
}

export class StillDocument {
	createElementNS(_ns: string, tag: string) {
		return new StillElement(this, tag);
	}
}

/** The still frame: the markup inside `varmq-map`, and the host's own inline style (its `--vm-feather`). */
export function stillMap(): { html: string; style: string } {
	const host = new StillDocument().createElementNS('', 'varmq-map');
	const nodes = renderMap(host as unknown as HTMLElement);
	// The square's width on screen is the browser's to know: the light map's line floor (render.ts, --vm-px) is set as
	// for a 400 px square, a phone's, until the client draws its own; on a desktop the lines are a little heavier until
	// then.
	nodes.setCamera(frame(REGIONS[REGIONS.length - 1]));
	return { html: host.innerHTML, style: host.getAttribute('style') ?? '' };
}
