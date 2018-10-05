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
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffail/gabs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	headerPrefix = "jaq-"
)

// config is struct to hold all the values expected from command-line flags, env
// vars, and config values.
type config struct {
	commandPath               string
	scheme, subdomain, domain string
	verb                      string
	query                     string
	headers                   []string
	auth                      string
	trace, debug              bool
	filepath                  string
	body                      string
	dryRun                    bool
	explode                   bool
	printHeaders              bool
	requestTimeout            int
	user, pass, token         string
	onError                   string
}

// httpCommand is a generator of *cobra.Commands which only differ by their HTTP
// verbs.
func httpCommand(httpVerb string) *cobra.Command {
	return &cobra.Command{
		Use:   strings.ToLower(httpVerb),
		Short: fmt.Sprintf("Perform a %s operation to the given URL.", strings.ToUpper(httpVerb)),
		Args:  cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the configuration now; if done outside of this command, flags
			// will not have been parsed yet.
			conf, err := newConfig(cmd)
			if err != nil {
				return err
			}

			if err := httpRun(conf, httpVerb, args[0]); err != nil {
				return err
			}

			return nil
		},
	}
}

// httpRun is the shared logic of all the HTTP commands but has configuration
// and input transformation logic extracted.
func httpRun(conf config, verb string, path string) error {
	req, err := newRequest(conf, path)
	if err != nil {
		return err
	}

	resp, err := response(conf, req)
	if err != nil {
		return err
	}

	return processResponse(conf, resp)
}

func processResponse(conf config, resp *http.Response) error {
	if resp == nil {
		return nil
	}

	defer resp.Body.Close()
	var copyHeaders http.Header
	if conf.printHeaders {
		copyHeaders = resp.Header
	}

	if resp.StatusCode < 400 {
		if _, err := copyNewline(os.Stdout, resp.Body, copyHeaders); err != nil {
			return err
		}
	} else {
		switch conf.onError {
		case "silence":
		case "fatal":
			if _, err := copyNewline(os.Stderr, resp.Body, copyHeaders); err != nil {
				return err
			}
			return fmt.Errorf("Unexpected status from response: %v", resp.Status)
		case "continue":
			if _, err := copyNewline(os.Stdout, resp.Body, copyHeaders); err != nil {
				return err
			}
		case "report":
			if _, err := copyNewline(os.Stderr, resp.Body, copyHeaders); err != nil {
				return err
			}
		default:
			if _, err := copyNewline(os.Stdout, resp.Body, copyHeaders); err != nil {
				return err
			}
		}
	}

	return nil
}

// response runs the request with the given configuration. The request is not
// modified. If trace/debug are set the request/responses are logged. If dryrun
// is set then the request is not actually executed.
func response(conf config, req *http.Request) (*http.Response, error) {
	c := &http.Client{
		Timeout: time.Duration(conf.requestTimeout) * time.Second,
	}

	if conf.trace || conf.debug {
		dump, err := httputil.DumpRequestOut(req, conf.debug)
		if err != nil {
			log.Println("Unable to dump request out:", err)
		}

		bodyMsg := ""
		if !conf.debug {
			bodyMsg = "\n[Body not dumped; set --debug or JAQ_DEBUG to include it]"
		}
		log.Printf("Sending request: %v%v", string(dump), bodyMsg)
	}

	if conf.dryRun {
		// Flags get stripped from args; add back the ones relevent to
		// the actual request.
		display := bytes.NewBufferString(conf.commandPath + " " + req.URL.Path)

		if len(req.URL.RawQuery) > 0 {
			display.WriteString(" --query ")
			display.WriteString(req.URL.RawQuery)
		}

		if len(conf.headers) > 0 {
			display.WriteString(" --headers ")
			display.WriteString(strings.Join(conf.headers, ","))
		}

		switch {
		case len(conf.filepath) > 0:
			display.WriteString(" --file ")
			display.WriteString(conf.filepath)
		case len(conf.body) > 0:
			display.WriteString(" --body ")
			display.WriteString(conf.body)
		}

		fmt.Println("DRYRUN: " + display.String())
		return nil, nil
	}

	resp, err := c.Do(req)
	if err != nil {
		return resp, err
	}

	if conf.trace || conf.debug {
		dump, err := httputil.DumpResponse(resp, conf.debug)
		if err != nil {
			log.Println("Unable to dump request out:", err)
		}
		bodyMsg := ""
		if !conf.debug {
			bodyMsg = "\n[Body not dumped; set --debug or JAQ_DEBUG to include it]"
		}
		log.Printf("Got response: %v%v", string(dump), bodyMsg)
	}

	return resp, nil
}

