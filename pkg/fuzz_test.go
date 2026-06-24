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
	"os"
	"testing"
)

// FuzzNew is the independent oracle for P3 (panic-safety): for any input, the
// full read path — New, WalkFolders, the message iterator, and the per-message
// body/recipient/attachment getters — must return errors, never panic or hang.
//
// The seed corpus pairs tiny malformed inputs (bad magic, zeroed, junk) with the
// real ANSI/Unicode fixtures in ../data so the fuzzer mutates from structurally
// valid archives. Run: go test ./pkg/ -run=^$ -fuzz=FuzzNew
func FuzzNew(f *testing.F) {
	// Malformed seeds.
	f.Add([]byte{})
	f.Add([]byte("!BDN"))
	f.Add(append([]byte("!BDN"), make([]byte, 1020)...))
	f.Add(make([]byte, 1024))
	f.Add(bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 512))

	// Real fixtures as seeds (best-effort — absence must not fail the corpus).
	// Only the small ANSI/Unicode fixtures are seeded: mutating a multi-MB
	// archive makes each exec pathologically slow and starves the fuzzer.
	for _, path := range []string{"../data/32-bit.pst", "../data/support.pst"} {
		if data, err := os.ReadFile(path); err == nil {
			f.Add(data)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip oversized mutations. Allocations are bounded by the file size, so a
		// large input legitimately permits large allocations; running 20 such in
		// parallel just thrashes the allocator without exercising new structure.
		// The bug surface we care about (decode loops, size fields, dispatch) lives
		// in small inputs.
		if len(data) > 1<<20 {
			return
		}

		file, err := New(bytes.NewReader(data))

		if err != nil {
			return
		}

		defer file.Cleanup()

		// Exercise the full read surface. Every call is panic-guarded; we assert
		// only that none of them crash, so errors are intentionally ignored.
		_ = file.WalkFolders(func(folder *Folder) error {
			messageIterator, err := folder.GetMessageIterator()

			if err != nil {
				return nil
			}

			for messageIterator.Next() {
				message := messageIterator.Value()

				if message == nil {
					t.Fatal("MessageIterator.Next() returned true but Value() is nil")
				}

				_, _ = message.GetBodyHTML()
				_, _ = message.GetBodyRTF()
				_, _ = message.GetRecipients()

				attachmentIterator, err := message.GetAttachmentIterator()

				if err != nil {
					continue
				}

				for attachmentIterator.Next() {
					_, _ = attachmentIterator.Value().WriteTo(bytes.NewBuffer(nil))
				}
			}

			return nil
		})
	})
}
