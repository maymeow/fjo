// Package apicmd implements `fjo api` (IMPLEMENTATION §5.9): authenticated
// raw requests to the Forgejo REST API, the fallback for endpoints without a
// dedicated command. Flags follow `gh api` (§1.4); deviations are listed in
// docs/compatibility.md.
package apicmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type options struct {
	hostname  string
	method    string
	fields    []string // -F, typed
	rawFields []string // -f, strings
	headers   []string
	input     string
	paginate  bool
	include   bool
	silent    bool

	methodSet bool
}

// NewCmd returns the `api` command.
func NewCmd(f *app.Factory) *cobra.Command {
	var o options
	cmd := &cobra.Command{
		Use:   "api <endpoint>",
		Short: "Make an authenticated Forgejo API request",
		Long: `Make an authenticated HTTP request to the Forgejo API and print the response.

The endpoint is a path relative to the API root (e.g. /user, which becomes
https://<host>/api/v1/user) or an absolute URL on the same instance.

The default method is GET, or POST when fields or --input are given.
For GET requests fields are sent as query parameters; otherwise they form a
JSON request body.

  -f/--raw-field key=value   adds a string value.
  -F/--field key=value       converts true, false, null and integers to JSON
                             types; "@file" reads the value from a file and
                             "@-" from standard input.

Keys may be nested: "labels[]=1" appends to an array and "a[b]=c" sets a key
of an object.

--paginate follows the server's Link headers (only within the instance) and
merges JSON array pages into one array.

On an HTTP error status the response body is printed to stdout and the error
to stderr; the exit code reflects the error class (4 auth, 11 forbidden,
12 not found, 13 conflict, 14 unavailable; see docs/compatibility.md).`,
		Example: `  fjo api /user
  fjo api /repos/owner/repo/issues --paginate
  fjo api /user/repos -F name=demo -F private=true
  fjo api -X PATCH /repos/owner/repo -f description="New description"
  fjo api /repos/owner/repo/labels --input labels.json
  fjo api /repos/search -f q=cli -f limit=5 -X GET`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.methodSet = cmd.Flags().Changed("method")
			return run(cmd.Context(), f, o, args[0])
		},
	}
	fl := cmd.Flags()
	// No -h shorthand: gh api uses --hostname only.
	fl.StringVar(&o.hostname, "hostname", "", "Forgejo host: host ID or URL (default: FJO_HOST, defaults.host)")
	fl.StringVarP(&o.method, "method", "X", http.MethodGet, "HTTP method")
	fl.StringArrayVarP(&o.fields, "field", "F", nil, "Add a typed parameter in key=value format")
	fl.StringArrayVarP(&o.rawFields, "raw-field", "f", nil, "Add a string parameter in key=value format")
	fl.StringArrayVarP(&o.headers, "header", "H", nil, "Add an HTTP request header in key:value format")
	fl.StringVar(&o.input, "input", "", `File to use as the request body ("-" for standard input)`)
	fl.BoolVar(&o.paginate, "paginate", false, "Fetch all pages and merge JSON arrays")
	fl.BoolVarP(&o.include, "include", "i", false, "Include the HTTP status line and response headers in the output")
	fl.BoolVar(&o.silent, "silent", false, "Do not print the response body")
	return cmd
}

