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
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rotisserie/eris"
	"io"
)

// GetNodeBTreeOffset returns the file offset to the node b-tree.
func (file *File) GetNodeBTreeOffset() (int64, error) {
	var outputBuffer []byte
	var offset int64

	switch file.FormatType {
	case FormatTypeUnicode:
		outputBuffer = make([]byte, 8)
		offset = 224
	case FormatTypeUnicode4k:
		outputBuffer = make([]byte, 8)
		offset = 224
	case FormatTypeANSI:
		outputBuffer = make([]byte, 4)
		offset = 188
	default:
		return 0, ErrFormatTypeUnsupported
	}

	if _, err := file.Reader.ReadAt(outputBuffer, offset); err != nil {
		return 0, eris.Wrap(err, "failed to read node b-tree offset")
	}

	switch file.FormatType {
	case FormatTypeANSI:
		return int64(binary.LittleEndian.Uint32(outputBuffer)), nil
	default:
		return int64(binary.LittleEndian.Uint64(outputBuffer)), nil
	}
}

// GetBlockBTreeOffset returns the file offset to the block b-tree.
func (file *File) GetBlockBTreeOffset() (int64, error) {
	var outputBuffer []byte
	var offset int64

	switch file.FormatType {
	case FormatTypeUnicode:
		outputBuffer = make([]byte, 8)
		offset = 240
	case FormatTypeUnicode4k:
		outputBuffer = make([]byte, 8)
		offset = 240
	case FormatTypeANSI:
		outputBuffer = make([]byte, 4)
		offset = 196
	default:
		return 0, ErrFormatTypeUnsupported
	}

	if _, err := file.Reader.ReadAt(outputBuffer, offset); err != nil {
		return 0, eris.Wrap(err, "failed to read block b-tree offset")
	}

	switch file.FormatType {
	case FormatTypeANSI:
		return int64(binary.LittleEndian.Uint32(outputBuffer)), nil
	default:
		return int64(binary.LittleEndian.Uint64(outputBuffer)), nil
	}
}

// GetBTreeNodeEntryCount returns the amount of entries in the b-tree.
func (file *File) GetBTreeNodeEntryCount(btreeNode []byte) uint16 {
	switch file.FormatType {
	case FormatTypeUnicode:
		return uint16(btreeNode[488])
	case FormatTypeUnicode4k:
		return binary.LittleEndian.Uint16(btreeNode[4056:4058]) // TODO - CHeck
	case FormatTypeANSI:
		return uint16(btreeNode[496])
	default:
		panic(ErrFormatTypeUnsupported)
	}
}

// GetBTreeNodeEntrySize returns the size of an entry in the b-tree.
func (file *File) GetBTreeNodeEntrySize(btreeNode []byte) uint8 {
	switch file.FormatType {
	case FormatTypeUnicode:
		return btreeNode[490]
	case FormatTypeUnicode4k:
		return btreeNode[4060]
	case FormatTypeANSI:
		return btreeNode[498]
	default:
		panic(ErrFormatTypeUnsupported)
	}
}

// GetBTreeNodeLevel returns the level of the b-tree node.
// References "The node and block b-tree".
func (file *File) GetBTreeNodeLevel(btreeNode []byte) uint8 {
	switch file.FormatType {
	case FormatTypeUnicode:
		return btreeNode[491]
	case FormatTypeUnicode4k:
		return btreeNode[4062] // TODO - Check
	case FormatTypeANSI:
		return btreeNode[499]
	default:
		panic(ErrFormatTypeUnsupported)
	}
}

// BTreeNode represents an entry in a b-tree node.
type BTreeNode struct {
	// Identifier is only unique to the node level.
	Identifier                 Identifier `json:"identifier"`
	FileOffset                 int64      `json:"fileOffset"`
	DataIdentifier             Identifier `json:"dataIdentifier"`
	LocalDescriptorsIdentifier Identifier `json:"localDescriptorsIdentifier"`
	Size                       uint16     `json:"size"`
	NodeLevel                  uint8      `json:"nodeLevel"`
}

