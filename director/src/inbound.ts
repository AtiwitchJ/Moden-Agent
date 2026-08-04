/** Sentinel the tmux runtime appends to the last chunk of every outbound
 * message so the receiver can split whole messages instead of inferring
 * end-of-message from chunk timing. Must stay byte-identical to
 * MessageEndSentinel in backend/internal/adapters/runtime/tmux/commands.go. */
export const MESSAGE_END_SENTINEL = "\n.AO_MSG_END.\n";

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

/** Pulls zero or more whole messages out of an inbound buffer. Returns the
 * messages in arrival order and the trailing remainder that did not yet have
 * a sentinel — the caller should append more chunks to it before calling
 * again.
 *
 * Splits on the sentinel rather than on a quiet window: a 300ms timer
 * truncates messages whose tmux chunks arrive >300ms apart (which is every
 * large handoff), so the model sees a partial report and cannot act on it. */
export function extractMessages(buffer: string): { messages: string[]; remainder: string } {
	const messages: string[] = [];
	let start = 0;
	while (true) {
		const idx = buffer.indexOf(MESSAGE_END_SENTINEL, start);
		if (idx === -1) break;
		messages.push(buffer.slice(start, idx));
		start = idx + MESSAGE_END_SENTINEL.length;
	}
	return { messages, remainder: buffer.slice(start) };
}
