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
	_ "embed"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/rotisserie/eris"
	"github.com/tinylib/msgp/msgp"
)

// Message represents a message.
type Message struct {
	File                   *File
	Identifier             Identifier
	PropertyContext        *PropertyContext
	AttachmentTableContext *TableContext
	LocalDescriptors       []LocalDescriptor // Used by the PropertyContext and TableContext.
	Properties             msgp.Decodable    // Type properties.Message, properties.Appointment, properties.Contact
	Class                  string            // Raw PidTagMessageClass (0x001A), e.g. "IPM.Contact.SBE". Empty when unreadable.
	Kind                   MessageKind       // Family derived from Class by the prefix router; classify on this, not Properties.
}

// GetMessageTableContext returns the message table context of this folder which contains references to all messages.
// Note this only returns the identifier of each message.
func (folder *Folder) GetMessageTableContext() (TableContext, error) {
	emailsIdentifier := folder.Identifier + 12

	emailsNode, err := folder.File.GetNodeBTreeNode(emailsIdentifier)

	if err != nil {
		return TableContext{}, eris.Wrap(err, "failed to find node b-tree node")
	}

	localDescriptors, err := folder.File.GetLocalDescriptors(emailsNode)

	if err != nil {
		return TableContext{}, eris.Wrap(err, "failed to find local descriptors")
	}

	emailsDataNode, err := folder.File.GetDataBTreeNode(emailsIdentifier)

	if err != nil {
		return TableContext{}, eris.Wrap(err, "failed to find data b-tree node")
	}

	emailsHeapOnNode, err := folder.File.GetHeapOnNode(emailsDataNode)

	if err != nil {
		return TableContext{}, eris.Wrap(err, "failed to get Heap-on-Node")
	}

	// 26610 is a message property HNID.
	tableContext, err := folder.File.GetTableContext(emailsHeapOnNode, localDescriptors, 26610)

	if err != nil {
		return TableContext{}, eris.Wrap(err, "failed to get table context")
	}

	return tableContext, nil
}

// MessageIterator implements a message iterator.
type MessageIterator struct {
	file                *File
	messageTableContext TableContext

	err            error
	currentIndex   int
	currentMessage *Message
}

// Err return the error cause.
func (messageIterator *MessageIterator) Err() error {
	return messageIterator.err
}

// Next advances the iterator and reports whether a message is available.
//
// Contract: a read failure surfaces via Err() and terminates iteration (Next
// returns false and does not retry the failed row). A malformed row that yields
// no message is skipped, never surfaced — so after Next returns true, Value() is
// always non-nil. A panic in the decode paths is recovered into Err().
func (messageIterator *MessageIterator) Next() bool {
	advanced, err := guard("MessageIterator.Next", messageIterator.next)

	if err != nil {
		messageIterator.err = err
		return false
	}

	return advanced
}

func (messageIterator *MessageIterator) next() (bool, error) {
	for messageIterator.currentIndex < len(messageIterator.messageTableContext.Properties) {
		row := messageIterator.messageTableContext.Properties[messageIterator.currentIndex]

		// Advance before processing so a failed or empty row is never retried.
		messageIterator.currentIndex++

		var currentMessage *Message

		for _, property := range row {
			// We only return the message identifier in GetMessageTableContext,
			// so we don't need to check the property ID here.
			propertyReader, err := messageIterator.messageTableContext.GetPropertyReader(property)

			if err != nil {
				return false, eris.Wrap(err, "failed to get property reader")
			}

			messageIdentifier, err := propertyReader.GetInteger32()

			if err != nil {
				return false, eris.Wrap(err, "failed to get message identifier")
			}

			message, err := messageIterator.file.GetMessage(Identifier(messageIdentifier))

			if err != nil {
				return false, eris.Wrapf(err, "failed to find message: %d", messageIdentifier)
			}

			currentMessage = message
		}

		if currentMessage == nil {
			// Malformed/empty row produced no message — skip it, never yield nil.
			continue
		}

		messageIterator.currentMessage = currentMessage

		return true, nil
	}

	return false, nil
}

// Value returns the current value in the iterator.
func (messageIterator *MessageIterator) Value() *Message {
	return messageIterator.currentMessage
}

// Size returns the amount of messages in the message iterator.
func (messageIterator *MessageIterator) Size() int {
	return len(messageIterator.messageTableContext.Properties)
}

func (messageIterator *MessageIterator) CurrentIndex() int {
	return messageIterator.currentIndex
}

// GetMessageIterator returns an iterator for messages.
func (folder *Folder) GetMessageIterator() (MessageIterator, error) {
	if folder.MessageCount == 0 {
		return MessageIterator{}, ErrMessagesNotFound
	} else if folder.Identifier.GetType() == IdentifierTypeSearchFolder {
		return MessageIterator{}, ErrMessagesNotFound
	}

	messageTableContext, err := folder.GetMessageTableContext()

	if err != nil {
		return MessageIterator{}, eris.Wrap(err, "failed to get message table context")
	}

	return MessageIterator{
		file:                folder.File,
		messageTableContext: messageTableContext,
	}, nil
}

// GetAllMessages returns an array of all messages from the message table context.
// See GetMessageIterator.
func (folder *Folder) GetAllMessages() ([]*Message, error) {
	messageIterator, err := folder.GetMessageIterator()

	if err != nil {
		return nil, err
	}

	var messages []*Message

	for messageIterator.Next() {
		messages = append(messages, messageIterator.Value())
	}

	return messages, messageIterator.Err()
}

