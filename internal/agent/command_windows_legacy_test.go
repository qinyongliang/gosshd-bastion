//go:build windows

package agent

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

const testLegacyMarker = "__GOSSHD_0123456789abcdef0123456789abcdef__"

var testLegacyEnd = legacyWindowsPromptEnd(testLegacyMarker)

func TestLegacyWindowsPromptDecoderAcrossEveryByteBoundary(t *testing.T) {
	cwd := `C:\Users\user\项目; with spaces\`
	record := testLegacyMarker + "7;12:34:56.78;" + cwd + testLegacyEnd
	decorated := record[:13] + "\x1b[0K\r\n" + record[13:61] + "\x1b[0K\r\n" + record[61:]
	want := "before" + "\x1b[0K\r\n\x1b[0K\r\n" + "\x1b]633;D;7\a\x1b]633;A\a\x1b]633;P;Cwd=" + cwd + "\aafter"
	for split := 0; split <= len(decorated); split++ {
		decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
		got := decoder.decode("before"+decorated[:split], false) + decoder.decode(decorated[split:]+"after", true)
		if got != want {
			t.Fatalf("split %d: got %q, want %q", split, got, want)
		}
	}
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	var out strings.Builder
	for i := 0; i < len(decorated); i++ {
		out.WriteString(decoder.decode(decorated[i:i+1], false))
	}
	out.WriteString(decoder.decode("", true))
	if out.String() != strings.TrimSuffix(strings.TrimPrefix(want, "before"), "after") {
		t.Fatalf("one byte at a time: %q", out.String())
	}
}

func TestLegacyWindowsPromptDecoderPreservesOrdinaryOutput(t *testing.T) {
	for _, input := range []string{
		"hello\r\n\x1b[31mred\x1b[0m?]633;D;0?\\\r\n",
		testLegacyMarker + "0;stamp;$P" + testLegacyEnd,
		testLegacyMarker + "not-an-exit;stamp;C:\\work" + testLegacyEnd,
		testLegacyMarker + "0;;C:\\work" + testLegacyEnd,
		testLegacyMarker + "0;stamp;C:\\work\x00" + testLegacyEnd,
		testLegacyMarker + "0;stamp;relative\\path" + testLegacyEnd,
		"__GOSSHD_different_nonce__0;stamp;C:\\work__GOSSHD_different_nonce__",
		"ordinary _ __GOSSHD_0123",
		testLegacyMarker + "0;stamp;C:\\unfinished",
	} {
		for split := 0; split <= len(input); split++ {
			decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
			got := decoder.decode(input[:split], false) + decoder.decode(input[split:], true)
			if got != input {
				t.Fatalf("input %q split %d changed to %q", input, split, got)
			}
		}
	}
}

func TestLegacyWindowsPromptDecoderBoundsPendingRecords(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	input := testLegacyMarker + strings.Repeat("x", legacyWindowsPromptLimit)
	if got := decoder.decode(input, false); got != input || decoder.pending != "" {
		t.Fatalf("oversized record was buffered or discarded")
	}
	input = testLegacyMarker[:8] + strings.Repeat("\x1b[0K", legacyWindowsPromptLimit)
	if got := decoder.decode(input, false); got != input || decoder.pending != "" {
		t.Fatalf("oversized partial marker was buffered or discarded")
	}
}

func TestLegacyWindowsPromptDecoderSuppressesRedrawCompletion(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	record := testLegacyMarker + "0;12:34:56.78;C:\\work" + testLegacyEnd
	if got := decoder.decode(record, false); !strings.Contains(got, "\x1b]633;D;0\a") {
		t.Fatalf("first prompt missing: %q", got)
	}
	if got := decoder.decode("\x1b[H"+record, false); strings.Contains(got, "633;D") {
		t.Fatalf("redraw completed a command: %q", got)
	}
	if got := decoder.decode(strings.Replace(record, "56.78", "57.90", 1), true); !strings.Contains(got, "\x1b]633;D;0\a") {
		t.Fatalf("new prompt missing: %q", got)
	}
}

func TestLegacyWindowsPromptDecoderNonFilesystemPrompt(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	got := decoder.decode(testLegacyMarker+"1;2;"+testLegacyEnd, true)
	if want := "\x1b]633;D;1\a\x1b]633;A\a"; got != want {
		t.Fatalf("non-filesystem prompt %q, want %q", got, want)
	}
}

func TestLegacyWindowsOutputFramingAndEOF(t *testing.T) {
	input := "before" + testLegacyMarker + "0;stamp;C:\\work" + testLegacyEnd + "after" + testLegacyMarker[:10]
	var frames bytes.Buffer
	copyLegacyWindowsReaderToFrame(&frames, protocol.FrameStdout, strings.NewReader(input), testLegacyMarker)
	reader := bufio.NewReader(&frames)
	var output strings.Builder
	for {
		frame, err := protocol.ReadFrame(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type != protocol.FrameStdout {
			t.Fatalf("frame type: %d", frame.Type)
		}
		output.Write(frame.Data)
	}
	if want := "before\x1b]633;D;0\a\x1b]633;A\a\x1b]633;P;Cwd=C:\\work\aafter" + testLegacyMarker[:10]; output.String() != want {
		t.Fatalf("framing: %q, want %q", output.String(), want)
	}
}

func TestLegacyWindowsShellArgsDoNotEmitConsoleOSC(t *testing.T) {
	for _, shell := range []string{"cmd.exe", `C:\Windows\system32\cmd.exe`, "powershell.exe", "pwsh.exe"} {
		args := windowsLegacyInteractiveShellArgs(shell, testLegacyMarker)
		command := windowsCommandLineWithArgs(shell, args)
		if !strings.Contains(command, testLegacyMarker) || strings.ContainsAny(command, "\x1b\a") || strings.Contains(command, "$E]633") || strings.Contains(command, " /Q ") {
			t.Fatalf("unsafe legacy command: %q", command)
		}
	}
	if got := windowsLegacyInteractiveShellArgs("custom.exe", testLegacyMarker); got != nil {
		t.Fatalf("custom shell args: %v", got)
	}
	if got, want := windowsInteractiveShellArgs("cmd.exe"), []string{"/D", "/K", windowsCmdIntegrationCommand()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConPTY integration changed: %v", got)
	}
}

func TestLegacyWindowsPromptDecoderFastCommandsAndHistoricalRedraw(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	record := func(stamp string) string { return testLegacyMarker + "0;" + stamp + `;C:\work` + testLegacyEnd }
	first, second := record("12:34:56.78"), record("12:34:56.79")
	for _, prompt := range []string{first, first, second} {
		if got := decoder.decode(prompt, false); !strings.Contains(got, "\x1b]633;D;0\a") {
			t.Fatalf("new prompt suppressed: %q", got)
		}
	}
	// Fragment the CSI cursor-up signal as well as the historical records.
	var replay strings.Builder
	raw := "\x1b[12A" + first + "\r\n" + second + "\x1b[?25h"
	for i := 0; i < len(raw); i++ {
		replay.WriteString(decoder.decode(raw[i:i+1], false))
	}
	if strings.Contains(replay.String(), "633;") {
		t.Fatalf("historical redraw completed a command: %q", replay.String())
	}
	if got := decoder.decode(second, true); !strings.Contains(got, "\x1b]633;D;0\a") {
		t.Fatalf("fast prompt after redraw suppressed: %q", got)
	}
}

func TestLegacyWindowsPromptDecoderBoundsRedrawHistory(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	for n := 0; n < 300; n++ {
		decoder.decode(testLegacyMarker+"0;"+strconv.Itoa(n)+`;C:\work`+testLegacyEnd, false)
	}
	if len(decoder.promptOrder) != legacyWindowsPromptHistory || len(decoder.seenPrompts) != legacyWindowsPromptHistory {
		t.Fatalf("history is unbounded: %d/%d", len(decoder.promptOrder), len(decoder.seenPrompts))
	}
	decoder.decode(testLegacyMarker+"0;299;"+`C:\work`+testLegacyEnd, false)
	if len(decoder.promptOrder) != legacyWindowsPromptHistory || len(decoder.seenPrompts) != legacyWindowsPromptHistory {
		t.Fatal("duplicate prompt evicted history unnecessarily")
	}
}

func TestLegacyWindowsPromptDecoderRecoversAfterOversizedRecord(t *testing.T) {
	decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
	oversized := testLegacyMarker + strings.Repeat("x", legacyWindowsPromptLimit)
	if got := decoder.decode(oversized, false); got != oversized {
		t.Fatal("oversized ordinary text was discarded")
	}
	prompt := testLegacyMarker + `0;next;C:\work` + testLegacyEnd
	if got := decoder.decode(prompt, true); !strings.Contains(got, "\x1b]633;D;0\a") {
		t.Fatalf("decoder did not recover: %q", got)
	}
}

func TestLegacyWindowsPromptDecoderRecoversFromMalformedPrefix(t *testing.T) {
	ordinary := testLegacyMarker + "incomplete ordinary text"
	record := testLegacyMarker + `0;next;C:\work` + testLegacyEnd
	input := ordinary + record
	want := ordinary + "\x1b]633;D;0\a\x1b]633;A\a\x1b]633;P;Cwd=C:\\work\a"
	for split := 0; split <= len(input); split++ {
		decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
		got := decoder.decode(input[:split], false) + decoder.decode(input[split:], true)
		if got != want {
			t.Fatalf("split %d lost the real prompt: %q", split, got)
		}
	}
}

func TestLegacyWindowsPromptDecoderCroppedRedrawEnd(t *testing.T) {
	wrapped := testLegacyEnd[:18] + "\x1b[0K\r\n" + testLegacyEnd[18:]
	tail := "cropped old directory" + wrapped + "\r\n"
	record := testLegacyMarker + `0;new;C:\work` + testLegacyEnd
	input := tail + record
	want := "cropped old directory\x1b[0K\r\n\r\n\x1b]633;D;0\a\x1b]633;A\a\x1b]633;P;Cwd=C:\\work\a"
	for split := 0; split <= len(input); split++ {
		decoder := legacyWindowsPromptDecoder{marker: testLegacyMarker}
		got := decoder.decode(input[:split], false) + decoder.decode(input[split:], true)
		if got != want {
			t.Fatalf("split %d leaked orphaned marker or lost real completion: %q", split, got)
		}
	}
}
