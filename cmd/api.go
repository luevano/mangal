package cmd

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/script/lib/api"
	"github.com/luevano/mangal/v5/serve"
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/cobra"
)

var apiArgs = struct {
	Data   string
	SSE    bool
	Server string
	List   bool
	Raw    bool
	Write  bool
}{}

func init() {
	rootCmd.AddCommand(apiCmd)
	f := apiCmd.Flags()
	f.StringVarP(&apiArgs.Data, "data", "d", "", "JSON body, or @file (@- for stdin); the method is POST with one")
	f.BoolVar(&apiArgs.SSE, "sse", false, "Stream server-sent events (e.g. events?topics=job,op), one JSON per line")
	f.StringVar(&apiArgs.Server, "server", "", "The server's url (by default the running mangal serve of this config)")
	f.BoolVar(&apiArgs.List, "list", false, "List the API's operations (method, path, scope) from its spec")
	f.BoolVar(&apiArgs.Raw, "raw", false, "Print the answer as it comes, not pretty-printed")
	f.BoolVar(&apiArgs.Write, "write", false, "With no mangal serve (the API in this process): download and manage on top of script.api_scopes")
}

var apiCmd = &cobra.Command{
	Use:   "api [METHOD] PATH",
	Short: "Call the serve API",
	Long: `Call the API of --server with an API token in MANGAL_API_TOKEN (or in the file
MANGAL_API_TOKEN_FILE names; "mangal token add <name> --perms read" on the server makes one),
else of the connection's server with its saved token, on local (or with --local) of the running
mangal serve of this config (found with its discovery file, which has its key: an admin), else
of the API in this process: the same operations and answers, as the "script" identity with
script.api_scopes (--write adds download and manage); its downloads run in this process and the
command waits for them. PATH is relative to /api/v1 unless it starts with /api/. The answer is
pretty-printed; an error answer exits non-zero. Changes plan first: POST without ?apply=1
answers the plan and its hash, then the same with ?apply=1 and "expect": "<hash>" applies it.`,
	Example: `  mangal api status
  mangal api 'library/mangas?q=punch'
  mangal api POST library/merge -d '{"keep":5,"other":6}'
  mangal api POST 'library/merge?apply=1' -d '{"keep":5,"other":6,"expect":"sha256:..."}'
  mangal api --sse 'events?topics=job,op'
  mangal api --local --write POST downloads -d '{"provider":"mango-mangadex","query":"berserk","chapters":"last"}'
  mangal api --list`,
	GroupID: groupMode,
	Args:    cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
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
		conn, local, err := apiConn(cmd, apiArgs.Server, apiArgs.Write)
		if err != nil {
			return err
		}
		if local != nil {
			defer func() { err = cmp.Or(err, closeLocal(cmd, local)) }()
		}
		req, err := http.NewRequestWithContext(cmd.Context(), method, conn.URL+api.Path(path), bytes.NewReader(body))
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if conn.Token != "" {
			req.Header.Set("Authorization", "Bearer "+conn.Token)
		}
		if apiArgs.SSE {
			req.Header.Set("Accept", "text/event-stream")
		}
		res, err := conn.HTTP.Do(req)
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

// errNoServe is apiServer's when no server is set and no mangal serve runs for this config.
var errNoServe = errors.New("no mangal serve runs for this config")

// apiConn is the API the command calls: apiServer's with MANGAL_API_TOKEN over its key,
// else the API in this process (local, the caller closes it: it waits for its jobs).
func apiConn(cmd *cobra.Command, given string, write bool) (api.Conn, *serve.Local, error) {
	server, key, httpClient, err := apiServer(cmd, given)
	if errors.Is(err, errNoServe) {
		local, err := newLocalAPI(cmd, write)
		if err != nil {
			return api.Conn{}, nil, err
		}
		return api.Conn{URL: serve.LocalURL, HTTP: local.Client}, local, nil
	}
	if err != nil {
		return api.Conn{}, nil, err
	}
	envToken, err := config.EnvSecret("MANGAL_API_TOKEN")
	if err != nil {
		return api.Conn{}, nil, err
	}
	return api.Conn{URL: server, Token: cmp.Or(envToken, key), HTTP: httpClient}, nil, nil
}

// newLocalAPI is the API in this process with script.api_scopes, download and manage on
// top with write.
func newLocalAPI(cmd *cobra.Command, write bool) (*serve.Local, error) {
	perms := config.Script.APIScopes.Get()
	if write {
		perms = append(slices.Clone(perms), serve.ScopeDownload, serve.ScopeManage)
	}
	local, err := serve.NewLocal(ctxOf(cmd), perms)
	if err != nil {
		return nil, fmt.Errorf("%s: the API in this process: %w", config.Script.APIScopes.Key, err)
	}
	cmd.PrintErrf("no mangal serve: the API in this process (%s)\n", strings.Join(perms, ", "))
	return local, nil
}

// closeLocal waits for the jobs started in this process (ctrl+c cancels them, an error).
func closeLocal(cmd *cobra.Command, local *serve.Local) error {
	ctx := ctxOf(cmd)
	n := local.Active()
	if n > 0 && ctx.Err() == nil {
		cmd.PrintErrf("waiting for %d job(s) of this process (ctrl+c cancels)\n", n)
	}
	local.Close(ctx)
	if n > 0 && ctx.Err() != nil {
		return fmt.Errorf("the jobs of this process: %w", ctx.Err())
	}
	return nil
}

// scriptAPI is mangal.api's Connect: api.connect's server with its token (else the one
// saved for it), else apiConn's, found on the first call. done closes the API in this
// process when it was used.
func scriptAPI(cmd *cobra.Command, given string, write bool) (connect api.Connect, done func() error) {
	var local *serve.Local
	find := sync.OnceValues(func() (api.Conn, error) {
		conn, l, err := apiConn(cmd, given, write)
		local = l
		return conn, err
	})
	connect = func(_ context.Context, url, token string) (api.Conn, error) {
		if url == "" {
			return find()
		}
		url = strings.TrimSuffix(url, "/")
		token, client, err := serverClient(cmd, url, token)
		return api.Conn{URL: url, Token: token, HTTP: client}, err
	}
	return connect, func() error {
		if local != nil {
			return closeLocal(cmd, local)
		}
		return nil
	}
}

// serverClient is the token (the one saved for url when empty) and the client of a
// server, with the remote.* settings.
func serverClient(cmd *cobra.Command, url, token string) (string, *http.Client, error) {
	if token == "" {
		var err error
		if token, _, err = remote.Token(url); err != nil {
			return "", nil, err
		}
	}
	rc, err := remote.Open(url, token, remoteWarn(cmd))
	if err != nil {
		return "", nil, err
	}
	// a stream has no timeout
	return token, &http.Client{Transport: rc.HTTP.Transport, CheckRedirect: rc.HTTP.CheckRedirect}, nil
}

// apiServer is the server to call and how: --server as given, the connection's with its saved
// token and the remote.* settings, else the running mangal serve of this config with the
// discovery file's key (errNoServe without one).
func apiServer(cmd *cobra.Command, given string) (server, key string, client *http.Client, err error) {
	if given != "" {
		return strings.TrimSuffix(given, "/"), "", http.DefaultClient, nil
	}
	url, err := remoteTarget(cmd).Address()
	if err != nil {
		return "", "", nil, err
	}
	if url != "" {
		token, client, err := serverClient(cmd, url, "")
		return url, token, client, err
	}
	data := watch.DataDB()
	d, found, err := watch.ReadDiscovery(watch.DiscoveryPath(data))
	if err != nil {
		return "", "", nil, err
	}
	if !found || d.Data != data || d.Address == "" {
		return "", "", nil, errNoServe
	}
	server = strings.TrimSuffix(d.Address, "/")
	if client, err = watch.LocalClient(server, 0); err != nil {
		return "", "", nil, err
	}
	return server, d.Key, client, nil
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
			scope += " (plan)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", o.Method, o.Path, scope, o.Summary)
	}
	return tw.Flush()
}