// NewBTreeNodeReader is used by the Heap-on-Node.
func NewBTreeNodeReader(btreeNode BTreeNode, reader Reader) *io.SectionReader {
	return io.NewSectionReader(reader, btreeNode.FileOffset, int64(btreeNode.Size))
}

// BTreeNodeLessFunc tests whether the current node is less than the given argument.
//
// This must provide a strict weak ordering.
// If !a.Less(b) && !b.Less(a), we treat this to mean a == b (we can only hold one of either a or b in the tree).
func BTreeNodeLessFunc(a BTreeNode, b BTreeNode) bool {
	if a.Identifier == b.Identifier {
		// We don't return the first identifier because there may be two or more nodes with
		// the same identifier, so prefer leaf nodes (which there is always only one of).
		return a.NodeLevel < b.NodeLevel
	} else {
		return a.Identifier < b.Identifier
	}
}

// GetBTreeNodeRawEntries returns the raw b-tree node entries in bytes.
// References https://github.com/mooijtech/go-pst/blob/master/docs/README.md#btpage
// Used by GetBTreeNodeEntries.
func (file *File) GetBTreeNodeRawEntries(btreeNodeOffset int64, callback func([]byte, error)) {
	var outputBuffer []byte

	switch file.FormatType {
	case FormatTypeUnicode:
		outputBuffer = make([]byte, 512)
	case FormatTypeUnicode4k:
		outputBuffer = make([]byte, 4056) // TODO - Check
	case FormatTypeANSI:
		outputBuffer = make([]byte, 512)
	default:
		callback(nil, ErrFormatTypeUnsupported)
	}

	if _, err := file.Reader.ReadAtAsync(outputBuffer, uint64(btreeNodeOffset), func(err error) {
		callback(outputBuffer, err)
	}); err != nil {
		callback(nil, err)
	}
}

// GetBTreeNodeEntries returns the entries in the b-tree node.
func (file *File) GetBTreeNodeEntries(btreeNodeOffset int64, btreeType BTreeType, callback func(btreeNodeEntries []BTreeNode, nodeLevel uint8, err error)) {
	parentBTreeNodeLevel, err := file.GetParentBTreeNodeLevel(btreeNodeOffset)

	if err != nil {
		callback(nil, 0, errors.WithStack(err))
		return
	}

	file.GetBTreeNodeRawEntries(btreeNodeOffset, func(btreeNodeEntry []byte, err error) {
		if err != nil {
			callback(nil, 0, err)
			return
		}

		// A page whose trailer doesn't identify it as a page of this b-tree
		// is damage (a blank page, or a header pointing at something else) —
		// report it instead of parsing garbage as entries.
		if err := file.validateBTreePage(btreeNodeEntry, btreeType, btreeNodeOffset); err != nil {
			callback(nil, 0, err)
			return
		}

		btreeNodeEntryCount := file.GetBTreeNodeEntryCount(btreeNodeEntry)
		//btreeNodeLevel := file.GetBTreeNodeLevel(btreeNodeEntry) // Of children.
		btreeNodeEntrySize := file.GetBTreeNodeEntrySize(btreeNodeEntry)
		btreeNodeEntries := make([]BTreeNode, btreeNodeEntryCount)

		for i := 0; i < int(btreeNodeEntryCount); i++ {
			btreeNodeEntryData := btreeNodeEntry[i*int(btreeNodeEntrySize) : (i*int(btreeNodeEntrySize))+int(btreeNodeEntrySize)]

			if parentBTreeNodeLevel > 0 && (btreeType == BTreeTypeNode || btreeType == BTreeTypeBlock) {
				// Branch node or block b-tree node.
				btreeNodeEntries[i] = BTreeNode{
					Identifier: GetBTreeNodeEntryIdentifier(btreeNodeEntryData, file.FormatType),
					FileOffset: GetBTreeNodeEntryFileOffset(btreeNodeEntryData, true, file.FormatType),
					NodeLevel:  parentBTreeNodeLevel,
				}
			} else if parentBTreeNodeLevel == 0 && btreeType == BTreeTypeNode {
				// Leaf node b-tree node.
				btreeNodeEntries[i] = BTreeNode{
					Identifier:                 GetBTreeNodeEntryIdentifier(btreeNodeEntryData, file.FormatType),
					DataIdentifier:             GetBTreeNodeEntryDataIdentifier(btreeNodeEntryData, file.FormatType),
					LocalDescriptorsIdentifier: GetBTreeNodeEntryLocalDescriptorsIdentifier(btreeNodeEntryData, file.FormatType),
					NodeLevel:                  parentBTreeNodeLevel,
				}
			} else if parentBTreeNodeLevel == 0 && btreeType == BTreeTypeBlock {
				// Leaf block b-tree node.
				btreeNodeEntries[i] = BTreeNode{
					Identifier: GetBTreeNodeEntryIdentifier(btreeNodeEntryData, file.FormatType),
					FileOffset: GetBTreeNodeEntryFileOffset(btreeNodeEntryData, false, file.FormatType),
					Size:       GetBTreeNodeEntrySize(btreeNodeEntryData, file.FormatType),
					NodeLevel:  parentBTreeNodeLevel,
				}
			}
		}

		callback(btreeNodeEntries, parentBTreeNodeLevel, nil)
	})
}

