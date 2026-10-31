// The `go-play` element: runs a Go program on the Go Playground and plays its output back in the page. GoPlay.astro
// (the docs pages) and home/Examples.astro (JOB 05 on the home page) each give it their own markup, with these hooks
// inside: `data-pins` (the go and varmq versions, from examples/go.mod), `[data-read]` (the code as read), `[data-editor]`
// (a textarea holding the source), the buttons `[data-run]`, `[data-edit]`, `[data-reset]` and `[data-open]`, an
// optional `[data-copy-src]` (copies the whole program, edits included), and `[data-output]` (a pre). Importing this
// module defines the element once.

import { compile, replay, requestBody, share, type OutKind, type Pins } from './playground';
import { copyText } from '../components/ui/dom';

const RUNNING = 'Running… (first run of an example can take ~15 s)';
const UNREACHABLE = "Couldn't reach the Go Playground. Try again, or open it there.";
const SHARE_FAILED = "Couldn't reach the Go Playground to share this code. Try again.";
const POPUP_BLOCKED = 'The browser blocked the new tab. Open this code in the Go Playground: ';
const TIMEOUT_MS = 60_000;

class GoPlay extends HTMLElement {
	pins!: Pins;
	read!: HTMLElement;
	editor!: HTMLTextAreaElement;
	runBtn!: HTMLButtonElement;
	editBtn!: HTMLButtonElement;
	resetBtn!: HTMLButtonElement;
	out!: HTMLPreElement;
	running = false;

	connectedCallback() {
		this.pins = JSON.parse(this.dataset.pins!);
		this.read = this.querySelector('[data-read]')!;
		this.editor = this.querySelector('[data-editor]')!;
		this.runBtn = this.querySelector('[data-run]')!;
		this.editBtn = this.querySelector('[data-edit]')!;
		this.resetBtn = this.querySelector('[data-reset]')!;
		this.out = this.querySelector('[data-output]')!;

		this.runBtn.addEventListener('click', () => this.run());
		this.editBtn.addEventListener('click', () => this.edit());
		this.resetBtn.addEventListener('click', () => this.reset());
		this.querySelector('[data-open]')!.addEventListener('click', () => this.open());
		const copy = this.querySelector<HTMLElement>('[data-copy-src]');
		copy?.addEventListener('click', () => void copyText(copy, this.editor.value, this.read));
		this.editor.addEventListener('keydown', (e) => this.onKey(e));
	}

	/** The request body for whatever the reader sees now: the original or their edit. */
	body() {
		return requestBody(this.editor.value, this.pins);
	}

	edit() {
		this.read.hidden = true;
		this.editor.hidden = false;
		this.editBtn.hidden = true;
		this.resetBtn.hidden = false;
		this.editor.focus();
	}

	reset() {
		this.editor.value = this.editor.defaultValue;
		this.editor.hidden = true;
		this.read.hidden = false;
		this.resetBtn.hidden = true;
		this.editBtn.hidden = false;
		this.editBtn.focus(); // Reset just hid itself; don't leave focus on nothing.
	}

	onKey(e: KeyboardEvent) {
		if (e.isComposing) return; // the keys belong to the input method
		if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
			e.preventDefault();
			this.run();
		} else if (e.key === 'Escape') {
			// Tab is taken for indenting, so Esc is the keyboard way out to the buttons.
			this.runBtn.focus();
		} else if (e.key === 'Tab' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
			e.preventDefault();
			// execCommand keeps the browser's undo history; setRangeText is the fallback.
			if (!document.execCommand('insertText', false, '\t')) {
				const { selectionStart: s, selectionEnd: end } = this.editor;
				this.editor.setRangeText('\t', s, end, 'end');
			}
		}
	}

	write(text: string, kind: OutKind) {
		const span = document.createElement('span');
		span.className = kind;
		span.textContent = text;
		this.out.append(span);
	}

	async run() {
		if (this.running) return;
		// aria-disabled, not disabled: a disabled button drops keyboard focus.
		this.running = true;
		this.runBtn.setAttribute('aria-disabled', 'true');
		this.runBtn.textContent = 'Running…';
		this.out.replaceChildren();
		this.write(RUNNING, 'system');

		const ctl = new AbortController();
		const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
		try {
			const res = await compile(this.body(), fetch, ctl.signal);
			// Clear on the first write, not now: replay may wait before it, and an
			// empty output collapses (pre:empty), which would make the box jump.
			let first = true;
			await replay(
				res,
				(text, kind) => {
					if (first) this.out.replaceChildren();
					first = false;
					this.write(text, kind);
				},
				(ms) => new Promise((r) => setTimeout(r, ms)),
			);
		} catch {
			this.out.replaceChildren();
			this.write(UNREACHABLE + '\n', 'error');
			const link = document.createElement('button');
			link.type = 'button';
			link.className = 'link';
			link.textContent = 'Open in Go Playground';
			link.addEventListener('click', () => this.open());
			this.out.append(link);
		} finally {
			clearTimeout(timer);
			this.running = false;
			this.runBtn.removeAttribute('aria-disabled');
			this.runBtn.textContent = 'Run';
		}
	}

	async open() {
		// Open the tab now, inside the click: a window.open after an await gets popup-blocked.
		const tab = window.open('', '_blank');
		if (tab) tab.opener = null;
		try {
			const url = await share(this.body());
			if (tab) {
				tab.location.href = url;
				return;
			}
			// Popup blocked. Leaving this page would lose an edit, so offer a link instead.
			if (!this.running) this.out.replaceChildren(); // don't wipe a run in progress
			this.write(POPUP_BLOCKED, 'system');
			const link = document.createElement('a');
			link.className = 'link';
			link.href = url;
			link.target = '_blank';
			link.rel = 'noopener';
			link.textContent = url;
			this.out.append(link, '\n');
		} catch {
			tab?.close();
			if (!this.running) this.out.replaceChildren(); // don't wipe a run in progress
			this.write(SHARE_FAILED + '\n', 'error');
		}
	}
}

if (!customElements.get('go-play')) customElements.define('go-play', GoPlay);
