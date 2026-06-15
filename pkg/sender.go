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

// Sender SMTP property IDs. GetSenderEmailAddress (PidTagSenderEmailAddress) is
// already SMTP for internet mail but an EX legacyExchangeDN for Exchange senders;
// these carry the canonical SMTP when the export recorded it (modern Exchange/
// O365 do; older Exchange often did not — the address lived in the GAL).
const (
	propertyIDSenderSmtpAddress           = 23809 // 0x5D01 PidTagSenderSmtpAddress
	propertyIDSentRepresentingSmtpAddress = 23810 // 0x5D02 PidTagSentRepresentingSmtpAddress
)

// GetSenderSmtpAddress returns the message's PidTagSenderSmtpAddress (0x5D01) —
// the sender's canonical SMTP address — or "" when the property is absent. This
// property is NOT in the generated properties.Message struct, so it is read
// directly off the message property context.
func (message *Message) GetSenderSmtpAddress() (string, error) {
	return message.getStringProperty(propertyIDSenderSmtpAddress)
}

// GetSentRepresentingSmtpAddress returns PidTagSentRepresentingSmtpAddress
// (0x5D02) — the canonical SMTP of the represented ("on behalf of") sender — or
// "" when absent. A useful fallback when GetSenderSmtpAddress is empty.
func (message *Message) GetSentRepresentingSmtpAddress() (string, error) {
	return message.getStringProperty(propertyIDSentRepresentingSmtpAddress)
}

// getStringProperty reads a string property off the message property context by
// ID, returning "" (no error) when the property is absent or carries no data.
// It uses GetStringValue so both Unicode (PropertyTypeString) and ANSI
// (PropertyTypeString8) values decode.
func (message *Message) getStringProperty(propertyID uint16) (string, error) {
	reader, err := message.PropertyContext.GetPropertyReader(propertyID, message.LocalDescriptors)

	if eris.Is(err, ErrPropertyNotFound) {
		return "", nil
	} else if err != nil {
		return "", eris.Wrap(err, "failed to get property reader")
	}

	value, err := reader.GetStringValue()

	if eris.Is(err, ErrPropertyNoData) || eris.Is(err, ErrPropertyTypeMismatch) {
		return "", nil
	}

	return value, err
}
