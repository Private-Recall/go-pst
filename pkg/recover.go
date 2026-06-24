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
	"github.com/rotisserie/eris"
)

// guard runs fn and converts any panic into an error.
//
// go-pst parses untrusted, attacker-influenceable bytes: a truncated or
// malformed archive can drive an index out of range, a bogus declared size into
// a huge make(), or a nil dereference deep in the decode paths. Such a file must
// surface as an error at the library's public boundaries, never as a process
// kill. Wrapping each public entry point in guard lets callers drop the
// defer/recover shim they would otherwise need around every go-pst call.
//
// guard already recovers panics, so the inner errors are returned verbatim; the
// recovered value is wrapped with the boundary name for context. Nested guards
// are harmless — the innermost recovers first and returns an error, so the panic
// never reaches an outer guard.
func guard[T any](boundary string, fn func() (T, error)) (result T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = eris.Errorf("pst: recovered panic in %s: %v", boundary, r)
		}
	}()

	return fn()
}

// guardVoid is guard for boundaries that return only an error.
func guardVoid(boundary string, fn func() error) error {
	_, err := guard(boundary, func() (struct{}, error) {
		return struct{}{}, fn()
	})

	return err
}
