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

	"github.com/rotisserie/eris"
)

// Recipient property IDs (MS-OXPROPS). The recipient table stores one row per
// To/Cc/Bcc recipient; these are the columns we read.
const (
	propIDRecipientDisplayName  = 12289 // 0x3001 PidTagDisplayName
	propIDRecipientAddressType  = 12290 // 0x3002 PidTagAddressType ("SMTP", "EX")
	propIDRecipientEmailAddress = 12291 // 0x3003 PidTagEmailAddress (EX legacyDN for Exchange recipients)
	propIDRecipientSmtpAddress  = 14846 // 0x39FE PidTagSmtpAddress (canonical SMTP, when present)
	propIDRecipientType         = 3093  // 0x0C15 PidTagRecipientType
)

// recipientTableIdentifier is the fixed NID of a message's recipient table
// (MS-PST §2.4.5.3, NID_RECIPIENT_TABLE = 0x692). Mirrors the attachment
// table's 0x671 (1649) used by GetAttachmentTableContext.
const recipientTableIdentifier = 1682 // 0x692

// RecipientType classifies a recipient (PidTagRecipientType, MS-OXCMSG §2.2.3.1.2).
type RecipientType int32

const (
	RecipientTypeUnknown RecipientType = 0
	RecipientTypeTo      RecipientType = 1
	RecipientTypeCc      RecipientType = 2
	RecipientTypeBcc     RecipientType = 3
)

// Recipient is one row of a message's recipient table — a single To/Cc/Bcc
// addressee with its canonical address(es), unlike the PidTagDisplayTo/Cc
// header strings which are a display-only, semicolon-joined mix of names and
// addresses.
type Recipient struct {
	DisplayName  string
	AddressType  string // "SMTP", "EX", ...
	EmailAddress string // PidTagEmailAddress (an EX legacyExchangeDN for Exchange recipients)
	SmtpAddress  string // PidTagSmtpAddress (canonical SMTP, when the PST recorded one)
	Type         RecipientType
}

// SMTP returns the recipient's best canonical SMTP address: PidTagSmtpAddress
// when present, otherwise PidTagEmailAddress when the address type is SMTP and
// it looks like an address. Returns "" for Exchange-only recipients (an EX
// legacyExchangeDN with no SMTP address recorded) — callers fall back to
// DisplayName for those.
func (recipient Recipient) SMTP() string {
	if recipient.SmtpAddress != "" {
		return recipient.SmtpAddress
	}

	if strings.EqualFold(recipient.AddressType, "SMTP") && strings.Contains(recipient.EmailAddress, "@") {
		return recipient.EmailAddress
	}

	return ""
}

// GetRecipientTableContext returns the table context holding this message's
// recipients (To/Cc/Bcc). It mirrors GetAttachmentTableContext but points at
// the recipient table (NID 0x692) and fetches the recipient properties
// directly — each recipient's properties live in its own table row, so unlike
// attachments there is no per-row sub-object HNID to follow.
//
// Returns ErrRecipientsNotFound when the message has no recipient table.
func (message *Message) GetRecipientTableContext() (*TableContext, error) {
	recipientLocalDescriptor, err := FindLocalDescriptor(recipientTableIdentifier, message.LocalDescriptors)

	if eris.Is(err, ErrLocalDescriptorNotFound) {
		return nil, ErrRecipientsNotFound
	} else if err != nil {
		return nil, eris.Wrap(err, "failed to find recipient local descriptor")
	}

	recipientHeapOnNode, err := message.File.GetHeapOnNodeFromLocalDescriptor(recipientLocalDescriptor)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get recipient Heap-on-Node")
	}

	recipientLocalDescriptors, err := message.File.GetLocalDescriptorsFromIdentifier(recipientLocalDescriptor.LocalDescriptorsIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get recipient local descriptors")
	}

	tableContext, err := message.File.GetTableContext(recipientHeapOnNode, recipientLocalDescriptors,
		propIDRecipientDisplayName, propIDRecipientAddressType, propIDRecipientEmailAddress, propIDRecipientSmtpAddress, propIDRecipientType)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get recipient table context")
	}

	return &tableContext, nil
}

// GetRecipients returns the message's recipients with their canonical
// addresses. Returns (nil, nil) when the message has no recipient table, so
// callers can range over the result without special-casing ErrRecipientsNotFound.
//
// Per-property read errors on a single column are tolerated (that column is
// left empty) rather than failing the whole message — a malformed recipient
// row in an attacker-supplied PST must not abort ingest.
func (message *Message) GetRecipients() ([]Recipient, error) {
	return guard("GetRecipients", message.getRecipients)
}

func (message *Message) getRecipients() ([]Recipient, error) {
	tableContext, err := message.GetRecipientTableContext()

	if eris.Is(err, ErrRecipientsNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, eris.Wrap(err, "failed to get recipient table context")
	}

	recipients := make([]Recipient, 0, len(tableContext.Properties))

	for _, row := range tableContext.Properties {
		var recipient Recipient

		for _, property := range row {
			propertyReader, err := tableContext.GetPropertyReader(property)

			if err != nil {
				return nil, eris.Wrap(err, "failed to get recipient property reader")
			}

			switch property.ID {
			case propIDRecipientDisplayName:
				if value, err := propertyReader.GetStringValue(); err == nil {
					recipient.DisplayName = value
				}
			case propIDRecipientAddressType:
				if value, err := propertyReader.GetStringValue(); err == nil {
					recipient.AddressType = value
				}
			case propIDRecipientEmailAddress:
				if value, err := propertyReader.GetStringValue(); err == nil {
					recipient.EmailAddress = value
				}
			case propIDRecipientSmtpAddress:
				if value, err := propertyReader.GetStringValue(); err == nil {
					recipient.SmtpAddress = value
				}
			case propIDRecipientType:
				if value, err := propertyReader.GetInteger32(); err == nil {
					recipient.Type = RecipientType(value)
				}
			}
		}

		recipients = append(recipients, recipient)
	}

	return recipients, nil
}
