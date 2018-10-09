# jaq
CLI tool for chaining JSON API requests.

[![Build Status](https://travis-ci.com/Ericsson/jaq.svg?branch=master)](https://travis-ci.com/Ericsson/jaq)

jaq simplifies running multiple, related API commands such as getting IDs or other fields from one endpoint for use in another query.
 - Uses a config file to avoid repetition on the command-line
 - Set URL paths, query strings, or headers
 - Pipe output from one endpoint to another
 - Ability to "explode" a JSON list into separate elements for separate processing

## Installation

1. Clone the repo & run `go install`
2. Setup a small config file at ~/.jaq.json:
3. Run commands!

```bash
go get github.com/Ericsson/jaq && go install
echo '{"domain":"jsonplaceholder.typicode.com"}' > ~/.jaq.json
jaq get /posts
```

Although not a requirement, jaq works really well if you are using [jq](https://github.com/stedolan/jq) as well for JSON querying/filtering.

## Examples

These examples assume you are have the jq tool available since we do not try to duplicate its capabilities.

> **NOTE**
> jaq consumes the `$` as a special character so the commands below escape it so it is not consumed by your shell.

```bash
# Examples assume config file
echo '{"domain":"jsonplaceholder.typicode.com"}' > ~/.jaq.json

# Get objects from an endpoint
jaq get /posts

# Delete all the objects by id which have userId == 3
jaq get /posts | jq -c '.[] | select(.userId == 3)' | jaq delete /posts/\${1.id}

# Get comments from the 3 most recent posts
jaq get /posts | jq -c .[-3:] | jaq get /comments -q postId=\${1.id}

```

## Configuration

jaq uses a configuration file (defaults to ~/.jaq.json but configurable via a flag) to make your commands more succinct.

You can also override most settings in the configuration file by using environment variables with similar keys. Use env vars of the form: `JAQ_<KEY_NAME>` to set the value of "key-name". (Env vars must be upper cased, prefixed with `JAQ_` and hyphens replaced with underscores.

### Auth

The auth setting can switch which authorization scheme to use by default. Currently the supported types are:
 - `basic` - Will use the "user" and "pass" fields to set the Authorization header.
 - `token` - Will set the header "Authorization: Bearer \<token\>"

### dry-run

The dry-run setting allows you to try out potentially destructive commands and ensure all the input transformations result in the expected commands.

When running in dry-run mode, all of the input transformations occur but the resulting commands are just printed to stdout rather than actually being run.

### Printing headers

Since jaq operates by piping JSON over stdin, if you want to use a header field you must include it in the JSON from the response. To facilitate this, when `--print-headers` is set, all of the headers are added as new JSON fields on all the JSON objects of the response with the prefix `jaq-`.

### Error handling

You may want different behavior when encountering an error (HTTP response >= 400). Options are:

 - `silence` - Completely ignore those responses and do not print them or error.
 - `fatal` - Print them to stderr and return an error.
 - `continue` - Print the responses to stdout as if they were not errors and continue.
 - `report` - Print the responses to stderr so they do not pollute stdout for other piped commands.

### Trace/Debug

When executing commands you may want an entire dump of the HTTP request/response. By specifying `--trace` the request/response will be dumped to stderr (so that it doesn't interfere with the JSON on stdout). By default, the body of the requests are _NOT_ dumped. You can set `--DEBUG` to also add the body of the request.
