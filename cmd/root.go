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
	"io"
	"log"
	"os"
	"strings"

	"github.com/Ericsson/jaq/transform"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"
)

// RootCmdrepresents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "jaq",
	Short: "A scriptable, command-line tool for working with JSON endpoints",
	Long: `A scriptable, command-line tool for working with JSON endpoints.
Pipe data from one endpoint to another while utilizing data from the first to
run multiple commands against the next. Pairs well with a tool like jq which
can filter and pretty print json.

Use a configuration file to automatically handle the most common fields when
routinely working with an API such as domain/subdomain and authorization.

Examples:

> jaq get /posts
> jaq get /posts | jq -c .[] | jaq delete /posts/${1.id} --dry-run
> jaq get /posts | jq -c .[0:3] | jaq get /comments -q postId=${1.id}
`,

	// SilenceUsage set so you don't get the whole usage output every time
	// there is an HTTP error.
	SilenceUsage: true,

	// SilenceErrors set so we have to be explicit about when printing/logging
	// messages to the user.
	SilenceErrors: true,
}

// Execute is called by main.main(). It only needs to happen once.
func Execute() {
	var pipeFrom io.Reader
	if !terminal.IsTerminal(int(os.Stdin.Fd())) {
		pipeFrom = os.Stdin
	} else {
		pipeFrom = nil
	}

	if err := execute(os.Args[1:], pipeFrom); err != nil {
		log.Println(err)
		os.Exit(-1)
	}
}

// execute handles parsing the input and translating that into sets of commands
// that then get run in series.
func execute(args []string, pipeFrom io.Reader) error {
	var userCmd [][]string
	var err error

	// Somewhat weird workaround, but we need to get some flag information
	// before subocmmands parse all the flags. If we call Parse() with the
	// actual command flags then we will not duplicate values in stringSlice
	// flags.
	tmpFlags := pflag.NewFlagSet("tmpSet", pflag.ContinueOnError)
	addFlags(tmpFlags)
	tmpFlags.Parse(args)

	// Now that we have the config file
	config := viper.GetString("config")
	initConfig(config)
	explode := viper.GetBool("explode")

	// Dont read from input if it is a terminal or else you will just hang
	// waiting for EOF.
	if pipeFrom != nil {
		userCmd, err = transform.InputToCommands(pipeFrom, args, explode)
		if err != nil {
			return err
		}
	} else {
		userCmd = make([][]string, 1)
		userCmd[0] = args
	}

	for _, userCmd := range userCmd {
		RootCmd.SetArgs(userCmd)
		if err := RootCmd.Execute(); err != nil {
			return err
		}
	}

	return nil
}

func init() {
	manualInit()
}

func manualInit() {
	RootCmd.ResetCommands()
	RootCmd.ResetFlags()
	viper.Reset()

	addFlags(RootCmd.PersistentFlags())
	manualInitHTTPVerbs()

	// Explicitly loading config now so that we can get config and explode.
	// Config is needed in order to properly load the right config file which
	// may contain explode. Explode is needed prior to sub-command invocation
	// for parsing of input.
	initConfig("")
}

// addFlags allows you to reinitialize flags/viper/cobra.
func addFlags(fs *pflag.FlagSet) {
	fs.StringP("config", "c", "", "Configuration file path")
	viper.BindPFlag("config", fs.Lookup("config"))

	fs.BoolP("dry-run", "d", false, "Dry-run mode; print commands after handling input subtitutions")
	viper.BindPFlag("dry-run", fs.Lookup("dry-run"))

	fs.BoolP("trace", "", false, "Trace mode. Outputs requests/responses to stderr")
	viper.BindPFlag("trace", fs.Lookup("trace"))
	fs.BoolP("debug", "", false, "Debug mode. Force full body output when tracing")
	viper.BindPFlag("debug", fs.Lookup("debug"))

	fs.StringP("auth", "", "", "Type of auth to be used")
	viper.BindPFlag("auth", fs.Lookup("auth"))

	fs.StringP("subdomain", "", "", "Subdomain to send request to")
	viper.BindPFlag("subdomain", fs.Lookup("subdomain"))

	fs.BoolP("explode", "", true, "Treat JSON arrays as separate elements and not one")
	viper.BindPFlag("explode", fs.Lookup("explode"))

	fs.StringP("scheme", "", "https", "Scheme for the HTTP request")
	viper.BindPFlag("scheme", fs.Lookup("scheme"))

	fs.StringP("query", "q", "", "Query string to be sent with request")
	fs.StringSliceP("header", "H", []string{}, "Comma-separated list of headers to add to be sent with request (e.g. a=b,x=y)")

	fs.StringP("body", "b", "", "Body to be sent with request")
	fs.StringP("file", "f", "", "File contents to be sent with request as the body")

	fs.StringP("on-error", "", "report", "Strategy for how to handle responses with codes >= 400")
	viper.BindPFlag("on-error", fs.Lookup("on-error"))

	fs.BoolP("print-headers", "", false, "Appends headers to response json objects as fields with the prefix jaq-")
	viper.BindPFlag("print-headers", fs.Lookup("print-headers"))

	fs.IntP("request-timeout", "t", 15, "Request timeout (in seconds)")
	viper.BindPFlag("request-timeout", fs.Lookup("request-timeout"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".jaq")
		viper.AddConfigPath("$HOME")
		viper.SetConfigType("json")
	}

	// Any viper.Get() will check JAQ_[KEY] in the env.
	viper.SetEnvPrefix("JAQ")
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Error reading config: %v", err)
		log.Print("jaq not configured; expects either $HOME/.jaq.json or a config at the path specified via --config")
		os.Exit(-1)
	}
}
