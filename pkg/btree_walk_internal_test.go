package pst

// Regression tests for the b-tree walk's damage handling
// (private-recall/recall#1752). Before these, WalkAndCreateBTree returned
// nothing: a blank root page (header flushed, pages never written — the
// #1752 archive) opened "successfully" with empty b-trees and failed later
// with a cryptic lookup miss, and a partially damaged tree silently dropped
// the unreachable subtree's mail with no error at all.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeUnicodeBTPage builds one 512-byte Unicode BTPAGE. entries are raw
// entry bytes, each cbEnt long; level 0 is a leaf, >0 a branch; ptype goes
// into the PAGETRAILER (0x81 NBT, 0x80 BBT, 0x00 blank).
func makeUnicodeBTPage(t *testing.T, entries [][]byte, cbEnt byte, level uint8, ptype byte) []byte {
	t.Helper()
	page := make([]byte, 512)
	off := 0
	for _, e := range entries {
		if len(e) != int(cbEnt) {
			t.Fatalf("entry is %d bytes, want cbEnt=%d", len(e), cbEnt)
		}
		copy(page[off:], e)
		off += int(cbEnt)
	}
	page[488] = byte(len(entries))    // cEnt
	page[489] = byte(488 / int(cbEnt)) // cEntMax
	page[490] = cbEnt              // cbEnt
	page[491] = level              // cLevel
	page[496] = ptype              // PAGETRAILER.ptype
	page[497] = ptype              // PAGETRAILER.ptypeRepeat
	return page
}

// unicodeBranchEntry builds a 24-byte Unicode BTENTRY: btkey, BREF{bid, ib}.
func unicodeBranchEntry(key, bid uint64, ib int64) []byte {
	e := make([]byte, 24)
	binary.LittleEndian.PutUint64(e[0:], key)
	binary.LittleEndian.PutUint64(e[8:], bid)
	binary.LittleEndian.PutUint64(e[16:], uint64(ib))
	return e
}

// unicodeLeafNBTEntry builds a 32-byte Unicode NBTENTRY: nid, bidData,
// bidSub, nidParent.
func unicodeLeafNBTEntry(nid, bidData uint64) []byte {
	e := make([]byte, 32)
	binary.LittleEndian.PutUint64(e[0:], nid)
	binary.LittleEndian.PutUint64(e[8:], bidData)
	return e
}

// testFile wraps crafted page bytes as a *File ready for a walk.
func testFile(pages []byte) *File {
	return &File{
		Reader:     NewDefaultReader(bytes.NewReader(pages)),
		FormatType: FormatTypeUnicode,
		Size:       int64(len(pages)),
	}
}

// TestWalkReportsBlankRootPage pins the #1752 archive's exact shape: the
// tree root is an all-zero page. The walk must report it as damage —
// distinctly and actionably — not parse it as an empty, valid tree.
func TestWalkReportsBlankRootPage(t *testing.T) {
	file := testFile(make([]byte, 512)) // one blank page at offset 0
	store := NewBTreeStoreInMemory()

	err := file.WalkAndCreateBTree(0, BTreeTypeNode, store)
	if err == nil {
		t.Fatal("walk of a blank root page reported no error")
	}
	var pageErr *BTreePageTypeError
	if !errors.As(err, &pageErr) {
		t.Fatalf("walk error = %v, want a *BTreePageTypeError", err)
	}
	if pageErr.Got != 0 || pageErr.Want != pageTypeNBT {
		t.Fatalf("page error got=0x%02x want=0x%02x; expected blank (0x00) vs NBT (0x81)", pageErr.Got, pageErr.Want)
	}
	if !strings.Contains(err.Error(), "scanpst") {
		t.Fatalf("error %q does not name the recovery tool (scanpst)", err.Error())
	}
	if store.Len() != 0 {
		t.Fatalf("store has %d entries from a blank page", store.Len())
	}
}

