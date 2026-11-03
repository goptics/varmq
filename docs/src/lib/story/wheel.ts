// One wheel turn moves the story one step. A turn moves a step once it has travelled THRESHOLD px; a pause of QUIET ms
// ends a turn, so each notch of a wheel turned notch by notch is a step. A wheel that keeps spinning, as a
// free-spinning mouse wheel does, moves on a step every COOLDOWN ms, about as long as the page takes to scroll one, for
// as long as it keeps half its speed; a swipe's momentum, which dies away, does not.
const COOLDOWN = 650;
const THRESHOLD = 40;
const QUIET = 200;
/** The wheel's speed is about the distance of its last TAU ms, per ms. */
const TAU = 100;

/** Returns a function that takes each wheel event, its time in ms and its delta in px, and says whether it moves a step. */
export function wheelSteps() {
	let stepped = -Infinity; // when the wheel last moved a step
	let wheeled = -Infinity; // its last event
	let travel = 0; // how far it has turned since the last step
	let speed = 0; // how fast it turns, in px per ms
	let peak = 0; // the fastest it has turned since it last moved a step
	return (time: number, delta: number) => {
		const fresh = time - wheeled > QUIET;
		// Speed is distance over time, not per event. After a long frame, as when a step starts, Chrome sends the notches
		// it held back as one event: counted per event, that reads as a burst, and the steady spin after it as a wheel
		// slowing down, which stopped a free-spinning wheel's steps.
		speed = speed * Math.exp((wheeled - time) / TAU) + Math.abs(delta) / TAU;
		wheeled = time;
		if (fresh || Math.sign(delta) !== Math.sign(travel)) travel = 0;
		peak = fresh ? speed : Math.max(peak, speed);
		if (!fresh && (time - stepped < COOLDOWN || speed < peak / 2)) return false;
		travel += delta;
		if (Math.abs(travel) < THRESHOLD) return false;
		travel = 0;
		peak = speed;
		stepped = time;
		return true;
	};
}