// GetBTreeNodeEntryIdentifier returns the Identifier of this b-tree node entry.
// References "The b-tree entries".
func GetBTreeNodeEntryIdentifier(btreeNodeEntryData []byte, formatType FormatType) Identifier {
	return GetIdentifierFromBytes(btreeNodeEntryData[:GetIdentifierSize(formatType)], formatType)
}

// GetIdentifierFromBytes returns the Identifier type from bytes.
func GetIdentifierFromBytes(identifierBytes []byte, formatType FormatType) Identifier {
	switch formatType {
	case FormatTypeANSI:
		return Identifier(binary.LittleEndian.Uint32(identifierBytes))
	default:
		// TODO - Reference [MS-PDF] that this is actually 32-bit
		return Identifier(binary.LittleEndian.Uint32(identifierBytes))
	}
}

// GetIdentifierSize returns the size of an Identifier.
func GetIdentifierSize(formatType FormatType) uint8 {
	switch formatType {
	case FormatTypeANSI:
		return 4
	default:
		return 8
	}
}

// Identifier represents a b-tree node identifier.
// TODO - Document the int types per use case and use separate types.
type Identifier int64

// Constants defining the special b-tree node identifiers.
const (
	IdentifierRootFolder   Identifier = 290
	IdentifierMessageStore Identifier = 33
	IdentifierNameToIDMap  Identifier = 97
)

// GetType returns the IdentifierType of this Identifier.
func (identifier Identifier) GetType() IdentifierType {
	// Bit-masking:
	// Use bitwise ANDing in order to extract a subset of the bits in the value.
	// 11111 (binary) = 0x1F (hex), which with bitwise ANDing extracts the first 5 bits.
	// See: https://www.rapidtables.com/convert/number/binary-to-hex.html
	return IdentifierType(identifier & 0x1F)
}

// IdentifierType represents the type of Identifier.
type IdentifierType uint8

// Constants defining the identifier types.
// References "Identifier types".
const (
	IdentifierTypeHID                     IdentifierType = 0
	IdentifierTypeInternal                IdentifierType = 1
	IdentifierTypeNormalFolder            IdentifierType = 2
	IdentifierTypeSearchFolder            IdentifierType = 3
	IdentifierTypeNormalMessage           IdentifierType = 4
	IdentifierTypeAttachment              IdentifierType = 5
	IdentifierTypeSearchUpdateQueue       IdentifierType = 6
	IdentifierTypeSearchCriteriaObject    IdentifierType = 7
	IdentifierTypeAssociatedMessage       IdentifierType = 8
	IdentifierTypeContentsTableIndex      IdentifierType = 10
	IdentifierTypeReceiveFolderTable      IdentifierType = 11
	IdentifierTypeOutgoingQueueTable      IdentifierType = 12
	IdentifierTypeHierarchyTable          IdentifierType = 13
	IdentifierTypeContentsTable           IdentifierType = 14
	IdentifierTypeAssociatedContentsTable IdentifierType = 15
	IdentifierTypeSearchContentsTable     IdentifierType = 16
	IdentifierTypeAttachmentTable         IdentifierType = 17
	IdentifierTypeRecipientTable          IdentifierType = 18
	IdentifierTypeSearchTableIndex        IdentifierType = 19
	IdentifierTypeLTP                     IdentifierType = 31
)

