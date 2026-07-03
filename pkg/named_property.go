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

import "github.com/rotisserie/eris"

// Named-property access.
//
// A named property (PidLid*/PidName*) has a canonical numeric identifier — the
// "long ID" (LID) defined by [MS-OXPROPS], e.g. 0x820D for
// PidLidAppointmentStartWhole — but that canonical ID is NOT the ID the property
// is stored under. Every PST assigns its own file-specific property ID (>=
// 0x8000) to each named property and records the mapping in the Name-To-ID map
// (see GetNameToIDMap). So a named property cannot be read by its canonical LID
// directly; it must first be resolved through that map.
//
// The generated property structs (properties.Appointment, …) do not populate
// named properties: PropertyContext.Populate keys each value by its file-specific
// ID, while the struct tags use a different, non-LID scheme, so the two never
// match and every named field decodes to its zero value. A caller that needs an
// appointment's start/end time, its location or recurrence, an Outlook category,
// a follow-up flag, or any other named property reads it through the accessors
// below.

// GetNamedPropertyReader resolves a named property — identified by its canonical
// LID and property set (e.g. 0x820D + PropertySetAppointment) — to a reader over
// this message's property context. Read the value with the reader's typed getters
// (GetDate, GetString, GetBoolean, …), or use the GetNamedTime / GetNamedString /
// GetNamedBoolean convenience wrappers below.
//
// It returns a wrapped ErrNameToIDMapKeyNotFound when the PST does not define the
// named property — an ordinary "not present" outcome the caller can treat as an
// absent field, distinct from a decode error.
func (message *Message) GetNamedPropertyReader(lid int, propertySet PropertySet) (PropertyReader, error) {
	if message.File == nil || message.File.NameToIDMap == nil {
		return PropertyReader{}, eris.New("go-pst: Name-To-ID map is unavailable")
	}

	id, err := message.File.NameToIDMap.GetPropertyID(lid, propertySet)

	if err != nil {
		return PropertyReader{}, eris.Wrap(err, "failed to resolve named property to a file-specific ID")
	}

	if id < 0 || id > 0xFFFF {
		return PropertyReader{}, eris.Errorf("go-pst: resolved named property ID out of range: %d", id)
	}

	return message.PropertyContext.GetPropertyReader(uint16(id), message.LocalDescriptors)
}

// GetNamedTime reads a named property of type PtypTime as Unix nanoseconds (the
// same convention as GetDate). Convenience over GetNamedPropertyReader for the
// common case — appointment start/end, reminder time, and similar.
func (message *Message) GetNamedTime(lid int, propertySet PropertySet) (int64, error) {
	reader, err := message.GetNamedPropertyReader(lid, propertySet)

	if err != nil {
		return 0, err
	}

	return reader.GetDate()
}

// GetNamedString reads a named property of type PtypString (Unicode) or
// PtypString8 (ANSI / code-page) as a Go string, mirroring the Unicode/ANSI
// handling the body and folder-name decoders use so it works on both PST
// variants.
func (message *Message) GetNamedString(lid int, propertySet PropertySet) (string, error) {
	reader, err := message.GetNamedPropertyReader(lid, propertySet)

	if err != nil {
		return "", err
	}

	if reader.Property.Type == PropertyTypeString8 {
		return reader.GetString8(1252)
	}

	return reader.GetString()
}

// GetNamedBoolean reads a named property of type PtypBoolean.
func (message *Message) GetNamedBoolean(lid int, propertySet PropertySet) (bool, error) {
	reader, err := message.GetNamedPropertyReader(lid, propertySet)

	if err != nil {
		return false, err
	}

	return reader.GetBoolean()
}
