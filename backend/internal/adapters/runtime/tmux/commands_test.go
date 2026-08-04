package tmux

import "testing"

func TestMessageEndSentinel_IsExportedAndNonEmpty(t *testing.T) {
	// Director's stdin reader splits whole messages on this sentinel. The
	// tmux runtime appends it before the Enter keystroke so a multi-chunk
	// message arrives as one logical turn, not several.
	if MessageEndSentinel == "" {
		t.Fatal("MessageEndSentinel must be a non-empty constant the Director can split on")
	}
}

func TestWrapMessageWithSentinel(t *testing.T) {
	// wrapMessageWithSentinel is used by the tmux runtime to mark the end
	// of an outbound message.
	got := wrapMessageWithSentinel("hello")
	want := "hello" + MessageEndSentinel
	if got != want {
		t.Fatalf("wrapMessageWithSentinel = %q, want %q", got, want)
	}
}
