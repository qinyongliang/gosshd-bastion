//go:build windows

package agent

import (
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qinyongliang/gosshd-bastion/internal/protocol"
)

// WinPTY scrapes console cells rather than preserving application OSC bytes.
// A session-specific printable record survives that scrape; only the Agent
// turns it into OSC, so command output never needs a global "?]633" rewrite.
type legacyWindowsPromptDecoder struct {
	marker        string
	pending       string
	seenPrompts   map[[32]byte]struct{}
	promptOrder   [][32]byte
	redrawing     bool
	controlBuffer string
}

const (
	legacyWindowsPromptLimit   = 64 * 1024
	legacyWindowsPromptHistory = 256
)

func legacyWindowsPromptEnd(marker string) string {
	return strings.Replace(marker, "__GOSSHD_", "__GOSSHD_END_", 1)
}

func windowsLegacyInteractiveShellArgs(shell, marker string) []string {
	endMarker := legacyWindowsPromptEnd(marker)
	if isCmdShell(shell) {
		// $T distinguishes a new prompt from a screen redraw. cmd's OSC integration
		// also reports exit 0: PROMPT cannot expand the previous command's errorlevel.
		return []string{"/D", "/K", "prompt " + marker + "0;$T;$P" + endMarker + "$_$P$G"}
	}
	if isPowerShell(shell) {
		script := `[Console]::InputEncoding=[Text.UTF8Encoding]::new($false); [Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $script:__gosshdPromptSequence=0; function global:prompt { $code = if ($global:?) { 0 } else { 1 }; $script:__gosshdPromptSequence++; $location = $executionContext.SessionState.Path.CurrentLocation; $cwd = ''; if ($location.Provider.Name -eq 'FileSystem') { $cwd = $location.ProviderPath }; '` + marker + `' + $code + ';' + $script:__gosshdPromptSequence + ';' + $cwd + '` + endMarker + `' + "` + "`r`n" + `PS $location> " }`
		return []string{"-NoLogo", "-NoProfile", "-NoExit", "-Command", script}
	}
	return nil
}

// normalizedLegacyPrompt retains raw offsets, allowing ordinary terminal data
// to pass through byte-for-byte. WinPTY may insert CSI and wrapped newlines
// anywhere in a printable record, including inside its marker.
func normalizedLegacyPrompt(raw string) (string, []int) {
	var text strings.Builder
	positions := make([]int, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == '\r' || raw[i] == '\n' {
			i++
			continue
		}
		if raw[i] == 0x1b && i+1 == len(raw) {
			break
		}
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			end := i + 2
			for end < len(raw) && !(raw[end] >= 0x40 && raw[end] <= 0x7e) {
				end++
			}
			if end == len(raw) {
				break
			}
			i = end + 1
			continue
		}
		positions = append(positions, i)
		text.WriteByte(raw[i])
		i++
	}
	return text.String(), positions
}

// Retain wrapped line breaks and terminal controls so cursor-up redraw offsets
// still refer to the same rows after the printable metadata is removed.
func legacyPromptLayout(raw string) string {
	var controls strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\r' || raw[i] == '\n' {
			controls.WriteByte(raw[i])
			continue
		}
		if i+1 == len(raw) {
			break
		}
		if raw[i] != 0x1b || raw[i+1] != '[' {
			continue
		}
		end := i + 2
		for end < len(raw) && !(raw[end] >= 0x40 && raw[end] <= 0x7e) {
			end++
		}
		if end < len(raw) {
			controls.WriteString(raw[i : end+1])
			i = end
		}
	}
	return controls.String()
}

// WinPTY can replay several historical prompt lines after resizing. Suppress
// their events only during cursor-up redraws, not normal output: cmd's $T can
// legitimately have the same value for two fast consecutive commands.
func (d *legacyWindowsPromptDecoder) writeRaw(out *strings.Builder, raw string) {
	out.WriteString(raw)
	input := d.controlBuffer + raw
	d.controlBuffer = ""
	for i := 0; i < len(input); i++ {
		if input[i] != 0x1b {
			continue
		}
		if i+1 == len(input) {
			d.controlBuffer = input[i:]
			break
		}
		if input[i+1] != '[' {
			continue
		}
		end := i + 2
		for end < len(input) && !(input[end] >= 0x40 && input[end] <= 0x7e) {
			end++
		}
		if end == len(input) {
			if len(input)-i < 64 {
				d.controlBuffer = input[i:]
			}
			break
		}
		switch input[end] {
		case 'A', 'H', 'f', 'J':
			d.redrawing = true
		case 'h':
			if input[i:end+1] == "\x1b[?25h" {
				d.redrawing = false
			}
		}
		i = end
	}
}