// GetBTreeNodeEntryFileOffset returns the file offset for this b-tree branch or leaf node.
// References "The b-tree entries".
func GetBTreeNodeEntryFileOffset(btreeNodeEntryData []byte, isBranchNode bool, formatType FormatType) int64 {
	if isBranchNode {
		switch formatType {
		case FormatTypeANSI:
			return int64(binary.LittleEndian.Uint32(btreeNodeEntryData[8 : 8+4]))
		default:
			return int64(binary.LittleEndian.Uint64(btreeNodeEntryData[16 : 16+8]))
		}
	} else {
		switch formatType {
		case FormatTypeANSI:
			return int64(binary.LittleEndian.Uint32(btreeNodeEntryData[4 : 4+4]))
		default:
			return int64(binary.LittleEndian.Uint64(btreeNodeEntryData[8 : 8+8]))
		}
	}
}

// GetBTreeNodeEntryDataIdentifier returns the node identifier of the data (in the block b-tree).
// References "The b-tree entries".
func GetBTreeNodeEntryDataIdentifier(btreeNodeEntryData []byte, formatType FormatType) Identifier {
	switch formatType {
	case FormatTypeANSI:
		return GetIdentifierFromBytes(btreeNodeEntryData[4:4+GetIdentifierSize(formatType)], formatType)
	default:
		return GetIdentifierFromBytes(btreeNodeEntryData[8:8+GetIdentifierSize(formatType)], formatType)
	}
}

// GetBTreeNodeEntryLocalDescriptorsIdentifier returns the identifier to the local descriptors in the block b-tree.
func GetBTreeNodeEntryLocalDescriptorsIdentifier(btreeNodeEntryData []byte, formatType FormatType) Identifier {
	switch formatType {
	case FormatTypeANSI:
		return GetIdentifierFromBytes(btreeNodeEntryData[8:8+GetIdentifierSize(formatType)], formatType)
	default:
		return GetIdentifierFromBytes(btreeNodeEntryData[16:16+GetIdentifierSize(formatType)], formatType)
	}
}

// GetBTreeNodeEntrySize returns the size of the data in the block b-tree leaf node entry.
// References "The b-tree entries".
func GetBTreeNodeEntrySize(btreeNodeEntryData []byte, formatType FormatType) uint16 {
	switch formatType {
	case FormatTypeANSI:
		return binary.LittleEndian.Uint16(btreeNodeEntryData[8 : 8+2])
	default:
		return binary.LittleEndian.Uint16(btreeNodeEntryData[16 : 16+2])
	}
}

// BTreeType represents either the node b-tree or block b-tree.
type BTreeType uint8

// Constants defining the b-tree types.
const (
	BTreeTypeNode BTreeType = iota
	BTreeTypeBlock
)

// GetParentBTreeNodeLevel returns the level of the b-tree node.
// References "The node and block b-tree".
func (file *File) GetParentBTreeNodeLevel(btreeNodeOffset int64) (uint8, error) {
	outputBuffer := make([]byte, 1)
	var offset int64

	switch file.FormatType {
	case FormatTypeUnicode:
		offset = btreeNodeOffset + 491
	case FormatTypeUnicode4k:
		offset = btreeNodeOffset + 4061
	case FormatTypeANSI:
		offset = btreeNodeOffset + 499
	default:
		return 0, errors.WithStack(ErrFormatTypeUnsupported)
	}

	if _, err := file.Reader.ReadAt(outputBuffer, offset); err != nil {
		return 0, errors.WithStack(err)
	}

	return outputBuffer[0], nil
}

// pageTypeNBT and pageTypeBBT are the PAGETRAILER ptype values identifying
// node and block b-tree pages. References [MS-PST] "PAGETRAILER".
const (
	pageTypeNBT byte = 0x81
	pageTypeBBT byte = 0x80
)

