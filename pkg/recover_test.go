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
	"errors"
	"testing"
)

func TestGuardRecoversPanic(t *testing.T) {
	result, err := guard("boundary", func() (int, error) {
		panic("boom")
	})

	if err == nil {
		t.Fatal("expected an error from a recovered panic, got nil")
	}
	if result != 0 {
		t.Errorf("expected zero value on panic, got %d", result)
	}
}

func TestGuardRecoversOutOfRangePanic(t *testing.T) {
	_, err := guard("boundary", func() (byte, error) {
		var b []byte
		return b[5], nil // index out of range
	})

	if err == nil {
		t.Fatal("expected an error from a recovered out-of-range panic, got nil")
	}
}

func TestGuardVoidRecoversPanic(t *testing.T) {
	err := guardVoid("boundary", func() error {
		panic("boom")
	})

	if err == nil {
		t.Fatal("expected an error from a recovered panic, got nil")
	}
}

func TestGuardPassesThroughResult(t *testing.T) {
	sentinel := errors.New("sentinel")

	result, err := guard("boundary", func() (int, error) {
		return 42, sentinel
	})

	if result != 42 {
		t.Errorf("guard altered the result: got %d, want 42", result)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("guard altered the error: got %v, want %v", err, sentinel)
	}
}

// TestNewMalformedInputReturnsErrorNoPanic asserts the New boundary never
// crashes on garbage input — the core P3 guarantee.
func TestNewMalformedInputReturnsErrorNoPanic(t *testing.T) {
	seeds := map[string][]byte{
		"empty":        {},
		"short":        {0x21, 0x42, 0x44, 0x4e}, // "!BDN" magic only
		"zeroed-1k":    make([]byte, 1024),
		"junk":         bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 512),
		"bad-magic-1k": append([]byte("!BDNxxxx"), make([]byte, 1016)...),
	}

	for name, seed := range seeds {
		t.Run(name, func(t *testing.T) {
			// Must not panic; must return an error rather than a *File.
			file, err := New(bytes.NewReader(seed))

			if err == nil {
				t.Errorf("New(%s) returned nil error for malformed input (file=%v)", name, file)
			}
		})
	}
}

// TestMessageIteratorSkipsEmptyRows locks the P5 contract that a malformed/empty
// row is skipped rather than surfaced as a nil Value.
func TestMessageIteratorSkipsEmptyRows(t *testing.T) {
	iterator := MessageIterator{
		messageTableContext: TableContext{
			Properties: [][]Property{{}, {}}, // two empty rows, no message identifiers
		},
	}

	if iterator.Next() {
		t.Fatalf("Next() returned true for empty rows; Value() = %v", iterator.Value())
	}
	if err := iterator.Err(); err != nil {
		t.Errorf("Err() = %v, want nil after cleanly exhausting empty rows", err)
	}
	if iterator.Value() != nil {
		t.Errorf("Value() = %v, want nil", iterator.Value())
	}
}
