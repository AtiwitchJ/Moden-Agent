/**
 * Iterations held back so the agent can still issue its blocking transition
 * after deciding to stop. Without this reserve the agent would notice it is out
 * of budget at the exact moment it no longer has budget to act — the failure
 * mode this module exists to prevent.
 */
export const RESERVE_ITERATIONS = 3;

export class IterationBudget {
	readonly #max: number;
	#used = 0;

	constructor(max: number) {
		if (!Number.isInteger(max) || max <= 0) {
			throw new Error(`iteration budget must be a positive integer, got ${max}`);
		}
		this.#max = max;
	}

	get used(): number {
		return this.#used;
	}

	get remaining(): number {
		return Math.max(0, this.#max - this.#used);
	}

	recordIteration(): void {
		this.#used++;
	}

	/**
	 * True once the agent must stop deliberating and block the card, while it
	 * still has enough budget left to record why.
	 */
	shouldForceBlock(): boolean {
		return this.remaining <= RESERVE_ITERATIONS;
	}
}
