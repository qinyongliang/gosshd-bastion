//go:build windows

package agent

import (
	"io"

	"golang.org/x/sys/windows"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

func windowsPipeOutputReader(r io.Reader, shell string) io.Reader {
	if !isCmdShell(shell) {
		return r
	}
	enc := windowsConsoleEncoding()
	if enc == nil {
		return r
	}
	return transform.NewReader(r, enc.NewDecoder())
}

func windowsConsoleEncoding() encoding.Encoding {
	cp, _ := windows.GetConsoleOutputCP()
	if cp == 0 {
		cp = windows.GetACP()
	}
	return windowsConsoleEncodingForCodePage(cp)
}

func windowsConsoleEncodingForCodePage(cp uint32) encoding.Encoding {
	switch cp {
	case 0, 65001:
		return nil
	case 936:
		return simplifiedchinese.GBK
	case 950:
		return traditionalchinese.Big5
	case 932:
		return japanese.ShiftJIS
	case 949:
		return korean.EUCKR
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 866:
		return charmap.CodePage866
	case 1252:
		return charmap.Windows1252
	default:
		return nil
	}
}
