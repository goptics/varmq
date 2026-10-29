'use client';
import { Check, Copy } from 'lucide-react';
import { useRef, useState } from 'react';

/** The install command in its send slot, with a button that copies it; where the clipboard is refused, it selects it. */
export function CopyCommand({ command }: { command: string }) {
	const text = useRef<HTMLElement>(null);
	const [copied, setCopied] = useState(false);

	async function copy() {
		try {
			await navigator.clipboard.writeText(command);
			setCopied(true);
			setTimeout(() => setCopied(false), 1600);
		} catch {
			getSelection()?.selectAllChildren(text.current!);
		}
	}

	return (
		<div className="hp-send">
			<code ref={text}>{command}</code>
			<button type="button" onClick={copy} aria-label="Copy the install command">
				{copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
			</button>
			<span className="hp-sr" aria-live="polite">
				{copied ? 'Copied' : ''}
			</span>
		</div>
	);
}
