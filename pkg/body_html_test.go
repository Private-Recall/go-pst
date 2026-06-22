package pst_test

import (
	"os"
	"strings"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"
)

// TestGetBodyHTMLRecoversBinaryBody is the regression for the binary-HTML-body
// bug: support.pst stores every HTML body as PidTagHtml (0x1013) in PtypBinary
// form, which the generated properties.Message.GetBodyHtml does not map (its
// msgp key is keyed to the Unicode-string type), so it returns empty. GetBodyHTML
// reads the property directly and must recover all of them.
func TestGetBodyHTMLRecoversBinaryBody(t *testing.T) {
	reader, err := os.Open("../data/support.pst")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	pstFile, err := pst.New(reader)
	if err != nil {
		t.Fatalf("parse pst: %v", err)
	}
	t.Cleanup(func() { pstFile.Cleanup() })

	var binaryHTML, recovered, oldMissed int
	walkErr := pstFile.WalkFolders(func(folder *pst.Folder) error {
		it, err := folder.GetMessageIterator()
		if eris.Is(err, pst.ErrMessagesNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		for it.Next() {
			m := it.Value()
			p, ok := m.Properties.(*properties.Message)
			if !ok {
				continue
			}
			rd, e := m.PropertyContext.GetPropertyReader(4115, m.LocalDescriptors)
			if e != nil || rd.Property.Type != pst.PropertyTypeBinary {
				continue // no HTML body, or a string form handled elsewhere
			}
			binaryHTML++
			html, err := m.GetBodyHTML()
			if err != nil {
				t.Errorf("GetBodyHTML msg %d: %v", m.Identifier, err)
				continue
			}
			if html != "" && strings.Contains(strings.ToLower(html), "<") {
				recovered++
			} else {
				t.Errorf("msg %d: binary HTML not recovered: %.60q", m.Identifier, html)
			}
			if p.GetBodyHtml() == "" {
				oldMissed++ // confirm the generated getter misses the binary form
			}
		}
		return it.Err()
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if binaryHTML == 0 {
		t.Fatal("fixture has no binary HTML bodies — cannot exercise the fix")
	}
	if recovered != binaryHTML {
		t.Errorf("recovered %d of %d binary HTML bodies", recovered, binaryHTML)
	}
	t.Logf("binary HTML bodies=%d recovered=%d (old GetBodyHtml missed=%d)", binaryHTML, recovered, oldMissed)
}

// TestGetBodyHTMLNoBody confirms GetBodyHTML reports a missing HTML body as
// ErrPropertyNotFound (not a spurious value) — enron is plain-text-only mail.
func TestGetBodyHTMLNoBody(t *testing.T) {
	reader, err := os.Open("../data/enron.pst")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	pstFile, err := pst.New(reader)
	if err != nil {
		t.Fatalf("parse pst: %v", err)
	}
	t.Cleanup(func() { pstFile.Cleanup() })

	var checked int
	walkErr := pstFile.WalkFolders(func(folder *pst.Folder) error {
		it, err := folder.GetMessageIterator()
		if eris.Is(err, pst.ErrMessagesNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		for it.Next() && checked < 100 {
			m := it.Value()
			if _, ok := m.Properties.(*properties.Message); !ok {
				continue
			}
			html, err := m.GetBodyHTML()
			if err == nil {
				if html != "" && !strings.Contains(strings.ToLower(html), "<") {
					t.Errorf("msg %d: non-HTML content from GetBodyHTML: %.60q", m.Identifier, html)
				}
			} else if !eris.Is(err, pst.ErrPropertyNotFound) && !eris.Is(err, pst.ErrPropertyNoData) {
				t.Errorf("msg %d: unexpected error: %v", m.Identifier, err)
			}
			checked++
		}
		return it.Err()
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if checked == 0 {
		t.Fatal("no messages checked")
	}
}
