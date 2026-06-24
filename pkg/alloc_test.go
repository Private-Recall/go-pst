// go-pst is a library for reading Personal Storage Table (.pst) files (written in Go/Golang).
//
// Copyright 2023 Marten Mooij
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pst

import (
	"bytes"
	"testing"
)

func TestCheckAllocSize(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		n       int64
		wantErr bool
	}{
		{"negative is rejected", 1000, -1, true},
		{"within known file size", 1000, 500, false},
		{"equal to file size", 1000, 1000, false},
		{"exceeds known file size", 1000, 1001, true},
		{"unknown size within fallback cap", 0, maxReasonableAlloc, false},
		{"unknown size exceeds fallback cap", 0, maxReasonableAlloc + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &File{Size: tt.size}

			if err := file.checkAllocSize(tt.n); (err != nil) != tt.wantErr {
				t.Errorf("checkAllocSize(%d) with file size %d: err=%v, wantErr=%v", tt.n, tt.size, err, tt.wantErr)
			}
		})
	}
}

func TestReaderSize(t *testing.T) {
	data := bytes.NewReader(make([]byte, 4096))

	if got := readerSize(data); got != 4096 {
		t.Errorf("readerSize(*bytes.Reader) = %d, want 4096", got)
	}

	// Unwraps DefaultReader to reach the underlying size-bearing reader.
	if got := readerSize(NewDefaultReader(bytes.NewReader(make([]byte, 1234)))); got != 1234 {
		t.Errorf("readerSize(*DefaultReader) = %d, want 1234", got)
	}

	// A bare io.ReaderAt with no size method yields 0 (unknown).
	if got := readerSize(struct{ stubReaderAt }{}); got != 0 {
		t.Errorf("readerSize(unknown) = %d, want 0", got)
	}
}

type stubReaderAt struct{}

func (stubReaderAt) ReadAt([]byte, int64) (int, error) { return 0, nil }

func TestRTFDecodeRejectsMalformed(t *testing.T) {
	decoder := NewRTFDecoder()

	// Shorter than the 16-byte header.
	if _, err := decoder.Decode([]byte{1, 2, 3}); err == nil {
		t.Error("Decode of a sub-header stream should error")
	}

	// Compressed signature with an absurd declared uncompressed size.
	stream := make([]byte, 32)
	stream[4], stream[5], stream[6], stream[7] = 0xff, 0xff, 0xff, 0x7f // ~2 GiB
	copy(stream[8:12], CompressionTypeCompressed)

	if _, err := decoder.Decode(stream); err == nil {
		t.Error("Decode with an implausible uncompressed size should error")
	}
}
