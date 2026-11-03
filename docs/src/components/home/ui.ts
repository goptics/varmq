// The home page sections' tabs (index.mdx, under the story). Each tab change is also sent as a
// `tabchange` event from the tab, which the storage demo (store.ts) listens for. The intro's install tabs are bound by
// Story.astro, so only the tab lists in these sections are bound here.

import { tabs } from '../ui/dom';

document.querySelectorAll<HTMLElement>('.home-jobs [role="tablist"]').forEach((list) => {
	tabs(list, (key, tab) => tab.dispatchEvent(new CustomEvent('tabchange', { bubbles: true, detail: { key } })));
});
