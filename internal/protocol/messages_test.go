package protocol

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

// TestReadJSONRejectsOversizedLine verifies that a message larger than
// MaxMessageBytes is rejected instead of being buffered without bound.
func TestReadJSONRejectsOversizedLine(t *testing.T) {
	// A line of MaxMessageBytes+1 'a' characters with no newline.
	huge := strings.Repeat("a", MaxMessageBytes+1)
	r := bufio.NewReader(strings.NewReader(huge))

	var v map[string]any
	err := ReadJSON(r, &v)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("expected ErrMessageTooLarge, got %v", err)
	}
}

// TestReadJSONAcceptsNormalMessage verifies a legitimate message still decodes.
func TestReadJSONAcceptsNormalMessage(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"type":"chat","text":"hola"}` + "\n"))
	var cmd Command
	if err := ReadJSON(r, &cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type != CmdChat || cmd.Text != "hola" {
		t.Fatalf("decoded %+v, want chat/hola", cmd)
	}
}

// TestReadJSONAcceptsLineLongerThanBuffer verifies a valid message larger than
// bufio's internal buffer (4 KiB) but under the cap is still read whole.
func TestReadJSONAcceptsLineLongerThanBuffer(t *testing.T) {
	long := `{"type":"chat","text":"` + strings.Repeat("x", 8000) + `"}` + "\n"
	r := bufio.NewReader(strings.NewReader(long))
	var cmd Command
	if err := ReadJSON(r, &cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmd.Text) != 8000 {
		t.Fatalf("text length = %d, want 8000", len(cmd.Text))
	}
}
