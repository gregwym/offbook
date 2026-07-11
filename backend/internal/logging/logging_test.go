package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestJSONLineWriter_ValidJSONLines proves each Write() produces exactly one
// valid JSON line with the expected msg field, trimming any trailing newline
// from the input (the stdlib logger always appends one), and always reports
// a full-length write (log.Fatal calls os.Exit right after Write returns, so
// a short-write error would only mask the real log message).
func TestJSONLineWriter_ValidJSONLines(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{name: "simple message with trailing newline", input: "hello world\n", wantMsg: "hello world"},
		{name: "message without trailing newline", input: "no newline here", wantMsg: "no newline here"},
		{name: "job alert style message", input: "[job][ALERT] offbook job failed: household-purge: boom\n", wantMsg: "[job][ALERT] offbook job failed: household-purge: boom"},
		{name: "message containing quotes and backslashes", input: `weird "quoted" \ value` + "\n", wantMsg: `weird "quoted" \ value`},
		{name: "empty message", input: "\n", wantMsg: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := jsonLineWriter{out: &buf}

			n, err := w.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Write returned error: %v", err)
			}
			if n != len(tt.input) {
				t.Errorf("Write returned n=%d, want %d", n, len(tt.input))
			}

			var decoded struct {
				Time  string `json:"time"`
				Level string `json:"level"`
				Msg   string `json:"msg"`
			}
			if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &decoded); err != nil {
				t.Fatalf("output is not valid JSON: %v (line: %q)", err, buf.String())
			}
			if decoded.Msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", decoded.Msg, tt.wantMsg)
			}
			if decoded.Level != "info" {
				t.Errorf("level = %q, want %q", decoded.Level, "info")
			}
			if decoded.Time == "" {
				t.Error("time field is empty")
			}
			if !strings.HasSuffix(buf.String(), "\n") {
				t.Error("output line does not end with a newline")
			}
		})
	}
}

// TestJSONLineWriter_MultipleWritesProduceMultipleLines proves consecutive
// Write calls (as log.Printf issues, one per call) each yield their own line
// rather than clobbering or concatenating malformed JSON.
func TestJSONLineWriter_MultipleWritesProduceMultipleLines(t *testing.T) {
	var buf bytes.Buffer
	w := jsonLineWriter{out: &buf}

	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (buf: %q)", len(lines), buf.String())
	}
	for i, want := range []string{"first", "second"} {
		var decoded struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &decoded); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
		if decoded.Msg != want {
			t.Errorf("line %d msg = %q, want %q", i, decoded.Msg, want)
		}
	}
}
