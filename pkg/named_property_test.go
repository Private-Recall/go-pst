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

	"github.com/rotisserie/eris"

	pst "github.com/mooijtech/go-pst/v6/pkg"
)

// PidLidAppointmentStartWhole ([MS-OXOCAL] 2.2.1.5) — the appointment's start
// time, a PtypTime named property in PSETID_Appointment. Named properties are
// not decoded into the generated structs, so they are read through the
// GetNamed* accessors this test exercises.
const pidLidAppointmentStartWhole = 0x820D

// TestGetNamedTimeAppointmentStart proves the named-property accessors resolve a
// PSETID_Appointment property through the Name-To-ID map and read its value. The
// 32-bit sample carries a single appointment ("Olympus training"); its start
// time is a named property that the properties.Appointment struct leaves at zero,
// but GetNamedTime reads it. The expected value is pinned so a regression in the
// Name-To-ID resolution or the FILETIME→Unix conversion is caught.
func TestGetNamedTimeAppointmentStart(t *testing.T) {
	reader, err := os.Open("../data/32-bit.pst")
	if err != nil {
		t.Fatalf("failed to open sample: %v", err)
	}
	defer reader.Close()

	pstFile, err := pst.New(reader)
	if err != nil {
		t.Fatalf("failed to open PST file: %v", err)
	}
	defer pstFile.Cleanup()

	var start int64
	var sawAppointment bool
	err = pstFile.WalkFolders(func(folder *pst.Folder) error {
		iterator, err := folder.GetMessageIterator()
		if err != nil {
			return nil
		}
		for iterator.Next() {
			message := iterator.Value()
			if message == nil || message.Kind != pst.KindAppointment {
				continue
			}
			sawAppointment = true
			start, err = message.GetNamedTime(pidLidAppointmentStartWhole, pst.PropertySetAppointment)
			if err != nil {
				t.Errorf("GetNamedTime(AppointmentStartWhole) failed: %v", err)
			}
		}
		return iterator.Err()
	})
	if err != nil {
		t.Fatalf("WalkFolders failed: %v", err)
	}

	if !sawAppointment {
		t.Fatal("no appointment found in the sample")
	}
	// 1092940200000000000 ns = 2004-08-19 18:30:00 UTC — the sample appointment's
	// start. If this comes back zero, named-property resolution is broken again
	// (the exact failure the generated struct still has).
	const want = int64(1092940200000000000)
	if start != want {
		t.Errorf("appointment start = %d, want %d", start, want)
	}
}

// TestGetNamedPropertyReaderNotFound: an absent named property returns a wrapped
// ErrNameToIDMapKeyNotFound, so callers can distinguish "not present" from a
// decode error and treat it as an empty field.
func TestGetNamedPropertyReaderNotFound(t *testing.T) {
	reader, err := os.Open("../data/32-bit.pst")
	if err != nil {
		t.Fatalf("failed to open sample: %v", err)
	}
	defer reader.Close()

	pstFile, err := pst.New(reader)
	if err != nil {
		t.Fatalf("failed to open PST file: %v", err)
	}
	defer pstFile.Cleanup()

	var checked bool
	err = pstFile.WalkFolders(func(folder *pst.Folder) error {
		iterator, err := folder.GetMessageIterator()
		if err != nil {
			return nil
		}
		for iterator.Next() {
			message := iterator.Value()
			if message == nil {
				continue
			}
			checked = true
			// 0x9999 is not a defined named property in this sample.
			if _, err := message.GetNamedPropertyReader(0x9999, pst.PropertySetAppointment); !eris.Is(err, pst.ErrNameToIDMapKeyNotFound) {
				t.Errorf("GetNamedPropertyReader(bogus) err = %v, want ErrNameToIDMapKeyNotFound", err)
			}
			return nil
		}
		return iterator.Err()
	})
	if err != nil {
		t.Fatalf("WalkFolders failed: %v", err)
	}
	if !checked {
		t.Fatal("no message to probe")
	}
}