// newRequest creates an *http.Request from the configuration.
func newRequest(conf config, path string) (*http.Request, error) {
	apiURL, err := getURL(conf.scheme, conf.subdomain, conf.domain)
	if err != nil {
		return nil, fmt.Errorf("Unable to properly form URL from configuration: %v", err)
	}

	var body io.Reader

	// File contents supercedes json body.
	if conf.filepath != "" {
		body, err = os.Open(conf.filepath)
		if err != nil {
			return nil, err
		}
	} else {
		if len(conf.body) > 0 {
			body = bytes.NewBufferString(conf.body)
		}
	}

	req, err := http.NewRequest(conf.verb, apiURL.String(), body)
	if err != nil {
		return nil, err
	}

	req.URL.Path = path
	req.URL.RawQuery = conf.query

	// Set auth here so that the user can overwrite it if desired.
	switch conf.auth {
	case "token":
		req.Header.Set("Authorization", "Bearer "+conf.token)
	case "basic":
		req.SetBasicAuth(conf.user, conf.pass)
	}

	for _, h := range conf.headers {
		hParts := strings.SplitN(h, "=", 2)
		if len(hParts) != 2 {
			return nil, fmt.Errorf("invalid header: %q, expected comma-separated values of the form KEY=VALUE", h)
		}

		switch hParts[0] {
		case "Content-Length":
			length, err := strconv.Atoi(hParts[1])
			if err != nil {
				log.Printf("Unable to parse header Content-Length as an integer; have %q", hParts[1])
			}
			req.ContentLength = int64(length)
		case "Transfer-Encoding":
			// Note: I don't think anyone will have much reason to change this
			// and it's quite hard to test since it doesn't get dumped via
			// httputil.DumpRequest which is meant for clients.
			// See https://github.com/golang/go/issues/28026
			if len(req.TransferEncoding) == 0 {
				req.TransferEncoding = []string{}
			}
			encodings := strings.Split(hParts[1], ",")
			req.TransferEncoding = append(req.TransferEncoding, encodings...)
		default:
			req.Header.Set(hParts[0], hParts[1])
		}
	}

	return req, nil
}

// copyNewline does an io.Copy but follows it up by adding a newline so that
// output from muliple commands will not be on the same line. It adds the given
// headers to the json with the prefix "jaq-"
func copyNewline(w io.Writer, r io.Reader, copyHeaders http.Header) (n int64, err error) {
	if len(copyHeaders) > 0 {
		// First put into a buffer and read as json. Then add new fields.
		b := bytes.NewBuffer(nil)

		if n, err = io.Copy(b, r); err != nil {
			return
		}
		jsonObj, err := gabs.ParseJSON(b.Bytes())
		if err != nil {
			// Report errors adding headers but don't fail.
			log.Printf("Error parsing json from response. Unable to add header information: %v", err)
		} else {
			for header := range copyHeaders {
				headerKey := fmt.Sprintf("%v%v", headerPrefix, header)
				if _, err := jsonObj.Set(copyHeaders.Get(header), headerKey); err != nil {
					log.Printf("Error setting header field to json object: %v: %v", headerKey, copyHeaders.Get(header))
				}
			}
			b.Truncate(0)
			fmt.Fprintf(b, jsonObj.String())
			r = b
		}
	}

	if n, err = io.Copy(w, r); err != nil {
		return
	}
	if _, err = io.WriteString(w, "\n"); err != nil {
		return
	}
	return
}

func init() {
	ResetSettingsHTTPVerbs()
}

func ResetSettingsHTTPVerbs() {
	for _, cmd := range []*cobra.Command{
		httpCommand(http.MethodGet),
		httpCommand(http.MethodPut),
		httpCommand(http.MethodPost),
		httpCommand(http.MethodHead),
		httpCommand(http.MethodDelete),
		httpCommand(http.MethodTrace),
		httpCommand(http.MethodOptions),
	} {
		RootCmd.AddCommand(cmd)
	}
}

func getURL(scheme, subdomain, domain string) (*url.URL, error) {
	uStr := ""
	if subdomain == "" {
		uStr = fmt.Sprintf("%s://%s", scheme, domain)
	} else {
		uStr = fmt.Sprintf("%s://%s.%s", scheme, subdomain, domain)
	}
	return url.Parse(uStr)
}

func newConfig(cmd *cobra.Command) (config, error) {
	c := config{
		commandPath:    cmd.CommandPath(),
		requestTimeout: viper.GetInt("request_timeout"),
		scheme:         viper.GetString("scheme"),
		subdomain:      viper.GetString("subdomain"),
		domain:         viper.GetString("domain"),
		auth:           viper.GetString("auth"),
		user:           viper.GetString("user"),
		pass:           viper.GetString("pass"),
		token:          viper.GetString("token"),
		onError:        viper.GetString("on-error"),
		printHeaders:   viper.GetBool("print-headers"),
		dryRun:         viper.GetBool("dry-run"),
		trace:          viper.GetBool("trace"),
		debug:          viper.GetBool("debug"),
		verb:           strings.ToUpper(cmd.Use),
	}

	var err error
	c.query, err = cmd.Flags().GetString("query")
	if err != nil {
		return c, err
	}

	c.body, err = cmd.Flags().GetString("body")
	if err != nil {
		return c, err
	}

	c.filepath, err = cmd.Flags().GetString("file")
	if err != nil {
		return c, err
	}

	c.headers, err = cmd.Flags().GetStringSlice("header")
	if err != nil {
		return c, err
	}

	return c, nil
}
