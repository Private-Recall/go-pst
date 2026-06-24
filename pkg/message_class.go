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
// PidTagMessageClass (0x001A) by the prefix router. It is the canonical signal
// for consumers: classify on Message.Kind rather than re-reading the class
// property or type-switching on Message.Properties.
//
// A class that matches no known family resolves to KindUnknown with its raw
// Class string preserved — it is never silently relabeled as KindMail.
type MessageKind int

const (
	// KindUnknown is an unrecognized or unreadable message class. Its Properties
	// is a best-effort generic properties.Message container.
	KindUnknown MessageKind = iota
	KindMail                // IPM.Note*, REPORT.* (delivery/read receipts)
	KindContact             // IPM.Contact*, IPM.AbchPerson
	KindAppointment         // IPM.Appointment*, IPM.Schedule.Meeting*
	KindTask                // IPM.Task*
	KindJournal             // IPM.Activity*
	KindDistList            // IPM.DistList*
	KindRSS                 // IPM.Post.Rss*
	KindNote                // IPM.StickyNote* (Outlook sticky note)
	KindSMS                 // IPM.Note.Mobile.SMS / .MMS
)

// String returns the enum name, for logging and tests.
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
	case KindSMS:
		return "SMS"
	default:
		return "Unknown"
	}
}

// messageClassRoute maps a lowercased PidTagMessageClass prefix to the family it
// belongs to and a factory for the properties struct that decodes it.
type messageClassRoute struct {
	prefix        string
	kind          MessageKind
	newProperties func() msgp.Decodable
}

// messageClassRoutes is the prefix routing table for PidTagMessageClass.
//
// MAPI message classes are hierarchical and case-insensitive ([MS-OXCMSG]
// §2.2.1.1), so routing is by lowercased prefix on a "." boundary, evaluated
// most-specific-first: the first entry whose prefix equals the class or is a
// dotted ancestor of it wins. Order therefore matters — e.g. IPM.Note.Mobile.SMS
// must precede IPM.Note so it routes to SMS, not Mail.
//
// Real-world subclasses that the previous exact-match dispatch mis-routed to
// Mail (IPM.Contact.SBE, IPM.Appointment.MeetingPlace, IPM.Schedule.Meeting.Resp.*,
// IPM.NOTE.SECURE.SIGN, …) are all handled here by prefix.
var messageClassRoutes = []messageClassRoute{
	// Most specific first.
	{"ipm.note.mobile.sms", KindSMS, func() msgp.Decodable { return &properties.SMS{} }},
	{"ipm.note.mobile.mms", KindSMS, func() msgp.Decodable { return &properties.SMS{} }},
	{"ipm.schedule.meeting", KindAppointment, func() msgp.Decodable { return &properties.Appointment{} }},
	{"ipm.appointment", KindAppointment, func() msgp.Decodable { return &properties.Appointment{} }},
	// The appointment OLE class GUID, stored verbatim by some Outlook versions.
	{"ipm.ole.class.{00061055-0000-0000-c000-000000000046}", KindAppointment, func() msgp.Decodable { return &properties.Appointment{} }},
	{"ipm.contact", KindContact, func() msgp.Decodable { return &properties.Contact{} }},
	{"ipm.abchperson", KindContact, func() msgp.Decodable { return &properties.Contact{} }},
	{"ipm.distlist", KindDistList, func() msgp.Decodable { return &properties.AddressBook{} }},
	{"ipm.task", KindTask, func() msgp.Decodable { return &properties.Task{} }},
	{"ipm.activity", KindJournal, func() msgp.Decodable { return &properties.Journal{} }},
	{"ipm.post.rss", KindRSS, func() msgp.Decodable { return &properties.RSS{} }},
	{"ipm.stickynote", KindNote, func() msgp.Decodable { return &properties.Note{} }},
	{"ipm.note", KindMail, func() msgp.Decodable { return &properties.Message{} }},
	// Delivery/read receipts and non-delivery reports.
	{"report", KindMail, func() msgp.Decodable { return &properties.Message{} }},
}

// classifyMessageClass routes a raw PidTagMessageClass to its MessageKind and a
// factory for the properties struct that decodes it. Matching is
// case-insensitive and on a "." hierarchy boundary. A class that matches no
// known family — including non-mail container classes such as IPM.Microsoft.*,
// IPM.Ole.*, IPM.Document.*, IPM.JetForm.*, IPM.InfoPathForm.*, and the empty
// class of an unreadable property — resolves to KindUnknown with a generic
// properties.Message container, and is never relabeled as KindMail.
func classifyMessageClass(class string) (MessageKind, func() msgp.Decodable) {
	lower := strings.ToLower(strings.TrimSpace(class))

	for _, route := range messageClassRoutes {
		if lower == route.prefix || strings.HasPrefix(lower, route.prefix+".") {
			return route.kind, route.newProperties
		}
	}

	return KindUnknown, func() msgp.Decodable { return &properties.Message{} }
}