// BTreePageTypeError reports a page referenced as part of a b-tree whose
// trailer does not identify it as one. The archetypal case is an all-zero
// page (Got == 0): the header points at a b-tree root that was never
// flushed — Outlook crashed between the header write and the page write —
// so the whole tree is unreachable even though the data blocks usually
// still exist. Outlook's scanpst.exe rebuilds exactly this damage.
type BTreePageTypeError struct {
	Offset int64
	Got    byte
	Want   byte
}

func (e *BTreePageTypeError) Error() string {
	if e.Got == 0 {
		return fmt.Sprintf("go-pst: b-tree page at offset %d is blank (page type 0x00, want 0x%02x): the file is damaged (Outlook's scanpst.exe can usually rebuild it)", e.Offset, e.Want)
	}
	return fmt.Sprintf("go-pst: page at offset %d is not a b-tree page (page type 0x%02x, want 0x%02x): the file is damaged (Outlook's scanpst.exe can usually rebuild it)", e.Offset, e.Got, e.Want)
}

// validateBTreePage checks that a raw b-tree page's trailer identifies it as
// a page of the expected b-tree and that its declared entry table fits the
// page's entry region. Both checks catch damage the entry parser would
// otherwise misread as (or crash on) entries. The Unicode4k trailer is not
// modeled (GetBTreeNodeRawEntries doesn't even read those bytes), so 4k
// pages skip validation rather than fail it.
func (file *File) validateBTreePage(page []byte, btreeType BTreeType, offset int64) error {
	var ptypeOffset, entryRegion int

	switch file.FormatType {
	case FormatTypeUnicode:
		ptypeOffset, entryRegion = 496, 488
	case FormatTypeANSI:
		ptypeOffset, entryRegion = 500, 496
	default:
		return nil
	}

	want := pageTypeNBT
	if btreeType == BTreeTypeBlock {
		want = pageTypeBBT
	}

	if got := page[ptypeOffset]; got != want {
		return errors.WithStack(&BTreePageTypeError{Offset: offset, Got: got, Want: want})
	}

	entryCount := int(file.GetBTreeNodeEntryCount(page))
	entrySize := int(file.GetBTreeNodeEntrySize(page))

	if entryCount > 0 && (entrySize == 0 || entryCount*entrySize > entryRegion) {
		return errors.WithStack(fmt.Errorf("go-pst: b-tree page at offset %d declares %d entries of %d bytes, exceeding the %d-byte entry region: the file is damaged", offset, entryCount, entrySize, entryRegion))
	}

	return nil
}

// maxBTreeDepth bounds the b-tree walk recursion. Real PST trees are 3-4
// levels; anything deeper is a corrupt or malicious file cycling through
// branch pages.
const maxBTreeDepth = 64

// BTreeWalkError aggregates what a b-tree walk could not read. The walk
// salvages: a failed subtree is reported here while its siblings still
// load, so the store holds every reachable entry. Callers deciding whether
// the file is usable should combine this with the store's Len — a non-empty
// store plus a BTreeWalkError means PARTIAL coverage (some folders/messages
// silently unreachable), which completeness-sensitive callers (ingest
// pipelines) must surface rather than swallow.
type BTreeWalkError struct {
	// FailureCount is how many subtree reads failed; Failures keeps the
	// first few for diagnosis.
	FailureCount int
	Failures     []error
	// Conflicts is how many entries were skipped because an entry with the
	// same identity was already in the store (duplicate entries are
	// corruption; the first-loaded entry wins).
	Conflicts int
}

func (e *BTreeWalkError) Error() string {
	msg := fmt.Sprintf("go-pst: b-tree walk: %d unreadable subtree(s), %d duplicate entrie(s)", e.FailureCount, e.Conflicts)
	if len(e.Failures) > 0 {
		msg += ": " + e.Failures[0].Error()
	}
	return msg
}

// Unwrap exposes the recorded failures to errors.Is/As.
func (e *BTreeWalkError) Unwrap() []error { return e.Failures }

// maxRecordedWalkFailures caps BTreeWalkError.Failures so a massively
// corrupt file doesn't accumulate unbounded error values; FailureCount
// still counts them all.
const maxRecordedWalkFailures = 8

