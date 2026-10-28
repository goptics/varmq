// A job square for the ops scenes. Everything else they need comes from `../ui/dom`.

import { h } from '../ui/dom';

/** A job square, the same shape as the landing page's. */
export const job = (id: string | number, cls = '') => h('span', { class: `ops-job ${cls}`.trim(), text: String(id) });
