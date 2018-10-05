// Copyright © 2017 John Schnake <schnake.john@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The transform package is meant to only implement the logic necessary to
// convert piped data plus a user command into a set of user comamands including
// references to positional arguments and json fields.
package transform

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestInputToCommands(t *testing.T) {
	testCases := []struct {
		desc          string
		r             io.Reader
		args          []string
		explodeArrays bool
		expectCmds    [][]string
		expectErr     error
	}{
		{
			desc:       "Empty data does not modify cmd",
			r:          bytes.NewBuffer(nil),
			args:       []string{"a b $1"},
			expectCmds: [][]string{[]string{"a b $1"}},
		}, {
			desc:       "Single string",
			r:          bytes.NewBufferString("c"),
			args:       []string{"a b $1"},
			expectCmds: [][]string{[]string{"a b c"}},
		}, {
			desc:       "Single json object",
			r:          bytes.NewBufferString(`{"c":"d"}`),
			args:       []string{"a b $1"},
			expectCmds: [][]string{[]string{`a b {"c":"d"}`}},
		}, {
			desc: "Multiple string object n=1",
			r:    bytes.NewBufferString(`c d`),
			args: []string{"a b $1"},
			expectCmds: [][]string{
				[]string{`a b c`},
				[]string{`a b d`},
			},
		}, {
			desc: "Multiple json object",
			r:    bytes.NewBufferString(`{"c":"d"} {"e":"f"}`),
			args: []string{"a b $1"},
			expectCmds: [][]string{
				[]string{`a b {"c":"d"}`},
				[]string{`a b {"e":"f"}`},
			},
		}, {
			desc: "Multiple string rows object",
			r:    bytes.NewBufferString("c\nd"),
			args: []string{"a b $1"},
			expectCmds: [][]string{
				[]string{`a b c`},
				[]string{`a b d`},
			},
		}, {
			desc: "Multiple json object rows",
			r:    bytes.NewBufferString(`{"c":"d"}` + "\n" + `{"e":"f"}`),
			args: []string{"a b $1"},
			expectCmds: [][]string{
				[]string{`a b {"c":"d"}`},
				[]string{`a b {"e":"f"}`},
			},
		}, {
			desc: "Multiple json object rows and fields",
			r:    bytes.NewBufferString(`{"x":"y"}` + "\n" + `{"x":"z"}`),
			args: []string{"a b ${1.x}"},
			expectCmds: [][]string{
				[]string{`a b y`},
				[]string{`a b z`},
			},
		}, {
			desc: "Invalid json field",
			r:    bytes.NewBufferString(`{"c":"d"}` + "\n" + `{"e":"f"}`),
			args: []string{"a b ${1.zzz}"},
			expectCmds: [][]string{
				[]string{`a b <nil>`},
				[]string{`a b <nil>`},
			},
		}, {
			desc: "Invalid positional reference",
			r:    bytes.NewBufferString(`{"c":"d"}` + "\n" + `{"e":"f"}`),
			args: []string{"a b $3 ${3.zzz}"},
			expectCmds: [][]string{
				[]string{"a b <nil> <nil>"},
				[]string{"a b <nil> <nil>"},
			},
		}, {
			desc: "Multiline JSON",
			r: bytes.NewBufferString(`{
	"a":"c",
	"b":"d"
}`),
			args: []string{"a b ${1.a} ${1.b}"},
			expectCmds: [][]string{
				[]string{"a b c d"},
			},
		}, {
			desc:          "Array of JSON; explode",
			r:             bytes.NewBufferString(`[{"a":"c"},{"a":"d"}]`),
			args:          []string{"a b ${1.a} $1"},
			explodeArrays: true,
			expectCmds: [][]string{
				[]string{`a b c {"a":"c"}`},
				[]string{`a b d {"a":"d"}`},
			},
		}, {
			desc:          "Array of JSON; explode=false",
			r:             bytes.NewBufferString(`[{"a":"c"},{"a":"d"}]`),
			args:          []string{"a b ${1.a} ${1}"},
			explodeArrays: false,
			expectCmds: [][]string{
				[]string{`a b [c d] [{"a":"c"},{"a":"d"}]`},
			},
		}, {
			desc: "Bad positional value",
			r:    bytes.NewBufferString(`{"a":"b"}`),
			args: []string{"$z"},
			expectCmds: [][]string{
				[]string{`<nil>`},
			},
		}, {
			desc: "JSON query of non JSON data",
			r:    bytes.NewBufferString(`a`),
			args: []string{"$z"},
			expectCmds: [][]string{
				[]string{`<nil>`},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			cmds, err := InputToCommands(tc.r, tc.args, tc.explodeArrays)
			if !reflect.DeepEqual(cmds, tc.expectCmds) {
				t.Errorf("Expected %#v got %#v", tc.expectCmds, cmds)
			}

			if !reflect.DeepEqual(err, tc.expectErr) {
				t.Errorf("Expected %#v got %#v", tc.expectErr, err)
			}
		})
	}
}

func TestTruncatedValue(t *testing.T) {
	testCases := []struct {
		desc     string
		i        interface{}
		expected string
	}{
		{
			desc:     "Short value",
			i:        "abc",
			expected: "abc",
		}, {
			desc:     "Long value",
			i:        strings.Repeat("a", 1000),
			expected: fmt.Sprintf("%q...\n[Value truncated]", strings.Repeat("a", 512)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			out := truncatedValue(tc.i)
			if out != tc.expected {
				t.Errorf("Expected %q got %q", tc.expected, out)
			}
		})
	}
}