// TestWalkSalvagesSiblingsOnPartialDamage: a branch root with one good and
// one blank child must load the good child's entries AND report the bad one
// — previously the good data loaded and the damage vanished.
func TestWalkSalvagesSiblingsOnPartialDamage(t *testing.T) {
	goodLeaf := makeUnicodeBTPage(t, [][]byte{
		unicodeLeafNBTEntry(97, 1000), // the name-to-id map node, say
		unicodeLeafNBTEntry(290, 1002),
	}, 32, 0, pageTypeNBT)
	blankLeaf := make([]byte, 512)
	root := makeUnicodeBTPage(t, [][]byte{
		unicodeBranchEntry(97, 2000, 512),   // -> goodLeaf
		unicodeBranchEntry(400, 2001, 1024), // -> blankLeaf
	}, 24, 1, pageTypeNBT)

	pages := append(append(append([]byte{}, root...), goodLeaf...), blankLeaf...)
	file := testFile(pages)
	store := NewBTreeStoreInMemory()

	err := file.WalkAndCreateBTree(0, BTreeTypeNode, store)
	if err == nil {
		t.Fatal("partial damage reported no error")
	}
	var walkErr *BTreeWalkError
	if !errors.As(err, &walkErr) {
		t.Fatalf("walk error = %T %v, want *BTreeWalkError", err, err)
	}
	if walkErr.FailureCount != 1 {
		t.Fatalf("FailureCount = %d, want 1", walkErr.FailureCount)
	}
	var pageErr *BTreePageTypeError
	if !errors.As(err, &pageErr) || pageErr.Offset != 1024 {
		t.Fatalf("failure should be the blank page at offset 1024; got %v", err)
	}
	// The salvage: the good sibling's entries are in the store (the branch
	// entries are stored too as part of the walk).
	if _, err := file.GetBTreeNode(97, store); err != nil {
		t.Fatalf("good sibling's entry 97 not salvaged: %v", err)
	}
	if _, err := file.GetBTreeNode(290, store); err != nil {
		t.Fatalf("good sibling's entry 290 not salvaged: %v", err)
	}
}

// TestWalkReportsEntryTableOverrun: a page whose declared entry table
// exceeds the entry region must error, not slice out of bounds.
func TestWalkReportsEntryTableOverrun(t *testing.T) {
	page := makeUnicodeBTPage(t, nil, 32, 0, pageTypeNBT)
	page[488] = 200 // cEnt: 200 * 32 = 6400 bytes >> 488-byte region
	file := testFile(page)
	store := NewBTreeStoreInMemory()

	err := file.WalkAndCreateBTree(0, BTreeTypeNode, store)
	if err == nil {
		t.Fatal("entry-table overrun reported no error")
	}
	if !strings.Contains(err.Error(), "entry region") {
		t.Fatalf("error %q does not describe the overrun", err.Error())
	}
}

// TestWalkReportsWrongTreeType: pointing the node-tree walk at a block-tree
// page (a misdirected header BREF) must be reported as damage.
func TestWalkReportsWrongTreeType(t *testing.T) {
	page := makeUnicodeBTPage(t, nil, 24, 0, pageTypeBBT)
	file := testFile(page)

	err := file.WalkAndCreateBTree(0, BTreeTypeNode, NewBTreeStoreInMemory())
	var pageErr *BTreePageTypeError
	if !errors.As(err, &pageErr) {
		t.Fatalf("walk error = %v, want *BTreePageTypeError", err)
	}
	if pageErr.Got != pageTypeBBT || pageErr.Want != pageTypeNBT {
		t.Fatalf("got=0x%02x want=0x%02x", pageErr.Got, pageErr.Want)
	}
}

// TestNewBlankNodeBTreeRootFailsActionably reproduces the #1752 archive's
// damage on a healthy fixture: zero the 512 bytes the header's NBT BREF
// points at, and New must fail with the actionable damage error instead of
// opening empty and failing later with a bare "failed to find b-tree node".
func TestNewBlankNodeBTreeRootFailsActionably(t *testing.T) {
	src, err := os.ReadFile("../data/enron.pst")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Unicode header ROOT: BREFNBT.ib at offset 224.
	nbtOffset := binary.LittleEndian.Uint64(src[224 : 224+8])
	damaged := append([]byte{}, src...)
	for i := uint64(0); i < 512; i++ {
		damaged[nbtOffset+i] = 0
	}
	path := filepath.Join(t.TempDir(), "blank-nbt-root.pst")
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	_, newErr := New(f)
	if newErr == nil {
		t.Fatal("New succeeded on a blank node b-tree root")
	}
	var pageErr *BTreePageTypeError
	if !errors.As(newErr, &pageErr) {
		t.Fatalf("New error = %v, want to unwrap to *BTreePageTypeError", newErr)
	}
	if !strings.Contains(newErr.Error(), "scanpst") {
		t.Fatalf("New error %q does not name the recovery tool", newErr.Error())
	}
}

// TestHealthyFixturesOpenClean guards the validation against false
// positives: every healthy fixture (Unicode and ANSI — the ANSI trailer
// sits at a different offset) must still open with zero walk errors.
func TestHealthyFixturesOpenClean(t *testing.T) {
	for _, fixture := range []string{"../data/enron.pst", "../data/support.pst", "../data/32-bit.pst"} {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			f, err := os.Open(fixture)
			if err != nil {
				t.Fatalf("open fixture: %v", err)
			}
			defer f.Close()
			pf, err := New(f)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer pf.Cleanup()
			if len(pf.WalkErrors) != 0 {
				t.Fatalf("healthy fixture has WalkErrors: %v", pf.WalkErrors)
			}
		})
	}
}