func (d *legacyWindowsPromptDecoder) decode(data string, eof bool) string {
	d.pending += data
	endMarker := legacyWindowsPromptEnd(d.marker)
	var out strings.Builder
	for d.pending != "" {
		text, positions := normalizedLegacyPrompt(d.pending)
		start := strings.Index(text, d.marker)
		orphanEnd := strings.Index(text, endMarker)
		if orphanEnd >= 0 && (start < 0 || orphanEnd < start) {
			// A resize can crop the beginning of an old record. An unmatched
			// end token carries no event and must not become ordinary output.
			rawStart := positions[orphanEnd]
			rawEnd := positions[orphanEnd+len(endMarker)-1] + 1
			d.writeRaw(&out, d.pending[:rawStart])
			d.writeRaw(&out, legacyPromptLayout(d.pending[rawStart:rawEnd]))
			d.pending = d.pending[rawEnd:]
			continue
		}
		if start < 0 {
			keep := 0
			if !eof {
				for _, token := range []string{d.marker, endMarker} {
					for n := min(len(text), len(token)-1); n > keep; n-- {
						if strings.HasSuffix(text, token[:n]) {
							keep = n
							break
						}
					}
				}
			}
			if len(d.pending) > legacyWindowsPromptLimit {
				keep = 0
			}
			if keep > 0 {
				rawStart := positions[len(text)-keep]
				d.writeRaw(&out, d.pending[:rawStart])
				d.pending = d.pending[rawStart:]
			} else {
				d.writeRaw(&out, d.pending)
				d.pending = ""
			}
			break
		}
		rawStart := positions[start]
		d.writeRaw(&out, d.pending[:rawStart])
		end := strings.Index(text[start+len(d.marker):], endMarker)
		nextStart := strings.Index(text[start+len(d.marker):], d.marker)
		if nextStart >= 0 && (end < 0 || nextStart < end) {
			// Recover if a cropped/incomplete record precedes a new prompt.
			rawNext := positions[start+len(d.marker)+nextStart]
			d.writeRaw(&out, d.pending[rawStart:rawNext])
			d.pending = d.pending[rawNext:]
			continue
		}
		if end < 0 {
			d.pending = d.pending[rawStart:]
			if eof || len(d.pending) > legacyWindowsPromptLimit {
				d.writeRaw(&out, d.pending)
				d.pending = ""
			}
			break
		}
		end += start + len(d.marker)
		rawEnd := positions[end+len(endMarker)-1] + 1
		record := d.pending[rawStart:rawEnd]
		fields := strings.SplitN(text[start+len(d.marker):end], ";", 3)
		valid := len(fields) == 3 && len(record) <= legacyWindowsPromptLimit
		code := 0
		if valid {
			var err error
			code, err = strconv.Atoi(fields[0])
			valid = err == nil && fields[1] != "" && !strings.ContainsAny(fields[1]+fields[2], "\x00\x1b\a") && (fields[2] == "" || filepath.IsAbs(fields[2]))
		}
		if !valid {
			d.writeRaw(&out, record)
		} else {
			d.writeRaw(&out, legacyPromptLayout(record))
			key := sha256.Sum256([]byte(strings.Join(fields, ";")))
			if _, seen := d.seenPrompts[key]; !seen || !d.redrawing {
				if d.seenPrompts == nil {
					d.seenPrompts = make(map[[32]byte]struct{})
				}
				if !seen {
					if len(d.promptOrder) == legacyWindowsPromptHistory {
						delete(d.seenPrompts, d.promptOrder[0])
						d.promptOrder = d.promptOrder[1:]
					}
					d.seenPrompts[key] = struct{}{}
					d.promptOrder = append(d.promptOrder, key)
				}
				fmt.Fprintf(&out, "\x1b]633;D;%d\a\x1b]633;A\a", code)
				if fields[2] != "" {
					fmt.Fprintf(&out, "\x1b]633;P;Cwd=%s\a", fields[2])
				}
			}
		}
		d.pending = d.pending[rawEnd:]
	}
	return out.String()
}

func copyLegacyWindowsReaderToFrame(w io.Writer, typ byte, r io.Reader, marker string) {
	decoder := legacyWindowsPromptDecoder{marker: marker}
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		text := decoder.decode(string(buf[:n]), err != nil)
		if text != "" {
			if writeErr := protocol.WriteFrame(w, protocol.Frame{Type: typ, Data: []byte(text)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
