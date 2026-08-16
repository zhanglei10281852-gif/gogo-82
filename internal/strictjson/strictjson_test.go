package strictjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sample struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestDecodeAcceptsAWellFormedDocument(t *testing.T) {
	var target sample
	if err := Decode([]byte("{\"name\":\"face\",\"value\":1.5}"), &target); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if target.Name != "face" || target.Value != 1.5 {
		t.Fatalf("decoded %+v", target)
	}
}

func TestDecodeRefusesAnEmptyDocument(t *testing.T) {
	var target sample
	for _, body := range []string{"", "   ", "\n\t "} {
		if err := Decode([]byte(body), &target); err == nil {
			t.Errorf("an empty document (%q) must be refused", body)
		}
	}
}

func TestDecodeRefusesAnUnknownMember(t *testing.T) {
	var target sample
	err := Decode([]byte("{\"name\":\"face\",\"valu\":1.5}"), &target)
	if err == nil {
		t.Fatal("a misspelled member must be refused rather than ignored")
	}
	if !strings.Contains(err.Error(), "valu") {
		t.Errorf("the error must name the member, got %v", err)
	}
}

func TestDecodeStripsAByteOrderMark(t *testing.T) {
	var target sample
	if err := Decode([]byte("\xef\xbb\xbf{\"name\":\"face\"}"), &target); err != nil {
		t.Fatalf("a document with a byte order mark must decode: %v", err)
	}
	if target.Name != "face" {
		t.Fatalf("decoded %+v", target)
	}
}

func TestDecodeFileReportsThePath(t *testing.T) {
	path := write(t, "{\"name\":\"face\",\"nope\":1}\n")
	var target sample
	err := DecodeFile(path, &target)
	if err == nil {
		t.Fatal("an invalid document must be refused")
	}
	if !strings.Contains(err.Error(), "document.json") {
		t.Errorf("the error must name the file, got %v", err)
	}
	if err := DecodeFile(filepath.Join(t.TempDir(), "absent.json"), &target); err == nil {
		t.Error("a missing file must be refused")
	}
}

func TestDecodeLinesSkipsBlankLinesAndNumbersTheRest(t *testing.T) {
	path := write(t, "{\"name\":\"a\"}\n\n{\"name\":\"b\"}\n   \n{\"name\":\"c\"}\n")
	seen := []string{}
	lines := []int{}
	err := DecodeLines(path, func(line int, data []byte) error {
		var item sample
		if err := DecodeInto(data, &item); err != nil {
			return err
		}
		seen = append(seen, item.Name)
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatalf("DecodeLines: %v", err)
	}
	if strings.Join(seen, ",") != "a,b,c" {
		t.Fatalf("records = %v", seen)
	}
	if lines[0] != 1 || lines[1] != 3 || lines[2] != 5 {
		t.Fatalf("line numbers = %v, want the physical lines", lines)
	}
}

func TestDecodeLinesReportsTheFailingLine(t *testing.T) {
	path := write(t, "{\"name\":\"a\"}\n{\"nope\":1}\n")
	err := DecodeLines(path, func(line int, data []byte) error {
		var item sample
		return DecodeInto(data, &item)
	})
	if err == nil {
		t.Fatal("a bad record must be refused")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error must name the line, got %v", err)
	}
}

func TestDecodeLinesRefusesAMissingFile(t *testing.T) {
	err := DecodeLines(filepath.Join(t.TempDir(), "absent.jsonl"), func(int, []byte) error { return nil })
	if err == nil {
		t.Fatal("a missing ledger must be refused")
	}
}

func TestEncodeIsIndentedAndDoesNotEscapeHTML(t *testing.T) {
	data, err := Encode(map[string]string{"label": "a<b>c&d"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "a<b>c&d") {
		t.Fatalf("encoded %q, want the literal characters", text)
	}
	if !strings.Contains(text, "\n  ") {
		t.Fatalf("encoded %q, want indentation", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("an encoded document must end with a newline")
	}
}

func TestEncodeCompactIsOneLine(t *testing.T) {
	data, err := EncodeCompact(sample{Name: "face", Value: 1.5})
	if err != nil {
		t.Fatalf("EncodeCompact: %v", err)
	}
	text := string(data)
	if strings.Count(text, "\n") != 1 || !strings.HasSuffix(text, "\n") {
		t.Fatalf("encoded %q, want a single line record", text)
	}
}

func TestEncodeIsStableBetweenCalls(t *testing.T) {
	value := sample{Name: "face", Value: 1.5}
	first, err := Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	second, err := Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("two encodings of the same value must be byte identical")
	}
}

func TestNumberConversion(t *testing.T) {
	value, err := Number(json.Number("1.25"))
	if err != nil || value != 1.25 {
		t.Fatalf("Number = %v, %v", value, err)
	}
	if _, err := Number(json.Number("not a number")); err == nil {
		t.Error("a malformed number must be refused")
	}
}

func TestDescribeRendersTheDocumentKind(t *testing.T) {
	if got := Describe("layout", nil); got != "layout: ok" {
		t.Errorf("Describe = %q", got)
	}
	if got := Describe("layout", os.ErrNotExist); !strings.HasPrefix(got, "layout: ") {
		t.Errorf("Describe = %q", got)
	}
}
