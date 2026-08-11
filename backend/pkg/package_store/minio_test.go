package package_store

import (
	"bytes"
	"strings"
	"testing"
)

func TestHashReaderExactRejectsShortAndOversizedReaders(t *testing.T) {
	data := []byte("skill package")
	want := hashBytes(data)

	got, err := hashReaderExact(bytes.NewReader(data), int64(len(data)))
	if err != nil || !strings.EqualFold(got, want) {
		t.Fatalf("hashReaderExact(valid) = %q, err=%v; want %q", got, err, want)
	}
	if _, err := hashReaderExact(bytes.NewReader(data[:len(data)-1]), int64(len(data))); err == nil {
		t.Fatal("hashReaderExact accepted a short reader")
	}
	if _, err := hashReaderExact(bytes.NewReader(append(append([]byte(nil), data...), '!')), int64(len(data))); err == nil {
		t.Fatal("hashReaderExact accepted an oversized reader")
	}
}
