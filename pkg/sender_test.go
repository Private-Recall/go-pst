package pst_test

import (
	"os"
	"strings"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"
)

// TestGetSenderSmtpAddress exercises the canonical-sender-SMTP accessors
// (recall#804): they read PidTagSenderSmtpAddress / PidTagSentRepresentingSmtp-
// Address off the message property context without error, returning "" when the
// property is absent (the common case in the old enron fixture).
func TestGetSenderSmtpAddress(t *testing.T) {
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
		for it.Next() && checked < 50 {
			m := it.Value()
			if _, ok := m.Properties.(*properties.Message); !ok {
				continue
			}
			s, err := m.GetSenderSmtpAddress()
			if err != nil {
				t.Errorf("GetSenderSmtpAddress: %v", err)
			}
			if s != "" && !strings.Contains(s, "@") {
				t.Errorf("sender smtp %q is not an address", s)
			}
			if _, err := m.GetSentRepresentingSmtpAddress(); err != nil {
				t.Errorf("GetSentRepresentingSmtpAddress: %v", err)
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
	t.Logf("checked %d messages", checked)
}
