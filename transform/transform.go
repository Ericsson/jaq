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

package transform

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/Jeffail/gabs"
)

var truncationLength = 512

// InputToCommands reads from the given io.Reader (e.g. os.Stdin) and uses the
// data there to replace values like $1.uuid in the args. It returns a
// [][]string which is a set of rows, each with a slice of string values.
func InputToCommands(r io.Reader, args []string, explodeArrays bool) ([][]string, error) {
	data, err := readData(r, explodeArrays)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return [][]string{args}, nil
	}

	cmds := make([][]string, len(data))
	for rowI := range data {
		cmds[rowI] = make([]string, len(args))
		for argI := range args {
			cmds[rowI][argI] = transform(data[rowI], args[argI])
		}
	}

	return cmds, nil
}

// readData reads all data from the given reader and splits it into a
// [][]string: a slice of commands, each command having multiple positional
// arguments. If explodeArrays is true, arrays are treated as if they were
// simply given as a list of newline separated JSON objects.
func readData(r io.Reader, explodeArrays bool) ([][]string, error) {
	var data [][]string

ProcessLoop:
	dec := json.NewDecoder(r)
	for dec.More() {
		var m interface{}
		err := dec.Decode(&m)
		if err != nil {
			if err == io.EOF {
				break
			}

			switch err.(type) {
			case *json.SyntaxError:
				// Allow parsing as a string.
			default:
				return nil, err
			}
		}

		switch raw := m.(type) {
		case []interface{}:
			if explodeArrays {
				for _, obj := range raw {
					jsonObj, ok := obj.(map[string]interface{})
					if !ok {
						return nil, errors.New("invalid piped data")
					}

					b, err := json.Marshal(jsonObj)
					if err != nil {
						return nil, err
					}
					// Every value gets appened in its own row.
					data = append(data, []string{string(b)})
				}
			} else {
				b, err := json.Marshal(raw)
				if err != nil {
					return nil, err
				}
				data = append(data, []string{string(b)})
			}
		case map[string]interface{}:
			b, err := json.Marshal(raw)
			if err != nil {
				return nil, err
			}
			// Every value gets appened in its own row.
			data = append(data, []string{string(b)})
		case nil:
			// Failed to parse as JSON; parse as a word.
			subR := dec.Buffered()
			scanner := bufio.NewScanner(subR)
			scanner.Split(bufio.ScanWords)
			for scanner.Scan() {
				data = append(data, []string{scanner.Text()})
			}
			if err := scanner.Err(); err != nil {
				log.Fatalf("reading standard input: %v", err)
			}

			// Restart the process loop with what is rest of the buffered data.
			// Use a multireader so that if the original decoder didn't buffer
			// it all we don't lose data.
			r = io.MultiReader(subR, r)
			goto ProcessLoop
		default:
			return nil, fmt.Errorf("unexpected type (%T): %v", raw, truncatedValue(raw))
		}
	}

	return data, nil
}

// transform uses the data to transform the argument (e.g. foo $1.uuid -> foo
// uuid)
func transform(data []string, arg string) string {
	return os.Expand(arg, dataLookup(data))
}

// dataLookup generates closures which lookup transformation values ($1.uuid)
// and returns their values based on the data passed to the generator.
func dataLookup(data []string) func(string) string {
	return func(s string) string {
		pos, jsonKeyQuery := parseTransform(s)
		if pos > len(data) || pos < 0 {
			return "<nil>"
		}
		item := data[pos-1]

		// If just giving position, leave as-is.
		if jsonKeyQuery == "" {
			return item
		}

		return jsonQuery(item, jsonKeyQuery)
	}
}

// parseTransform takes a string expected to be a substitution variable (e.g.
// $1.uuid) and splits it into its position and json query parts.
func parseTransform(s string) (position int, jsonQuery string) {
	parts := strings.SplitN(s, ".", 2)

	if len(parts) == 1 {
		// Either the whole thing is a positional arg or the query.
		pos, err := strconv.Atoi(s)
		if err != nil {
			return 1, s
		}
		return pos, ""
	}

	// Check if it starts with a position, otherwise it is all the query
	pos, err := strconv.Atoi(parts[0])
	if err != nil {
		return 1, s
	}

	return pos, parts[1]
}

// jsonQuery queries the given data for the value of the field specified by the
// given query. If there is an error parsing the json or the field does not
// exist, the empty string is returned.
func jsonQuery(data, query string) string {
	jsonParsed, err := gabs.ParseJSON([]byte(data))
	if err != nil {
		return "<nil>"
	}

	return fmt.Sprint(jsonParsed.Path(query).Data())
}

// truncatedValue is showing just part of the value in case its a huge binary or
// web page.
func truncatedValue(i interface{}) string {
	s := fmt.Sprint(i)
	if len(s) > truncationLength {
		s = fmt.Sprintf("%q...\n[Value truncated]", s[:truncationLength])
	}

	return s
}
