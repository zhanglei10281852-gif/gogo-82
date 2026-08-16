// Package strictjson is the only JSON entry point in MineGuard.
//
// Decoding is strict by construction: unknown members are an error, a second
// top-level value is an error, and an empty document is an error. A monitoring
// configuration that silently ignores a renamed threshold is worse than one that
// refuses to load, so nothing here falls back to a default on a typo.
package strictjson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// MaxLineBytes bounds a single JSON Lines record.
const MaxLineBytes = 4 << 20

// Decode strictly decodes a single JSON document from data into target.
func Decode(data []byte, target any) error {
	trimmed := bytes.TrimSpace(stripBOM(data))
	if len(trimmed) == 0 {
		return errors.New("document is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("document carries a second top-level value")
		}
		return fmt.Errorf("trailing content: %w", err)
	}
	return nil
}

// DecodeFile reads and strictly decodes one JSON document.
func DecodeFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := Decode(data, target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// DecodeLines strictly decodes a JSON Lines document, calling consume for every
// record with its one-based line number. Blank lines are skipped so an editor
// that leaves a trailing newline does not break a ledger.
func DecodeLines(path string, consume func(line int, data []byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(stripBOM(scanner.Bytes()))
		if len(raw) == 0 {
			continue
		}
		payload := append([]byte(nil), raw...)
		if err := consume(line, payload); err != nil {
			return fmt.Errorf("%s line %d: %w", path, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// DecodeInto strictly decodes one JSON Lines record.
func DecodeInto(data []byte, target any) error {
	return Decode(data, target)
}

// Encode renders value as indented JSON with a trailing newline and without HTML
// escaping, which keeps stored documents byte-comparable between runs.
func Encode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// EncodeCompact renders value as a single-line JSON record with a trailing
// newline, which is the shape every ledger line takes.
func EncodeCompact(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Number converts a decoded json.Number into a float64.
func Number(raw json.Number) (float64, error) {
	value, err := raw.Float64()
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", raw.String())
	}
	return value, nil
}

// stripBOM removes a leading UTF-8 byte order mark.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}

// Describe renders a decoding error with the document kind for report output.
func Describe(kind string, err error) string {
	if err == nil {
		return kind + ": ok"
	}
	return kind + ": " + strings.TrimSpace(err.Error())
}
