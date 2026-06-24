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
	"os"

	"github.com/rotisserie/eris"
)

// maxReasonableAlloc bounds a single value allocation when the file size is
// unknown (the reader exposes no size). PST property values are far smaller in
// practice; this only exists so a malformed multi-gigabyte size field becomes an
// error instead of an out-of-memory kill or allocator thrash.
const maxReasonableAlloc = 256 << 20 // 256 MiB

// checkAllocSize reports whether allocating n bytes for a single value is
// plausible for this archive. A value can never be larger than the whole file,
// so File.Size (when known) is the hard bound; otherwise maxReasonableAlloc
// applies. Decode paths consult this before make([]byte, declaredSize) so a
// truncated/forged size field surfaces as an error rather than a huge
// allocation.
func (file *File) checkAllocSize(n int64) error {
	if n < 0 {
		return eris.Errorf("pst: invalid negative allocation size %d", n)
	}

	limit := int64(maxReasonableAlloc)

	if file != nil && file.Size > 0 {
		limit = file.Size
	}

	if n > limit {
		return eris.Errorf("pst: allocation of %d bytes exceeds bound %d (malformed archive)", n, limit)
	}

	return nil
}

// readerSize best-effort determines the byte length of the underlying reader,
// unwrapping DefaultReader and probing the common size-bearing interfaces
// (*bytes.Reader, *io.SectionReader, *os.File, *strings.Reader). Returns 0 when
// the size cannot be determined.
func readerSize(r any) int64 {
	for {
		switch v := r.(type) {
		case *DefaultReader:
			r = v.reader
		case interface{ Size() int64 }: // *bytes.Reader, *io.SectionReader
			return v.Size()
		case interface{ Stat() (os.FileInfo, error) }: // *os.File
			info, err := v.Stat()
			if err != nil {
				return 0
			}
			return info.Size()
		case interface{ Len() int }: // *strings.Reader and friends
			return int64(v.Len())
		default:
			return 0
		}
	}
}
