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

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestHTTPVerbs(t *testing.T) {

	serverResponse := `{"a":"b"}`
	serverErrResponse := `{"err":"true"}`
	bodyTrimmedMsg := "[Body not dumped; set --debug or JAQ_DEBUG to include it]"

	// Unset home so by default you won't find config files.
	tmpDir, err := ioutil.TempDir("", "testTmp")
	if err != nil {
		t.Fatalf("Failed to setup temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	ioutil.WriteFile(filepath.Join(tmpDir, ".jaq.json"), []byte(`{}`), 0777)
	os.Setenv("HOME", tmpDir)

	type testCase struct {
		desc  string
		args  []string
		setup func()

		pipedInput io.Reader

		expectedOutput    string
		expectedErrOutput string
		expectedErr       error

		// Special case for when you don't want to check exact equality. For
		// instance, logs which have time/host info and make exact comparison
		// more difficult.
		stdErrExpectation func(*testing.T, string)
	}

	// Add basic cases by looping through all the commands; just initalize the
	// tests with the more complex cases.
	testCases := []testCase{}
	for _, cmd := range []*cobra.Command{
		httpCommand(http.MethodGet),
		httpCommand(http.MethodPut),
		httpCommand(http.MethodPost),
		httpCommand(http.MethodDelete),
		httpCommand(http.MethodTrace),
		httpCommand(http.MethodOptions),
	} {
		testCases = append(testCases, testCase{
			desc:           cmd.Use,
			args:           []string{cmd.Use, "/"},
			expectedOutput: serverResponse + "\n",
		})
	}

	testCases = append(testCases, []testCase{
		{
			// HEAD is a special case where we won't get a body back. Currently
			// a newline is appended even though no body is output, consider
			// removing that if it is a problem.
			desc:           "head",
			args:           []string{"head", "/"},
			expectedOutput: "\n",
		}, {
			desc:           "get with headers",
			args:           []string{"get", "/", "--print-headers"},
			expectedOutput: `{"a":"b","jaq-Content-Length":"9"}` + "\n",
		}, {
			desc:           "get with dry-run",
			args:           []string{"get", "/", "--dry-run"},
			expectedOutput: "DRYRUN: jaq get /\n",
		}, {
			desc:           "get with dry-run query and header",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=qVal", "-H", "Hkey=Hval"},
			expectedOutput: `DRYRUN: jaq get / --query qKey=qVal --headers Hkey=Hval` + "\n",
		}, {
			desc:           "get with dry-run multiple values",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=qVal&qKey2=qVal2", "-H", "Hkey=Hval,Hkey2=Hval2"},
			expectedOutput: `DRYRUN: jaq get / --query qKey=qVal&qKey2=qVal2 --headers Hkey=Hval,Hkey2=Hval2` + "\n",
		}, {
			desc:           "get with dry-run repeated flags",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=qVal&qKey2=qVal2", "-H", "Hkey=Hval", "-H", "Hkey2=Hval2"},
			expectedOutput: `DRYRUN: jaq get / --query qKey=qVal&qKey2=qVal2 --headers Hkey=Hval,Hkey2=Hval2` + "\n",
		}, {
			desc: "on-error report",
			args: []string{"get", "/error"},
			setup: func() {
				viper.Set("on-error", "report")
			},
			expectedErrOutput: serverErrResponse + "\n",
		}, {
			desc: "on-error continue",
			args: []string{"get", "/error"},
			setup: func() {
				viper.Set("on-error", "continue")
			},
			expectedOutput: serverErrResponse + "\n",
		}, {
			desc: "on-error silence",
			args: []string{"get", "/error"},
			setup: func() {
				viper.Set("on-error", "silence")
			},
		}, {
			desc: "on-error fatal",
			args: []string{"get", "/error"},
			setup: func() {
				viper.Set("on-error", "fatal")
			},
			expectedErrOutput: serverErrResponse + "\n",
			expectedErr:       errors.New("Unexpected status from response: 404 Not Found"),
		}, {
			desc: "explode",
			args: []string{"get", "/", "--dry-run", "-q", "qKey=$1"},
			setup: func() {
				viper.Set("explode", "true")
			},
			pipedInput:     strings.NewReader(fmt.Sprintf("[%v]", serverResponse)),
			expectedOutput: "DRYRUN: jaq get / --query qKey=" + serverResponse + "\n",
		}, {
			desc: "no explode using viper override",
			args: []string{"get", "/", "--dry-run", "-q", "qKey=$1"},
			setup: func() {
				viper.Set("explode", "false")
			},
			pipedInput:     strings.NewReader(fmt.Sprintf("[%v]", serverResponse)),
			expectedOutput: "DRYRUN: jaq get / --query qKey=[" + serverResponse + "]\n",
		}, {
			desc:           "no explode using flag",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=$1", "--explode=false"},
			pipedInput:     strings.NewReader(fmt.Sprintf("[%v]", serverResponse)),
			expectedOutput: "DRYRUN: jaq get / --query qKey=[" + serverResponse + "]\n",
		}, {
			desc:           "explode using flag",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=$1", "--explode"},
			pipedInput:     strings.NewReader(fmt.Sprintf("[%v]", serverResponse)),
			expectedOutput: "DRYRUN: jaq get / --query qKey=" + serverResponse + "\n",
		}, {
			desc:           "Use desired config",
			args:           []string{"get", "/", "--dry-run", "-q", "qKey=$1", "--config", filepath.Join("testdata", "noExplodeConfig.json")},
			pipedInput:     strings.NewReader(fmt.Sprintf("[%v]", serverResponse)),
			expectedOutput: "DRYRUN: jaq get / --query qKey=[" + serverResponse + "]\n",
			setup: func() {
				os.Setenv("HOME", "testdata")
			},
		}, {
			desc:           "Basic auth",
			args:           []string{"get", "/", "--trace"},
			expectedOutput: serverResponse + "\n",
			setup: func() {
				viper.Set("user", "foo")
				viper.Set("pass", "bar")
				viper.Set("token", "token")
				viper.Set("auth", "basic")
			},
			stdErrExpectation: func(t *testing.T, s string) {
				if strings.Contains(s, "token") {
					t.Errorf("Expected stderr to not include %q but got %q", "token", s)
				}
				if !strings.Contains(s, "Authorization: Basic") {
					t.Errorf("Expected stderr to include %q but got %q", "Authorization: Basic", s)
				}
			},
		}, {
			desc:           "Headers",
			args:           []string{"get", "/", "-H", `FOO=BAR`, "--trace"},
			expectedOutput: serverResponse + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, "Foo: BAR") {
					t.Errorf("Expected stderr to include %q but got: %q", "Foo: BAR", s)
				}
			},
		}, {
			// When a body is present it defaults to sending a chunked request
			// with Content-Length 0. Should be able to manually set the
			// Content-Length to avoid chunking.
			desc:           "Content-Length header",
			args:           []string{"get", "/", "--body", `{"flag":"data"}`, "-H", `Content-Length=15`, "--trace"},
			expectedOutput: serverResponse + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, "Sending request") {
					t.Errorf("Expected stderr to include %q but got: %q", "Sending request", s)
				}
				if !strings.Contains(s, "Got response") {
					t.Errorf("Expected stderr to include %q but got: %q", "Got response", s)
				}
				if !strings.Contains(s, "Content-Length: 15") {
					t.Errorf("Expected stderr to include %q but got: %q", "Content-Length: 15", s)
				}
			},
		}, {
			desc:           "with body",
			args:           []string{"get", "/echo", "--body", `{"flag":"data"}`},
			expectedOutput: `{"flag":"data"}` + "\n",
		}, {
			desc:           "Body gets sent even when debug set",
			args:           []string{"get", "/echo", "--body", `{"flag":"data"}`, "--debug"},
			expectedOutput: `{"flag":"data"}` + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, `{"flag":"data"}`) {
					t.Errorf("Expected stderr to include %q but got: %q", `{"flag":"data"}`, s)
				}
			},
		}, {
			desc:           "File gets sent even when debug set",
			args:           []string{"get", "/echo", "--file", `testdata/testFile.json`, "--debug"},
			expectedOutput: `{"file":"data"}` + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, `{"file":"data"}`) {
					t.Errorf("Expected stderr to include %q but got: %q", `{"flag":"data"}`, s)
				}
			},
		}, {
			desc:           "with filename",
			args:           []string{"get", "/echo", "--file", `testdata/testFile.json`},
			expectedOutput: `{"file":"data"}` + "\n",
		}, {
			desc:           "filename supercedes body",
			args:           []string{"get", "/echo", "--file", `testdata/testFile.json`, "--body", `{"flag":"data"}`},
			expectedOutput: `{"file":"data"}` + "\n",
		}, {
			desc:           "filename supercedes body in dry-run",
			args:           []string{"get", "/echo", "--file", `testdata/testFile.json`, "--body", `{"flag":"data"}`, "--dry-run"},
			expectedOutput: "DRYRUN: jaq get /echo --file testdata/testFile.json\n",
		}, {
			desc:           "body in dry-run",
			args:           []string{"get", "/echo", "--body", `{"flag":"data"}`, "--dry-run"},
			expectedOutput: `DRYRUN: jaq get /echo --body {"flag":"data"}` + "\n",
		}, {
			desc:           "trace",
			args:           []string{"get", "/", "--trace"},
			expectedOutput: serverResponse + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, "Sending request") {
					t.Errorf("Expected stderr to include %q but got: %q", "Sending request", s)
				}
				if !strings.Contains(s, "Got response") {
					t.Errorf("Expected stderr to include %q but got: %q", "Got response", s)
				}
				if !strings.Contains(s, bodyTrimmedMsg) {
					t.Errorf("Expected stderr to include %q but got: %q", bodyTrimmedMsg, s)
				}
			},
		}, {
			desc:           "trace request with dryrun",
			args:           []string{"get", "/", "--dry-run", "--trace"},
			expectedOutput: "DRYRUN: jaq get /\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, "Sending request") {
					t.Errorf("Expected stderr to include %q but got: %q", "Sending request", s)
				}
				if strings.Contains(s, "Got response") {
					t.Errorf("Expected stderr to not include %q but got: %q", "Got response", s)
				}
				if !strings.Contains(s, bodyTrimmedMsg) {
					t.Errorf("Expected stderr to include %q but got: %q", bodyTrimmedMsg, s)
				}
			},
		}, {
			desc:           "debug",
			args:           []string{"get", "/", "--debug"},
			expectedOutput: serverResponse + "\n",
			stdErrExpectation: func(t *testing.T, s string) {
				if !strings.Contains(s, "Sending request") {
					t.Errorf("Expected stderr to include %q but got: %q", "Sending request", s)
				}
				if !strings.Contains(s, "Got response") {
					t.Errorf("Expected stderr to include %q but got: %q", "Got response", s)
				}
				if strings.Contains(s, bodyTrimmedMsg) {
					t.Errorf("Expected stderr to not include %q but got: %q", bodyTrimmedMsg, s)
				}
				if !strings.Contains(s, serverResponse) {
					t.Errorf("Expected stderr to include %q but got: %q", serverResponse, s)
				}
			},
		},
	}...)

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Ensure HOME is reset for the next test.
			defer os.Setenv("HOME", tmpDir)

			h := func(w http.ResponseWriter, req *http.Request) {
				defer req.Body.Close()
				if !strings.EqualFold(req.Method, strings.ToUpper(tc.args[0])) {
					t.Errorf("Expected method %v, got %v", strings.ToUpper(tc.args[0]), req.Method)
				}

				// Disable implicit headers. Content-Length will remain.
				w.Header()["Date"] = nil
				w.Header()["Content-Type"] = nil

				switch req.URL.Path {
				case "/error":
					w.WriteHeader(404)
					w.Write([]byte(serverErrResponse))
				case "/array":
					w.Write([]byte(`[` + serverErrResponse + `]`))
				case "/echo":
					b, err := ioutil.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("Unable to read request body: %v", err)
					}
					w.Write(b)
				default:
					w.Write([]byte(serverResponse))
				}

				return
			}

			s := httptest.NewServer(http.HandlerFunc(h))
			defer s.Close()

			// Reset all viper/flag settings potentially loaded.
			manualInit()
			viper.Set("scheme", "http")
			viper.Set("domain", s.Listener.Addr().String())
			viper.Set("subdomain", "")

			// Test case specific settings such as changing viper/env
			if tc.setup != nil {
				tc.setup()
			}

			stdout, stderr, err := captureOutput(execute, tc.args, tc.pipedInput)

			if stdout != tc.expectedOutput {
				t.Errorf("Expected output %q, got %q", tc.expectedOutput, stdout)
			}
			if stderr != tc.expectedErrOutput && tc.stdErrExpectation == nil {
				t.Errorf("Expected stderr %q, got %q", tc.expectedErrOutput, stderr)
			}
			if tc.stdErrExpectation != nil {
				tc.stdErrExpectation(t, stderr)
			}
			if !reflect.DeepEqual(err, tc.expectedErr) {
				t.Errorf("Expected error: %#v but got: %#v : %v", tc.expectedErr, err, err.Error())
			}
		})
	}
}

// captureOutput runs the given function and returns stdout and stderr as
// strings and the error returned from the function. The function given expects
// an error but only for ease of integration with the cobra library functions we
// intend to use.
func captureOutput(f func([]string, io.Reader) error, args []string, pipeIn io.Reader) (stdout, stderr string, err error) {
	old := os.Stdout
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		log.Println("Error making os.Pipe():", err)
		os.Exit(-1)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		log.Println("Error making os.Pipe():", err)
		os.Exit(-1)
	}

	os.Stdout = w
	os.Stderr = wErr
	// Reset log output for log; otherwise it will go to the original stderr.
	log.SetOutput(wErr)

	err = f(args, pipeIn)

	w.Close()
	wErr.Close()

	os.Stdout = old
	os.Stderr = oldErr
	log.SetOutput(oldErr)

	var buf bytes.Buffer
	var bufErr bytes.Buffer
	io.Copy(&buf, r)
	io.Copy(&bufErr, rErr)
	return buf.String(), bufErr.String(), err
}
