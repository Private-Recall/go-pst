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
	"fmt"
	"testing"

	"github.com/mooijtech/go-pst/v6/pkg/properties"
)

// TestClassifyMessageClass locks the prefix router: every entry is the (class →
// Kind, concrete properties type) contract that consumers depend on. The
// IPM.*.Sub rows are the real-world subclasses the old exact-match dispatch
// mis-routed to Mail (issue #1 P1).
func TestClassifyMessageClass(t *testing.T) {
	tests := []struct {
		class    string
		wantKind MessageKind
		wantType interface{}
	}{
		// Mail.
		{"IPM.Note", KindMail, &properties.Message{}},
		{"IPM.Note.SMIME.MultipartSigned", KindMail, &properties.Message{}},
		{"IPM.NOTE.SECURE.SIGN", KindMail, &properties.Message{}}, // caps subclass
		{"REPORT.IPM.NOTE.NDR", KindMail, &properties.Message{}},  // non-delivery report
		{"REPORT.IPM.Note.IPNRN", KindMail, &properties.Message{}}, // read receipt

		// Contact — IPM.Contact.SBE is the data-loss case (was routed to Mail).
		{"IPM.Contact", KindContact, &properties.Contact{}},
		{"IPM.Contact.SBE", KindContact, &properties.Contact{}},
		{"IPM.AbchPerson", KindContact, &properties.Contact{}},

		// Appointment / meeting.
		{"IPM.Appointment", KindAppointment, &properties.Appointment{}},
		{"IPM.Appointment.MeetingPlace", KindAppointment, &properties.Appointment{}},
		{"IPM.Schedule.Meeting.Request", KindAppointment, &properties.Appointment{}},
		{"IPM.Schedule.Meeting.Resp.Pos", KindAppointment, &properties.Appointment{}},
		{"IPM.Schedule.Meeting.Canceled", KindAppointment, &properties.Appointment{}},
		{"IPM.OLE.CLASS.{00061055-0000-0000-C000-000000000046}", KindAppointment, &properties.Appointment{}},

		// SMS must beat the IPM.Note prefix (ordering check).
		{"IPM.Note.Mobile.SMS", KindSMS, &properties.SMS{}},
		{"IPM.Note.Mobile.MMS", KindSMS, &properties.SMS{}},

		// Task / Journal / DistList / RSS / sticky note.
		{"IPM.Task", KindTask, &properties.Task{}},
		{"IPM.Task.Assignment", KindTask, &properties.Task{}},
		{"IPM.Activity", KindJournal, &properties.Journal{}},
		{"IPM.DistList", KindDistList, &properties.AddressBook{}},
		{"IPM.Post.Rss", KindRSS, &properties.RSS{}},
		{"IPM.StickyNote", KindNote, &properties.Note{}},

		// Case-insensitivity across the board.
		{"ipm.contact.sbe", KindContact, &properties.Contact{}},
		{"IpM.ApPoInTmEnT", KindAppointment, &properties.Appointment{}},

		// Non-mail container classes and genuine unknowns → KindUnknown, never Mail.
		{"IPM.Microsoft.Approval.Reply.Positive", KindUnknown, &properties.Message{}},
		{"IPM.Ole.Class", KindUnknown, &properties.Message{}},
		{"IPM.Document", KindUnknown, &properties.Message{}},
		{"IPM.JetForm.Foo", KindUnknown, &properties.Message{}},
		{"IPM.InfoPathForm.Bar", KindUnknown, &properties.Message{}},
		{"IPM.Whatever.New", KindUnknown, &properties.Message{}},
		{"", KindUnknown, &properties.Message{}}, // unreadable/absent class
		{"   ", KindUnknown, &properties.Message{}},

		// Prefix must respect the "." boundary: "IPM.Notebook" is not "IPM.Note".
		{"IPM.Notebook", KindUnknown, &properties.Message{}},
	}

	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			gotKind, newProperties := classifyMessageClass(tt.class)

			if gotKind != tt.wantKind {
				t.Errorf("classifyMessageClass(%q) kind = %v, want %v", tt.class, gotKind, tt.wantKind)
			}

			gotType := fmt.Sprintf("%T", newProperties())
			wantType := fmt.Sprintf("%T", tt.wantType)

			if gotType != wantType {
				t.Errorf("classifyMessageClass(%q) type = %s, want %s", tt.class, gotType, wantType)
			}
		})
	}
}

// TestMessageKindString guards the String() mapping used in logs/tests.
func TestMessageKindString(t *testing.T) {
	tests := []struct {
		kind MessageKind
		want string
	}{
		{KindUnknown, "Unknown"},
		{KindMail, "Mail"},
		{KindContact, "Contact"},
		{KindAppointment, "Appointment"},
		{KindTask, "Task"},
		{KindJournal, "Journal"},
		{KindDistList, "DistList"},
		{KindRSS, "RSS"},
		{KindNote, "Note"},
		{KindSMS, "SMS"},
		{MessageKind(999), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("MessageKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
