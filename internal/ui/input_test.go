package ui

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"snail_tool/internal/input"
)

func TestReadLineAcceptsCRLFAndReadableUnicode(t *testing.T) {
	view := &UI{reader: bufio.NewReader(strings.NewReader("  中文\t目录  \r\nnext\n"))}
	if got, err := view.ReadLine(); err != nil || got != "中文\t目录" {
		t.Fatalf("ReadLine() = %q, %v", got, err)
	}
	if got, err := view.ReadLine(); err != nil || got != "next" {
		t.Fatalf("following input = %q, %v", got, err)
	}
}

func TestReadLineRejectsEntireInvalidLineAndPreservesNext(t *testing.T) {
	for _, raw := range []string{"203.0.113.10\xff:8080", "bad\ufffd", "bad\x00", "bad\rvalue", "bad\x1b[31m", "bad\u200b", strings.Repeat("x", input.MaxLength+1), strings.Repeat("x", input.MaxLength*3)} {
		view := &UI{reader: bufio.NewReaderSize(strings.NewReader(raw+"\nnext\n"), 32)}
		if got, err := view.ReadLine(); got != "" || !errors.Is(err, input.ErrInvalid) {
			t.Fatalf("invalid line was accepted: len=%d got=%q err=%v", len(raw), got, err)
		}
		if got, err := view.ReadLine(); err != nil || got != "next" {
			t.Fatalf("invalid line consumed next response: %q, %v", got, err)
		}
	}
}

func TestReadLineAllowsMaximumLengthAndDoesNotActOnEOF(t *testing.T) {
	value := strings.Repeat("a", input.MaxLength)
	view := &UI{reader: bufio.NewReader(strings.NewReader(value + "\r\ny"))}
	if got, err := view.ReadLine(); err != nil || got != value {
		t.Fatalf("maximum length rejected: len=%d err=%v", len(got), err)
	}
	if got, err := view.ReadLine(); got != "" || !errors.Is(err, io.EOF) {
		t.Fatalf("unterminated response must not execute an action: %q, %v", got, err)
	}
}

func TestAskRetriesCorruptionWithoutEchoingInput(t *testing.T) {
	view := &UI{reader: bufio.NewReader(strings.NewReader("secret\xff\ngood\nnext\n"))}
	output := captureStdout(t, func() {
		if got, err := view.Ask("输入："); err != nil || got != "good" {
			t.Fatalf("Ask() = %q, %v", got, err)
		}
	})
	if strings.Contains(output, "secret") || strings.Count(output, "输入：") != 2 {
		t.Fatalf("bad input was echoed or prompt not repeated: %q", output)
	}
	if got, err := view.ReadLine(); err != nil || got != "next" {
		t.Fatalf("retry consumed next prompt's response: %q, %v", got, err)
	}
}

func TestConfirmRetriesUnknownAnswersAndPreservesNavigation(t *testing.T) {
	for _, answer := range []string{"y", "n", "q"} {
		view := &UI{reader: bufio.NewReader(strings.NewReader("maybe\n" + answer + "\nnext\n"))}
		confirmed, err := view.Confirm("确认：")
		if confirmed != (answer == "y") || (answer == "q" && !errors.Is(err, ErrReturnToMenu)) || (answer != "q" && err != nil) {
			t.Fatalf("Confirm(%q) = %v, %v", answer, confirmed, err)
		}
		if got, err := view.ReadLine(); err != nil || got != "next" {
			t.Fatalf("confirmation consumed next response: %q, %v", got, err)
		}
	}
}
