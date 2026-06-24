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

	// Class is the raw PidTagMessageClass (0x001A), e.g. "IPM.Note" or
	// "IPM.Contact.SBE". Empty when the property is absent or unreadable.
	Class string
	// Kind is the message family derived from Class by the classification
	// router (see ClassifyKind). It lets a consumer branch on the message type
	// without re-reading the class property or type-switching on Properties.
	// An unrecognized class is KindUnknown, never silently KindMail.
	Kind MessageKind
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

// Next will ensure that Value returns the next item when executed.
// If the next value is not retrievable, Next will return false and Err() will return the error cause.
func (messageIterator *MessageIterator) Next() bool {
	hasNext := len(messageIterator.messageTableContext.Properties) > messageIterator.currentIndex

	if !hasNext {
		return false
	}

	var currentMessage *Message

	for _, property := range messageIterator.messageTableContext.Properties[messageIterator.currentIndex] {
		// We only return the message identifier in GetMessageTableContext,
		// so we don't need to check the property ID here.
		propertyReader, err := messageIterator.messageTableContext.GetPropertyReader(property)

		if err != nil {
			messageIterator.err = eris.Wrap(err, "failed to get property reader")
			return false
		}

		messageIdentifier, err := propertyReader.GetInteger32()

		if err != nil {
			messageIterator.err = eris.Wrap(err, "failed to get message identifier")
			return false
		}

		message, err := messageIterator.file.GetMessage(Identifier(messageIdentifier))

		if err != nil {
			messageIterator.err = eris.Wrapf(err, "failed to find message: %d", messageIdentifier)
			return false
		}

		currentMessage = message
	}

	messageIterator.currentIndex++
	messageIterator.currentMessage = currentMessage

	return true
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
func (file *File) GetMessage(identifier Identifier) (*Message, error) {
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

	// Read PidTagMessageClass (0x001A). An absent or unreadable class leaves
	// messageClass "", which classify() treats conservatively as KindMail —
	// rather than printing to stdout and guessing, as the library did before.
	// https://learn.microsoft.com/en-us/office/vba/outlook/concepts/forms/item-types-and-message-classes
	messageClass := ""

	if reader, err := propertyContext.GetPropertyReader(26, localDescriptors); err == nil {
		if value, err := reader.GetStringValue(); err == nil {
			messageClass = value
		}
	}

	messageKind, newProps := classify(messageClass)
	messageProperties := newProps()

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
	rtfPropertyReader, err := message.PropertyContext.GetPropertyReader(4105, message.LocalDescriptors)

	if err != nil {
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
