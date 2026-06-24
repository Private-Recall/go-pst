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
	"testing"

	"github.com/mooijtech/go-pst/v6/pkg/properties"
)

// TestClassifyKind locks the prefix-based, case-insensitive routing: exact
// classes, real-world subclasses (which the old exact-match dispatch mis-routed
// to mail), case variants, and the unknown/empty fallbacks.
func TestClassifyKind(t *testing.T) {
	cases := []struct {
		class string
		want  MessageKind
	}{
		// Mail: IPM.Note and its subclasses, plus delivery/read reports.
		{"IPM.Note", KindMail},
		{"IPM.Note.SMIME.MultipartSigned", KindMail},
		{"IPM.NOTE.SECURE.SIGN", KindMail}, // mixed/upper case observed in real archives
		{"IPM.Note.Mobile.SMS", KindMail},  // SMS reads fine as mail (documented)
		{"REPORT.IPM.Note.NDR", KindMail},
		// Contact: exact and the SBE subclass the old dispatch dropped.
		{"IPM.Contact", KindContact},
		{"IPM.Contact.SBE", KindContact},
		{"ipm.contact", KindContact}, // lowercase
		{"IPM.AbchPerson", KindContact},
		// Appointment / meeting family.
		{"IPM.Appointment", KindAppointment},
		{"IPM.Appointment.MeetingPlace", KindAppointment},
		{"IPM.Schedule.Meeting.Request", KindAppointment},
		{"IPM.Schedule.Meeting.Resp.Pos", KindAppointment},
		{"IPM.Schedule.Meeting.Canceled", KindAppointment},
		// Other mapped families.
		{"IPM.Task", KindTask},
		{"IPM.Task.Assign", KindTask},
		{"IPM.Activity", KindJournal},
		{"IPM.Post.Rss", KindRSS},
		{"IPM.DistList", KindDistList},
		{"IPM.StickyNote", KindNote},
		// Empty/absent class is conservatively mail; unknown classes are never mail.
		{"", KindMail},
		{"   ", KindMail},
		// Prefix, not substring: "IPM.Microsoft.*" does not start with a mapped
		// family prefix, so it is Unknown even though "Schedule" appears later.
		{"IPM.Microsoft.Schedule.Whatever", KindUnknown},
		{"IPM.Microsoft.WunderBar", KindUnknown},
		{"IPM.OLE.CLASS.{00061055-0000-0000-C000-000000000046}", KindUnknown},
		{"IPM.JetForm.Something", KindUnknown},
		{"IPM.Document", KindUnknown},
		{"Totally.Bogus", KindUnknown},
	}

	for _, c := range cases {
		if got := ClassifyKind(c.class); got != c.want {
			t.Errorf("ClassifyKind(%q) = %v, want %v", c.class, got, c.want)
		}
	}
}

// TestClassifyPropertiesType verifies the concrete properties.* type each kind
// decodes into — the acceptance criterion that a subclass routes to the correct
// type, not properties.Message.
func TestClassifyPropertiesType(t *testing.T) {
	cases := []struct {
		class string
		want  interface{}
	}{
		{"IPM.Note", &properties.Message{}},
		{"IPM.Contact.SBE", &properties.Contact{}},
		{"IPM.Appointment.MeetingPlace", &properties.Appointment{}},
		{"IPM.Schedule.Meeting.Request", &properties.Appointment{}},
		{"IPM.Task", &properties.Task{}},
		{"IPM.Activity", &properties.Journal{}},
		{"IPM.Post.Rss", &properties.RSS{}},
		{"IPM.DistList", &properties.AddressBook{}},
		{"IPM.StickyNote", &properties.Note{}},
		{"IPM.Unmapped.Thing", &properties.Message{}}, // unknown decodes into Message
		{"", &properties.Message{}},
	}

	for _, c := range cases {
		_, newProps := classify(c.class)
		got := newProps()
		if typeName(got) != typeName(c.want) {
			t.Errorf("classify(%q) props = %T, want %T", c.class, got, c.want)
		}
	}
}

func typeName(v interface{}) string {
	switch v.(type) {
	case *properties.Message:
		return "Message"
	case *properties.Contact:
		return "Contact"
	case *properties.Appointment:
		return "Appointment"
	case *properties.Task:
		return "Task"
	case *properties.Journal:
		return "Journal"
	case *properties.RSS:
		return "RSS"
	case *properties.AddressBook:
		return "AddressBook"
	case *properties.Note:
		return "Note"
	default:
		return "other"
	}
}

func TestMessageKindString(t *testing.T) {
	cases := map[MessageKind]string{
		KindUnknown:     "Unknown",
		KindMail:        "Mail",
		KindContact:     "Contact",
		KindAppointment: "Appointment",
		KindTask:        "Task",
		KindJournal:     "Journal",
		KindDistList:    "DistList",
		KindRSS:         "RSS",
		KindNote:        "Note",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("MessageKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
