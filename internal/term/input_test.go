package term

import (
	"bytes"
	"io"
	"testing"
)

// decodeAll reads every key the decoder can produce from r without touching a
// terminal. The test seam is the same decoder the real stdin path uses.
func decodeAll(t *testing.T, input string) []Key {
	t.Helper()
	d := newReaderDecoder(bytes.NewBufferString(input))
	var keys []Key
	for {
		k, err := d.readKey()
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return keys
		}
		if err != nil {
			t.Fatalf("decoding %q: %v", input, err)
		}
		keys = append(keys, k)
	}
}

func keyTypes(keys []Key) []KeyType {
	out := make([]KeyType, len(keys))
	for i, k := range keys {
		out[i] = k.Type
	}
	return out
}

// TestDecodeKeys is AC-055's input decoder: UTF-8 runes, control keys, CSI and
// SS3 escape sequences, SGR mouse wheel, and bracketed paste discarded.
func TestDecodeKeys(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []KeyType
	}{
		{"ascii", "jq", []KeyType{KeyRune, KeyRune}},
		{"utf8", "中é", []KeyType{KeyRune, KeyRune}},
		{"ctrl-c", "\x03", []KeyType{KeyCtrlC}},
		{"enter", "\r\n", []KeyType{KeyEnter, KeyEnter}},
		{"tab", "\t", []KeyType{KeyTab}},
		{"backspace", "\x7f\x08", []KeyType{KeyBackspace, KeyBackspace}},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", []KeyType{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"ss3 arrows", "\x1bOA\x1bOB\x1bOC\x1bOD", []KeyType{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"home-end", "\x1b[H\x1b[F\x1b[1~\x1b[4~", []KeyType{KeyHome, KeyEnd, KeyHome, KeyEnd}},
		{"pgup-pgdn-delete", "\x1b[5~\x1b[6~\x1b[3~", []KeyType{KeyPgUp, KeyPgDn, KeyDelete}},
		{"sgr wheel", "\x1b[<64;10;20M\x1b[<65;10;20M", []KeyType{KeyWheelUp, KeyWheelDown}},
		{"lone esc", "\x1b", []KeyType{KeyEsc}},
		{"paste discarded then key", "\x1b[200~y\x1b[201~r", []KeyType{KeyPaste, KeyRune}},
	}
	for _, c := range cases {
		got := keyTypes(decodeAll(t, c.input))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: key %d = %v, want %v (all %v)", c.name, i, got[i], c.want[i], got)
			}
		}
	}

	// The UTF-8 runes must survive decoding, not become replacement runes.
	keys := decodeAll(t, "中é")
	if keys[0].Rune != '中' || keys[1].Rune != 'é' {
		t.Fatalf("utf8 runes = %q, %q", keys[0].Rune, keys[1].Rune)
	}
	// A DSR reply is a key event too.
	keys = decodeAll(t, "\x1b[12;34R")
	if len(keys) != 1 || keys[0].Type != KeyDSR || keys[0].Row != 12 || keys[0].Col != 34 {
		t.Fatalf("DSR reply = %+v", keys)
	}
	// The bracketed paste content is thrown away, so no 'y' reaches the key
	// stream and cannot confirm a transaction.
	keys = decodeAll(t, "\x1b[200~y\x1b[201~")
	if len(keys) != 1 || keys[0].Type != KeyPaste {
		t.Fatalf("paste keys = %+v, want one discarded-paste event", keys)
	}
}

// TestEscapeFollowedByKeyKeepsTheByte proves a lone ESC does not swallow the
// next key: Esc then 'j' is two events.
func TestEscapeFollowedByKeyKeepsTheByte(t *testing.T) {
	keys := decodeAll(t, "\x1bj")
	if len(keys) != 2 || keys[0].Type != KeyEsc || keys[1].Type != KeyRune || keys[1].Rune != 'j' {
		t.Fatalf("esc+j = %+v", keys)
	}
}
