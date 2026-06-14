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

package pst_test

import (
	"os"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"
)

// TestGetRecipients walks the bundled enron fixture and asserts the recipient
// reader extracts per-recipient rows with addresses and recipient types — the
// recall#792 person-resolution upgrade (canonical recipient addresses instead
// of the PidTagDisplayTo/Cc display-string soup).
func TestGetRecipients(t *testing.T) {
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

	var totalRows, withAddress, typedTo int

	walkErr := pstFile.WalkFolders(func(folder *pst.Folder) error {
		it, err := folder.GetMessageIterator()
		if eris.Is(err, pst.ErrMessagesNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		for it.Next() {
			message := it.Value()
			if _, ok := message.Properties.(*properties.Message); !ok {
				continue
			}

			recipients, err := message.GetRecipients()
			if err != nil {
				t.Errorf("GetRecipients: %v", err)
				continue
			}
			for _, r := range recipients {
				totalRows++
				// enron is an Exchange export: addresses land in PidTagEmailAddress
				// (an EX legacyExchangeDN) with PidTagSmtpAddress usually empty.
				if r.EmailAddress != "" || r.SmtpAddress != "" {
					withAddress++
				}
				if r.Type == pst.RecipientTypeTo {
					typedTo++
				}
			}
		}
		return it.Err()
	})
	if walkErr != nil {
		t.Fatalf("walk folders: %v", walkErr)
	}

	if totalRows == 0 {
		t.Fatal("expected recipient rows in the enron fixture, got none")
	}
	if withAddress == 0 {
		t.Errorf("expected recipients with an address, got 0 of %d rows", totalRows)
	}
	if typedTo == 0 {
		t.Errorf("expected at least one To recipient, got 0 of %d rows", totalRows)
	}
	t.Logf("recipient rows=%d withAddress=%d typedTo=%d", totalRows, withAddress, typedTo)
}



// TestRecipientSMTP pins the SMTP() canonicalization: prefer PidTagSmtpAddress,
// fall back to PidTagEmailAddress only when the address type is SMTP, and return
// "" for an Exchange-only (EX legacyExchangeDN) recipient.
func TestRecipientSMTP(t *testing.T) {
	cases := []struct {
		name string
		in   pst.Recipient
		want string
	}{
		{"smtp address present", pst.Recipient{SmtpAddress: "a@b.com", AddressType: "SMTP", EmailAddress: "/O=X/CN=a"}, "a@b.com"},
		{"email is the smtp address", pst.Recipient{AddressType: "SMTP", EmailAddress: "a@b.com"}, "a@b.com"},
		{"exchange-only, no smtp", pst.Recipient{AddressType: "EX", EmailAddress: "/O=X/OU=Y/CN=RECIPIENTS/CN=a"}, ""},
		{"smtp type but no @ is not an address", pst.Recipient{AddressType: "SMTP", EmailAddress: "Adams"}, ""},
		{"empty", pst.Recipient{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.SMTP(); got != tc.want {
				t.Errorf("SMTP() = %q, want %q", got, tc.want)
			}
		})
	}
}
