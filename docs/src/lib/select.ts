// Every <select> on the site draws the same dropdown, since a browser's own list cannot be styled. The native select
// stays in the DOM, hidden, and stays the source of truth: page scripts read and set it, and hear its `input` and
// `change` as before. Without JS the select itself shows. The list is a popover, so a scene's `overflow: hidden` cannot
// clip it, and Escape and a click outside close it. Styles: ui.css, "Fields".
let lists = 0;

const nameOf = (select: HTMLSelectElement) =>
	select.getAttribute('aria-label') ??
	[...(select.labels[0]?.childNodes ?? [])]
		.filter((n) => n !== select)
		.map((n) => n.textContent)
		.join('')
		.trim();

export function upgrade(select: HTMLSelectElement) {
	const id = `select-${++lists}`;
	const name = nameOf(select);
	const button = document.createElement('button');
	button.type = 'button';
	button.className = 'select';
	button.setAttribute('role', 'combobox');
	button.setAttribute('aria-haspopup', 'listbox');
	button.setAttribute('aria-expanded', 'false');
	button.setAttribute('aria-controls', id);
	button.setAttribute('aria-label', name);
	button.innerHTML =
		'<span class="select__label"></span><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 6l4 4 4-4" /></svg>';
	const label = button.querySelector('span')!;

	const list = document.createElement('ul');
	list.id = id;
	list.className = 'select__list';
	list.popover = 'auto';
	list.setAttribute('role', 'listbox');
	list.setAttribute('aria-label', name);
	button.popoverTargetElement = list;

	const options = () => [...list.children] as HTMLElement[];
	const isOpen = () => list.matches(':popover-open');
	let active = -1;

	const setActive = (i: number) => {
		options()[active]?.removeAttribute('data-active');
		active = i;
		const li = options()[i];
		li?.setAttribute('data-active', '');
		li?.scrollIntoView({ block: 'nearest' });
		if (li) button.setAttribute('aria-activedescendant', li.id);
		else button.removeAttribute('aria-activedescendant');
	};

	// The next option that can be picked from `from`, or `from` itself at the end of the list.
	const step = (from: number, by: 1 | -1) => {
		for (let i = from + by; i >= 0 && i < select.options.length; i += by) if (!select.options[i].disabled) return i;
		return from;
	};

	const render = () => {
		const opts = [...select.options];
		// Every label sits in one grid cell, unseen, so the button is as wide as the longest, as a native select is.
		label.replaceChildren(
			...opts.map((o) => Object.assign(document.createElement('span'), { textContent: o.text, ariaHidden: 'true' })),
			Object.assign(document.createElement('span'), { textContent: select.selectedOptions[0]?.text ?? '' }),
		);
		list.replaceChildren(
			...opts.map((o, i) => {
				const li = document.createElement('li');
				li.id = `${id}-${i}`;
				li.textContent = o.text;
				li.setAttribute('role', 'option');
				li.setAttribute('aria-selected', String(o.selected));
				if (o.disabled) li.setAttribute('aria-disabled', 'true');
				return li;
			}),
		);
		button.disabled = select.disabled;
		active = -1;
	};

	const choose = (i: number) => {
		list.hidePopover();
		if (i === select.selectedIndex || select.options[i]?.disabled) return;
		select.selectedIndex = i;
		render();
		select.dispatchEvent(new Event('input', { bubbles: true }));
		select.dispatchEvent(new Event('change', { bubbles: true }));
	};

	// A script's `select.value = x` fires no event, so the only way to hear it is at the setter.
	const value = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!;
	Object.defineProperty(select, 'value', {
		get: () => value.get!.call(select),
		set(v: string) {
			value.set!.call(select, v);
			render();
		},
	});
	new MutationObserver(render).observe(select, { subtree: true, childList: true, attributes: true, attributeFilter: ['disabled'] });

	const closeOnScroll = (e: Event) => {
		if (e.target !== list) list.hidePopover();
	};

	list.addEventListener('beforetoggle', (e) => {
		if ((e as ToggleEvent).newState !== 'open') return;
		const box = button.getBoundingClientRect();
		list.style.minWidth = `${box.width}px`;
		list.style.left = `${box.left}px`;
		// Below the button, or above it where the window has more room there.
		const below = innerHeight - box.bottom;
		list.style.top = below >= box.top ? `${box.bottom + 4}px` : '';
		list.style.bottom = below >= box.top ? '' : `${innerHeight - box.top + 4}px`;
		list.style.maxHeight = `${Math.min(288, Math.max(below, box.top) - 12)}px`;
	});

	list.addEventListener('toggle', (e) => {
		const open = (e as ToggleEvent).newState === 'open';
		button.setAttribute('aria-expanded', String(open));
		if (open) {
			setActive(select.selectedIndex);
			addEventListener('scroll', closeOnScroll, { capture: true, passive: true });
			addEventListener('resize', closeOnScroll);
		} else {
			setActive(-1);
			removeEventListener('scroll', closeOnScroll, { capture: true });
			removeEventListener('resize', closeOnScroll);
		}
	});

	list.addEventListener('click', (e) => {
		const li = (e.target as Element).closest<HTMLElement>('[role="option"]');
		if (li && !li.hasAttribute('aria-disabled')) choose(options().indexOf(li));
	});

	button.addEventListener('keydown', (e) => {
		if (!isOpen()) {
			if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
			e.preventDefault();
			list.showPopover();
			return;
		}
		const move: Record<string, () => number> = {
			ArrowDown: () => step(active, 1),
			ArrowUp: () => step(active, -1),
			Home: () => step(-1, 1),
			End: () => step(select.options.length, -1),
		};
		if (move[e.key]) {
			e.preventDefault();
			setActive(move[e.key]());
		} else if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			choose(active);
		} else if (e.key === 'Tab') {
			list.hidePopover();
		}
	});

	for (const l of select.labels) {
		l.addEventListener('click', (e) => {
			if (button.contains(e.target as Node) || list.contains(e.target as Node)) return;
			e.preventDefault();
			button.focus();
		});
	}

	select.classList.add('select__native');
	select.after(button, list);
	render();
}

document.querySelectorAll('select').forEach(upgrade);
