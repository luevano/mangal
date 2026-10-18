package cmd

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/cobra"
)

var apiArgs = struct {
	Data   string
	SSE    bool
	Server string
	List   bool
	Raw    bool
}{}

func init() {
	rootCmd.AddCommand(apiCmd)
	f := apiCmd.Flags()
	f.StringVarP(&apiArgs.Data, "data", "d", "", "JSON body, or @file (@- for stdin); the method is POST with one")
	f.BoolVar(&apiArgs.SSE, "sse", false, "Stream server-sent events (e.g. events?topics=job,op), one JSON per line")
	f.StringVar(&apiArgs.Server, "server", "", "The server's url (by default the running mangal serve of this config)")
	f.BoolVar(&apiArgs.List, "list", false, "List the API's operations (method, path, scope) from its spec")
	f.BoolVar(&apiArgs.Raw, "raw", false, "Print the answer as it comes, not pretty-printed")
}

var apiCmd = &cobra.Command{
	Use:   "api [METHOD] PATH",
	Short: "Call the serve API",
	Long: `Call the API of the running mangal serve of this config (found with its discovery file,
which has its key: every permission), or of --server with an API token in MANGAL_API_TOKEN
("mangal token add <name> --scope read" on the server makes one; it also
overrides the key). PATH is relative to
/api/v1 unless it starts with /api/. The answer is pretty-printed; an error answer exits
non-zero. Changes plan first: POST without ?apply=1 answers the plan and its hash, then
the same with ?apply=1 and "expect": "<hash>" applies it.`,
	Example: `  mangal api status
  mangal api 'library/mangas?q=punch'
  mangal api POST library/merge -d '{"keep":5,"other":6}'
  mangal api POST 'library/merge?apply=1' -d '{"keep":5,"other":6,"expect":"sha256:..."}'
  mangal api --sse 'events?topics=job,op'
  mangal api --list`,
	GroupID: groupMode,
	Args:    cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if apiArgs.List {
			return listAPI(cmd)
		}
		if len(args) == 0 {
			return errors.New("a path is needed (--list shows them)")
		}
		method, path := "", args[len(args)-1]
		if len(args) == 2 {
			method = strings.ToUpper(args[0])
		}
		body, err := apiBody(cmd, apiArgs.Data)
		if err != nil {
			return err
		}
		if method == "" {
			method = http.MethodGet
			if body != nil {
				method = http.MethodPost
			}
		}
		server, key, err := apiServer(apiArgs.Server)
		if err != nil {
			return err
		}
		token := cmp.Or(os.Getenv("MANGAL_API_TOKEN"), key)
		req, err := http.NewRequestWithContext(cmd.Context(), method, server+apiPath(path), bytes.NewReader(body))
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if apiArgs.SSE {
			req.Header.Set("Accept", "text/event-stream")
		}
		httpClient := http.DefaultClient
		if apiArgs.Server == "" {
			if httpClient, err = watch.LocalClient(server, 0); err != nil {
				return err
			}
		}
		res, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if apiArgs.SSE && res.StatusCode < 400 {
			return streamSSE(cmd, res.Body)
		}
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if res.StatusCode >= 400 {
			out = cmd.ErrOrStderr()
		}
		printAPI(out, data, apiArgs.Raw)
		if res.StatusCode >= 400 {
			var e struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(data, &e)
			cmd.SilenceUsage = true
			if e.Code != "" {
				return fmt.Errorf("%s (%s)", res.Status, e.Code)
			}
			return errors.New(res.Status)
		}
		return nil
	},
}

// apiPath is the path under /api/v1 unless it's under /api/ already.
func apiPath(path string) string {
	if strings.HasPrefix(path, "/api/") || path == "/api" {
		return path
	}
	return "/api/v1/" + strings.TrimPrefix(path, "/")
}

// apiBody is the body of -d: JSON, @file, @- for stdin; nil without one.
func apiBody(cmd *cobra.Command, data string) ([]byte, error) {
	if data == "" {
		return nil, nil
	}
	var b []byte
	switch {
	case data == "@-":
		var err error
		if b, err = io.ReadAll(cmd.InOrStdin()); err != nil {
			return nil, err
		}
	case strings.HasPrefix(data, "@"):
		var err error
		if b, err = os.ReadFile(data[1:]); err != nil {
			return nil, err
		}
	default:
		b = []byte(data)
	}
	if !json.Valid(b) {
		return nil, errors.New("--data isn't JSON")
	}
	return b, nil
}

// apiServer is the server's url: given, or the running mangal serve's of this config.
// apiServer is the server's url, and the discovery file's key for this config's own.
func apiServer(given string) (server, key string, err error) {
	if given != "" {
		return strings.TrimSuffix(given, "/"), "", nil
	}
	data := watch.DataDB()
	d, found, err := watch.ReadDiscovery(watch.DiscoveryPath(data))
	if err != nil {
		return "", "", err
	}
	if !found || d.Data != data || d.Address == "" {
		return "", "", errors.New("no mangal serve runs for this config (start one, or give --server URL)")
	}
	return strings.TrimSuffix(d.Address, "/"), d.Key, nil
}

// printAPI prints JSON indented (as it comes with raw, or when it isn't JSON).
func printAPI(w io.Writer, data []byte, raw bool) {
	var buf bytes.Buffer
	if !raw && json.Indent(&buf, data, "", "  ") == nil {
		buf.WriteByte('\n')
		_, _ = w.Write(buf.Bytes())
		return
	}
	_, _ = w.Write(data)
	if len(data) != 0 && data[len(data)-1] != '\n' {
		fmt.Fprintln(w)
	}
}

// streamSSE prints the data of each event, a line each, until the stream ends.
func streamSSE(cmd *cobra.Command, body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			cmd.Println(data)
		}
	}
	if err := sc.Err(); err != nil && cmd.Context().Err() == nil {
		return err
	}
	return nil
}

// listAPI lists the operations of the spec.
func listAPI(cmd *cobra.Command) error {
	ops, err := web.Operations()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, o := range ops {
		if !o.V1 {
			continue
		}
		scope := o.Scope
		if o.Plan {
			scope += " (plan: read)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", o.Method, o.Path, scope, o.Summary)
	}
	return tw.Flush()
}
