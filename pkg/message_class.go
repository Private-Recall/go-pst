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
	"strings"

	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/tinylib/msgp/msgp"
)

// MessageKind is the family a message belongs to, derived from its
// PidTagMessageClass (0x001A). It lets a consumer branch on the message type
// (mail vs contact vs appointment …) without re-reading the class property or
// type-switching on Message.Properties.
//
// MAPI message classes are hierarchical and case-insensitive ([MS-OXCMSG]
// §2.2.1.1): "IPM.Contact.SBE" is a contact, "IPM.Appointment.MeetingPlace" an
// appointment. Classification therefore matches on the IPM.<family> prefix,
// case-insensitively, most-specific-first — not on an exact, case-sensitive
// string. A class that matches no known family is KindUnknown (a generic item
// backed by properties.Message), never silently relabeled as mail.
type MessageKind int

const (
	// KindUnknown is a message whose class matched no known family. It is backed
	// by properties.Message so its common fields still decode, but it is NOT
	// mail — consumers filtering for mail must exclude it.
	KindUnknown     MessageKind = iota
	KindMail                    // IPM.Note* and delivery/read reports (REPORT.*)
	KindContact                 // IPM.Contact*, IPM.AbchPerson
	KindAppointment             // IPM.Appointment*, IPM.Schedule.Meeting*
	KindTask                    // IPM.Task*
	KindJournal                 // IPM.Activity*
	KindDistList                // IPM.DistList*
	KindRSS                     // IPM.Post.Rss*
	KindNote                    // IPM.StickyNote* (a sticky note, not mail)
)

// String returns the kind's name (for logging/debugging).
func (k MessageKind) String() string {
	switch k {
	case KindMail:
		return "Mail"
	case KindContact:
		return "Contact"
	case KindAppointment:
		return "Appointment"
	case KindTask:
		return "Task"
	case KindJournal:
		return "Journal"
	case KindDistList:
		return "DistList"
	case KindRSS:
		return "RSS"
	case KindNote:
		return "Note"
	default:
		return "Unknown"
	}
}

// classRule maps a lowercased PidTagMessageClass prefix to a kind and the
// concrete properties type to decode into.
type classRule struct {
	prefix  string
	kind    MessageKind
	newProp func() msgp.Decodable
}

func newMessageProps() msgp.Decodable     { return &properties.Message{} }
func newContactProps() msgp.Decodable     { return &properties.Contact{} }
func newAppointmentProps() msgp.Decodable { return &properties.Appointment{} }
func newTaskProps() msgp.Decodable        { return &properties.Task{} }
func newJournalProps() msgp.Decodable     { return &properties.Journal{} }
func newAddressBookProps() msgp.Decodable { return &properties.AddressBook{} }
func newRSSProps() msgp.Decodable         { return &properties.RSS{} }
func newNoteProps() msgp.Decodable        { return &properties.Note{} }

// classRules is ordered most-specific-first; the first prefix that matches the
// lowercased class wins. Keep more-specific prefixes (e.g. "ipm.schedule.meeting")
// above their broader relatives, and keep this exported behavior in sync with
// ClassifyKind's documentation so consumers can predict the routing.
//
// IPM.Note.Mobile.SMS is intentionally left under the "ipm.note" → KindMail rule
// (it reads fine as a Message); a dedicated properties.SMS routing can be added
// later without changing the kind. Recognized-but-unmapped non-mail classes seen
// in real archives — IPM.Microsoft.*, IPM.OLE.*, IPM.Document.*, IPM.JetForm.*,
// IPM.InfoPathForm.* — are deliberately NOT listed: they fall through to
// KindUnknown (a generic item), never to KindMail.
var classRules = []classRule{
	// Recurring-appointment OLE class — a specific GUID upstream mapped to
	// Appointment. Kept as a first-class rule (above the general ipm.ole fall-
	// through to KindUnknown) so upgrading from upstream doesn't silently change
	// this item's type. More specific than "ipm.ole", so it must precede it.
	{"ipm.ole.class.{00061055-0000-0000-c000-000000000046}", KindAppointment, newAppointmentProps},
	{"ipm.schedule.meeting", KindAppointment, newAppointmentProps},
	{"ipm.schedule", KindAppointment, newAppointmentProps},
	{"ipm.appointment", KindAppointment, newAppointmentProps},
	{"ipm.contact", KindContact, newContactProps},
	{"ipm.abchperson", KindContact, newContactProps},
	{"ipm.distlist", KindDistList, newAddressBookProps},
	{"ipm.task", KindTask, newTaskProps},
	{"ipm.activity", KindJournal, newJournalProps},
	{"ipm.post.rss", KindRSS, newRSSProps},
	{"ipm.stickynote", KindNote, newNoteProps},
	{"ipm.note", KindMail, newMessageProps},
	{"report.", KindMail, newMessageProps},
}

// classify resolves a raw PidTagMessageClass to its kind and a factory for the
// properties type to decode into. An empty/whitespace class (the property is
// absent or unreadable) is treated conservatively as KindMail — a class-less
// item is, by MAPI convention, a plain message, and treating it as mail avoids
// dropping legitimate class-less mail. A non-empty class that matches no rule is
// KindUnknown (backed by properties.Message), never silently mail.
func classify(class string) (MessageKind, func() msgp.Decodable) {
	c := strings.ToLower(strings.TrimSpace(class))
	if c == "" {
		return KindMail, newMessageProps
	}
	for _, r := range classRules {
		if strings.HasPrefix(c, r.prefix) {
			return r.kind, r.newProp
		}
	}
	return KindUnknown, newMessageProps
}

// ClassifyKind returns the MessageKind for a raw PidTagMessageClass string,
// applying the same prefix-based, case-insensitive routing GetMessage uses to
// populate Message.Kind. It is exported so consumers can map a class to a kind
// directly (e.g. when filtering a folder's message-table classes) and so the
// class→kind contract is testable and visible. See classify for the empty/
// unknown-class semantics.
func ClassifyKind(class string) MessageKind {
	kind, _ := classify(class)
	return kind
}
