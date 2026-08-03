/** Frames a message that arrived on the Director's terminal control channel.
 *
 * Deliberately does not pre-classify it. Workers send two kinds of message on
 * this channel — a question that blocks them, and the phase handoff report they
 * are required to send when a phase completes — and the earlier framing called
 * every one of them a question. A completed phase then never became a
 * transition_card call, so the card never moved. */
export function inboundInstruction(message: string): string {
	return `A worker session sent this message on your terminal channel:

${message.trim()}

Decide what it is before you act:

- A question or a blocker: resolve it, then reply with the answer_worker tool.
- A phase handoff report (a completed phase, its changed files, checks and their results, commit or PR, and the next phase's focus): verify it against the live card with show_card, then either start the next phase's worker or advance the card with transition_card. Do not reply with answer_worker to a report that asks you nothing.
- Neither: read the card and continue driving it.`;
}