// WalkAndCreateBTree walks the b-tree and updates the given b-tree store.
//
// The walk is salvaging: an unreadable or invalid page fails only its own
// subtree, siblings still load. It returns nil when the whole tree loaded,
// or a *BTreeWalkError describing what was skipped. (It previously returned
// nothing and silently dropped unreadable subtrees — a partially damaged
// archive iterated as if whole, with the unreachable mail silently absent;
// see private-recall/recall#1752.)
func (file *File) WalkAndCreateBTree(btreeOffset int64, btreeType BTreeType, btreeStore BTreeStore) error {
	walk := &btreeWalk{file: file, btreeType: btreeType, store: btreeStore}
	walk.walk(btreeOffset, 0)

	if walk.err.FailureCount == 0 && walk.err.Conflicts == 0 {
		return nil
	}
	return &walk.err
}

// btreeWalk carries one WalkAndCreateBTree traversal's state.
type btreeWalk struct {
	file      *File
	btreeType BTreeType
	store     BTreeStore
	err       BTreeWalkError
}

func (w *btreeWalk) fail(err error) {
	w.err.FailureCount++
	if len(w.err.Failures) < maxRecordedWalkFailures {
		w.err.Failures = append(w.err.Failures, err)
	}
}

func (w *btreeWalk) walk(offset int64, depth int) {
	if depth > maxBTreeDepth {
		w.fail(fmt.Errorf("go-pst: b-tree deeper than %d levels at offset %d: corrupt tree", maxBTreeDepth, offset))
		return
	}

	w.file.GetBTreeNodeEntries(offset, w.btreeType, func(nodeEntries []BTreeNode, nodeLevel uint8, err error) {
		if err != nil {
			w.fail(err)
			return
		}

		if nodeLevel > 0 {
			// Branch node entries.
			for i := 0; i < len(nodeEntries); i++ {
				nodeEntry := nodeEntries[i]

				if _, exists := w.store.Load(nodeEntry); exists {
					w.err.Conflicts++
					continue
				}
				if nodeEntry.FileOffset <= 0 || (w.file.Size > 0 && nodeEntry.FileOffset >= w.file.Size) {
					w.fail(fmt.Errorf("go-pst: b-tree branch entry %d points outside the file (offset %d, file size %d)", nodeEntry.Identifier, nodeEntry.FileOffset, w.file.Size))
					continue
				}

				w.walk(nodeEntry.FileOffset, depth+1)
			}
		} else {
			// Leaf node entries
			for i := 0; i < len(nodeEntries); i++ {
				if _, exists := w.store.Load(nodeEntries[i]); exists {
					w.err.Conflicts++
				}
			}
		}
	})
}

// GetBTreeNode returns the node in the node or block b-tree with the given identifier.
func (file *File) GetBTreeNode(identifier Identifier, btreeStore BTreeStore) (BTreeNode, error) {
	btreeNode, found := btreeStore.Get(BTreeNode{Identifier: identifier})

	if !found {
		return BTreeNode{}, ErrBTreeNodeNotFound
	}

	return btreeNode, nil
}

// GetNodeBTreeNode returns the node with the given identifier in the node b-tree.
func (file *File) GetNodeBTreeNode(identifier Identifier) (BTreeNode, error) {
	return file.GetBTreeNode(identifier, file.NodeBTree)
}

// GetBlockBTreeNode returns the node with the given identifier in the block b-tree.
func (file *File) GetBlockBTreeNode(identifier Identifier) (BTreeNode, error) {
	// Clear the least significant bit (LSB), which is reserved, but sometimes set.
	return file.GetBTreeNode(identifier&0xfffffffe, file.BlockBTree)
}

// GetDataBTreeNode searches the identifier in the node b-tree, then searches the data identifier in the block b-tree.
func (file *File) GetDataBTreeNode(identifier Identifier) (BTreeNode, error) {
	nodeBTreeNode, err := file.GetNodeBTreeNode(identifier)

	if err != nil {
		return BTreeNode{}, eris.Wrap(err, "failed to get b-tree node")
	}

	return file.GetBlockBTreeNode(nodeBTreeNode.DataIdentifier)
}
