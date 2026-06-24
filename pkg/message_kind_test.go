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
	"os"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/rotisserie/eris"
)

// TestMessageKindPopulated walks a real archive and asserts every message has
// Class + Kind populated, with Kind matching the router applied to the raw Class
// (the acceptance criterion: Class and Kind are set for every message, derived
// by ClassifyKind). It also confirms the walk surfaces at least one mail message.
func TestMessageKindPopulated(t *testing.T) {
	reader, err := os.Open("../data/enron.pst")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	pstFile, err := pst.New(reader)
	if err != nil {
		t.Fatalf("parse pst: %v", err)
	}
	t.Cleanup(func() { pstFile.Cleanup() })

	var checked, mail int
	walkErr := pstFile.WalkFolders(func(folder *pst.Folder) error {
		it, err := folder.GetMessageIterator()
		if eris.Is(err, pst.ErrMessagesNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		for it.Next() && checked < 200 {
			m := it.Value()
			if m == nil {
				t.Fatal("iterator yielded a nil message")
			}
			// Kind must be exactly what the router derives from the raw Class —
			// proving GetMessage populated it via classify, not left it zero by
			// accident (KindUnknown is a legitimate value, KindMail the default
			// for a class-less item).
			if want := pst.ClassifyKind(m.Class); m.Kind != want {
				t.Errorf("message %d: Kind=%v, ClassifyKind(%q)=%v", m.Identifier, m.Kind, m.Class, want)
			}
			if m.Kind == pst.KindMail {
				mail++
			}
			checked++
		}
		return it.Err()
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if checked == 0 {
		t.Fatal("no messages checked")
	}
	if mail == 0 {
		t.Error("expected at least one mail message in the enron fixture")
	}
	t.Logf("checked %d messages (%d mail)", checked, mail)
}