func run(ctx context.Context, f *app.Factory, o options, endpoint string) error {
	method := strings.ToUpper(strings.TrimSpace(o.method))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return fjoerr.Usage("invalid --method %q", o.method)
	}
	endpoint, repoHost, err := fillPlaceholders(ctx, f, endpoint)
	if err != nil {
		return err
	}

	headers, err := parseHeaders(o.headers)
	if err != nil {
		return err
	}
	stdinUsed := o.input == "-"
	params, err := parseFields(f, o.rawFields, o.fields, &stdinUsed)
	if err != nil {
		return err
	}
	if !o.methodSet && (len(params) > 0 || o.input != "") {
		method = http.MethodPost
	}

	if o.paginate {
		if method != http.MethodGet {
			return fjoerr.Usage("--paginate requires the GET method")
		}
		if o.input != "" {
			return fjoerr.Usage("--paginate cannot be used with --input")
		}
	}

	// Body and query: fields go to the query string for GET or when --input
	// supplies the body (gh semantics), otherwise into a JSON object.
	var body []byte
	path := endpoint
	switch {
	case o.input != "":
		if body, err = readInput(f, o.input); err != nil {
			return err
		}
		if path, err = addQuery(path, params); err != nil {
			return err
		}
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
	case method == http.MethodGet || method == http.MethodHead:
		if path, err = addQuery(path, params); err != nil {
			return err
		}
	case len(params) > 0:
		obj, err := buildObject(params)
		if err != nil {
			return err
		}
		if body, err = json.Marshal(obj); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", "application/json")
		}
	}

	hostFlag := o.hostname
	if hostFlag == "" {
		hostFlag = repoHost // the repository's host when placeholders were used
	}
	host, err := f.ResolveHost(hostFlag)
	if err != nil {
		return err
	}
	client, _, err := f.ClientFor(host)
	if err != nil {
		return err
	}

	if o.paginate {
		return runPaginated(ctx, f, client, o, path, headers)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := client.NewRequest(ctx, method, path, reader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(resp.Body)
	if err = fjoerr.JoinCleanup(err, resp.Body.Close()); err != nil {
		return fjoerr.Wrap(fjoerr.KindUnavailable, err, "reading response from %s", client.Host())
	}
	w := f.IO.Out
	if o.include {
		printHead(w, resp)
	}
	if !o.silent {
		if err := writeBody(f, w, resp.Header, data); err != nil {
			return err
		}
	}
	if resp.StatusCode >= 400 {
		return api.ErrorFromBody(resp, data)
	}
	return nil
}

// fillPlaceholders replaces {owner}, {repo} and {branch} like gh: the
// repository comes from FJO_REPO, the default set with `fjo repo
// set-default` or the git remotes; {branch} is the checked-out branch. It
// returns the repository's host ("" when no placeholder was used).
func fillPlaceholders(ctx context.Context, f *app.Factory, endpoint string) (string, string, error) {
	hasRepo := strings.Contains(endpoint, "{owner}") || strings.Contains(endpoint, "{repo}")
	hasBranch := strings.Contains(endpoint, "{branch}")
	if !hasRepo && !hasBranch {
		return endpoint, "", nil
	}
	host := ""
	if hasRepo {
		ref, err := f.ResolveRepo(ctx, "")
		if err != nil {
			return "", "", err
		}
		endpoint = strings.ReplaceAll(endpoint, "{owner}", url.PathEscape(ref.Owner))
		endpoint = strings.ReplaceAll(endpoint, "{repo}", url.PathEscape(ref.Name))
		host = ref.Host
	}
	if hasBranch {
		branch, err := f.Git().CurrentBranch(ctx)
		if err != nil {
			if cerr := app.GitContextError(err); fjoerr.KindOf(cerr) == fjoerr.KindUsage {
				return "", "", cerr
			}
			return "", "", fjoerr.Wrap(fjoerr.KindUsage, err, "cannot fill {branch}")
		}
		endpoint = strings.ReplaceAll(endpoint, "{branch}", url.PathEscape(branch))
	}
	return endpoint, host, nil
}

func runPaginated(ctx context.Context, f *app.Factory, client *api.Client, o options, path string, headers http.Header) error {
	w := f.IO.Out
	var (
		items    []json.RawMessage
		array    = true
		lastHdr  http.Header
		single   []byte
		pageNum  int
		pageErrs error
	)
	err := client.Pages(ctx, path, headers, 0, func(resp *http.Response) (bool, error) {
		pageNum++
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return true, fjoerr.Wrap(fjoerr.KindUnavailable, err, "reading response from %s", client.Host())
		}
		if o.include {
			printHead(w, resp)
		}
		lastHdr = resp.Header
		var page []json.RawMessage
		if json.Unmarshal(data, &page) != nil {
			if pageNum == 1 {
				// Not a list endpoint: print the single response as-is.
				array, single = false, data
				return true, nil
			}
			return true, fjoerr.New(fjoerr.KindGeneral, "page %d of %s is not a JSON array", pageNum, api.SanitizeURL(resp.Request.URL))
		}
		items = append(items, page...)
		return len(page) == 0, nil
	})
	if err != nil {
		pageErrs = err
	}
	if !o.silent && pageNum > 0 {
		if !array {
			return fjoerr.JoinCleanup(pageErrs, writeBody(f, w, lastHdr, single))
		}
		if pageErrs == nil {
			if items == nil {
				items = []json.RawMessage{}
			}
			merged, merr := json.Marshal(items)
			if merr != nil {
				return fmt.Errorf("encode merged pages: %w", merr)
			}
			hdr := http.Header{"Content-Type": {"application/json"}}
			return writeBody(f, w, hdr, merged)
		}
	}
	return pageErrs
}

// writeBody prints a response body. JSON is indented when stdout is a
// terminal; everything else is copied verbatim.
func writeBody(f *app.Factory, w io.Writer, h http.Header, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if f.IO.IsStdoutTTY() && isJSON(h) {
		var buf bytes.Buffer
		if json.Indent(&buf, data, "", "  ") == nil {
			buf.WriteByte('\n')
			_, err := w.Write(buf.Bytes())
			return err
		}
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if f.IO.IsStdoutTTY() && data[len(data)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func isJSON(h http.Header) bool {
	ct := strings.ToLower(h.Get("Content-Type"))
	return strings.HasPrefix(ct, "application/json") || strings.Contains(ct, "+json")
}

func printHead(w io.Writer, resp *http.Response) {
	fmt.Fprintf(w, "%s %s\n", resp.Proto, resp.Status)
	names := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		for _, v := range resp.Header[k] {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}
	}
	fmt.Fprintln(w)
}

func parseHeaders(list []string) (http.Header, error) {
	h := http.Header{}
	for _, s := range list {
		k, v, ok := strings.Cut(s, ":")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t\r\n") || strings.ContainsAny(v, "\r\n") {
			return nil, fjoerr.Usage("invalid header %q: expected key:value", s)
		}
		h.Add(k, strings.TrimSpace(v))
	}
	return h, nil
}

func readInput(f *app.Factory, name string) ([]byte, error) {
	if name == "-" {
		b, err := io.ReadAll(f.IO.In)
		if err != nil {
			return nil, fjoerr.Wrap(fjoerr.KindGeneral, err, "read standard input")
		}
		return b, nil
	}
	b, err := os.ReadFile(name) //nolint:gosec // G304: --input names a file the user chose
	if err != nil {
		return nil, fjoerr.Wrap(fjoerr.KindUsage, err, "read --input file")
	}
	return b, nil
}

// addQuery appends params to the endpoint's query string. Values are
// rendered as strings; null becomes an empty value.
func addQuery(path string, params []param) (string, error) {
	if len(params) == 0 {
		return path, nil
	}
	q := url.Values{}
	for _, p := range params {
		var s string
		switch v := p.value.(type) {
		case nil:
		case string:
			s = v
		default:
			s = fmt.Sprint(v)
		}
		q.Add(p.key, s)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + q.Encode(), nil
}