// GetMessage returns the message of the identifier.
//
// A malformed message returns an error (including a recovered panic from the
// decode paths) rather than crashing the caller.
func (file *File) GetMessage(identifier Identifier) (*Message, error) {
	return guard("GetMessage", func() (*Message, error) {
		return file.getMessage(identifier)
	})
}

func (file *File) getMessage(identifier Identifier) (*Message, error) {
	if identifier.GetType() != IdentifierTypeNormalMessage {
		return nil, ErrMessageIdentifierTypeInvalid
	}

	messageNode, err := file.GetNodeBTreeNode(identifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find node b-tree node")
	}

	messageDataNode, err := file.GetBlockBTreeNode(messageNode.DataIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find block b-tree node")
	}

	messageHeapOnNode, err := file.GetHeapOnNode(messageDataNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get Heap-on-Node")
	}

	localDescriptors, err := file.GetLocalDescriptors(messageNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find local descriptors")
	}

	propertyContext, err := file.GetPropertyContext(messageHeapOnNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get property context")
	}

	// Resolve PidTagMessageClass (0x001A) and route it to the matching properties
	// type. The raw class and derived Kind are recorded on the returned Message so
	// consumers classify on msg.Kind rather than re-reading the property or
	// type-switching on Properties. An absent or unreadable class leaves Class
	// empty and routes to KindUnknown — never silently to mail.
	var messageClass string

	if messageClassPropertyReader, err := propertyContext.GetPropertyReader(26, localDescriptors); err == nil {
		if class, err := messageClassPropertyReader.GetStringValue(); err == nil {
			messageClass = class
		}
	}

	messageKind, newProperties := classifyMessageClass(messageClass)
	messageProperties := newProperties()

	if err := propertyContext.Populate(messageProperties, localDescriptors); err != nil {
		return nil, eris.Wrap(err, "failed to populate message properties")
	}

	return &Message{
		File:             file,
		Identifier:       identifier,
		PropertyContext:  propertyContext,
		LocalDescriptors: localDescriptors,
		Properties:       messageProperties,
		Class:            messageClass,
		Kind:             messageKind,
	}, nil
}

// GetBodyRTF return the RTF body, may be
func (message *Message) GetBodyRTF() (string, error) {
	return guard("GetBodyRTF", message.getBodyRTF)
}

func (message *Message) getBodyRTF() (string, error) {
	rtfPropertyReader, err := message.PropertyContext.GetPropertyReader(4105, message.LocalDescriptors)

	if err != nil {
		return "", err
	}

	if err := message.File.checkAllocSize(rtfPropertyReader.Size()); err != nil {
		return "", err
	}

	rtfBody := make([]byte, rtfPropertyReader.Size())

	if _, err := rtfPropertyReader.ReadAt(rtfBody, 0); err != nil {
		return "", errors.WithStack(err)
	}

	return NewRTFDecoder().Decode(rtfBody)
}

// GetBodyHTML returns the message's HTML body (PidTagHtml, property 0x1013).
//
// Outlook, Exchange and most webmail store this property as PtypBinary — the
// HTML bytes in the message's internet code page, which is UTF-8 in the
// overwhelming majority of modern mail — while some senders store it as a
// Unicode (PtypString) or ANSI (PtypString8) string. The generated
// properties.Message.GetBodyHtml only maps the Unicode-string form (its msgp key
// is keyed to PtypString), so a binary HTML body reads back empty there. This
// reads the property directly and decodes whichever of the three forms is
// present. Returns the underlying ErrPropertyNotFound (from GetPropertyReader)
// when the message carries no HTML body.
func (message *Message) GetBodyHTML() (string, error) {
	return guard("GetBodyHTML", message.getBodyHTML)
}

func (message *Message) getBodyHTML() (string, error) {
	reader, err := message.PropertyContext.GetPropertyReader(4115, message.LocalDescriptors)

	if err != nil {
		return "", err
	}

	switch reader.Property.Type {
	case PropertyTypeString:
		return reader.GetString()
	case PropertyTypeString8:
		if s, err := reader.GetString8(message.internetCodepage()); err == nil {
			return s, nil
		}
		return reader.GetString8(1252)
	default:
		// PtypBinary (and any non-string form): the raw HTML bytes. Treat as
		// UTF-8 when valid (the common case); otherwise decode using the message's
		// declared internet code page, then Windows-1252, rather than emit invalid
		// UTF-8 or drop the body on an unknown code page.
		if err := message.File.checkAllocSize(reader.Size()); err != nil {
			return "", err
		}

		data := make([]byte, reader.Size())

		if _, err := reader.ReadAt(data, 0); err != nil {
			return "", errors.WithStack(err)
		}

		if utf8.Valid(data) {
			return string(data), nil
		}

		if s, err := reader.DecodeString8(data, message.internetCodepage()); err == nil {
			return s, nil
		}
		if s, err := reader.DecodeString8(data, 1252); err == nil {
			return s, nil
		}
		return string(data), nil
	}
}

// internetCodepage returns PidTagInternetCodepage (the code page the body bytes
// are in), defaulting to Windows-1252 when the property is absent, unreadable,
// or non-positive.
func (message *Message) internetCodepage() int {
	reader, err := message.PropertyContext.GetPropertyReader(0x3FDE, message.LocalDescriptors)
	if err != nil {
		return 1252
	}
	cp, err := reader.GetInteger32()
	if err != nil || cp <= 0 {
		return 1252
	}
	return int(cp)
}
