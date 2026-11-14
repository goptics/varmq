// Geometry of the tour's step arc: ticks along a circle's arc, picked by the pointer's angle from the centre.
/** An arc in degrees, 0 = right, 90 = up. `from` is step 1's end. */
export interface Span {
	from: number;
	to: number;
}

export const HALF: Span = { from: 180, to: 0 };
export const QUARTER: Span = { from: 180, to: 90 };

const RAD = Math.PI / 180;

/** The angle of step i of n, spread evenly over the span. */
export function tickAngle(i: number, n: number, span: Span) {
	return span.from + ((span.to - span.from) * i) / (n - 1);
}

/** Step i's offset from the centre at radius r, in SVG coords (y down). */
export function tickPoint(i: number, n: number, span: Span, r: number) {
	const a = tickAngle(i, n, span) * RAD;
	return { x: r * Math.cos(a), y: -r * Math.sin(a) };
}

/** The step nearest a pointer at (dx, dy) from the centre, in screen coords (y down), clamped to [0, n - 1]. */
export function stepAt(dx: number, dy: number, n: number, span: Span) {
	let a = Math.atan2(-dy, dx) / RAD;
	if (a < -90) a += 360; // below-left counts as past the left end, not as a negative angle
	a = Math.min(Math.max(a, Math.min(span.from, span.to)), Math.max(span.from, span.to));
	return Math.round(((span.from - a) / (span.from - span.to)) * (n - 1));
}

const round = (v: number) => +v.toFixed(3); // +toFixed turns -0 into 0

/** SVG path `d` of the span's arc at radius r, centre (0, 0), drawn from step 1's end. */
export function arcPath(span: Span, r: number) {
	const at = (deg: number) => `${round(r * Math.cos(deg * RAD))} ${round(-r * Math.sin(deg * RAD))}`;
	return `M ${at(span.from)} A ${r} ${r} 0 0 1 ${at(span.to)}`;
}
