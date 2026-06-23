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

package pst_test

import (
	"errors"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"golang.org/x/text/encoding/ianaindex"
)

// TestDecodeString8UnsupportedCodePageDoesNotPanic is the regression for the
// nil-encoding crash: ianaindex.IANA.Encoding returns (nil, nil) for a code
// page whose name it recognizes but does not implement, and DecodeString8 used
// to call NewDecoder on that nil Encoding — a nil-pointer dereference that, on a
// real archive, crashed GetBodyHTML mid-message and aborted the caller's walk
// (recall: GMail 2006-2009 archive, message 2110756, code page 949). The fix
// returns ErrCodePageUnsupported instead. Every entry in the package's own code
// page map must decode-or-error, never panic.
func TestDecodeString8UnsupportedCodePageDoesNotPanic(t *testing.T) {
	pr := &pst.PropertyReader{}
	for cp, name := range pst.CodePageIdentifierToEncoding {
		// Calling with a tiny ASCII payload exercises the decoder selection,
		// which is where the nil dereference happened — before any byte decode.
		_, err := pr.DecodeString8([]byte("hello"), cp)
		_ = err // a decode error is fine; the property is a panic.
		_ = name
	}
}

// TestDecodeString8NilEncodingErrors asserts the specific code pages that
// ianaindex recognizes-but-does-not-implement now return ErrCodePageUnsupported
// (the guard) rather than panicking. utf-7 (65000) has no x/text decoder at all,
// so it degrades to this error.
func TestDecodeString8NilEncodingErrors(t *testing.T) {
	pr := &pst.PropertyReader{}
	if _, err := ianaindex.IANA.Encoding("utf-7"); err != nil {
		t.Skipf("environment maps utf-7 to a real encoding (%v); guard untestable here", err)
	}
	_, err := pr.DecodeString8([]byte("hello"), 65000) // utf-7
	if !errors.Is(err, pst.ErrCodePageUnsupported) {
		t.Fatalf("DecodeString8(cp=65000 utf-7) err = %v, want ErrCodePageUnsupported", err)
	}
}

// TestDecodeString8CJKRemapped asserts the two CJK code pages that previously
// mapped to recognized-but-unimplemented IANA names (936→gb2312, 949→
// ks_c_5601-1987, both nil) now resolve to real decoders (gbk / euc-kr) and
// decode rather than error. ASCII is a subset of both, so it round-trips.
func TestDecodeString8CJKRemapped(t *testing.T) {
	pr := &pst.PropertyReader{}
	for _, cp := range []int{936, 949} {
		got, err := pr.DecodeString8([]byte("hello"), cp)
		if err != nil {
			t.Errorf("DecodeString8(cp=%d) unexpected error: %v", cp, err)
			continue
		}
		if got != "hello" {
			t.Errorf("DecodeString8(cp=%d) = %q, want %q", cp, got, "hello")
		}
	}
}
