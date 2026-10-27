package serve

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// trackerLoginFor is how long a started login waits for its paste.
const trackerLoginFor = 10 * time.Minute

// trackerLogins are the logins started through the API waiting for their paste, by user
// and tracker: the login runs on (oauth.Manual) between the two requests.
type trackerLogins struct {
	mu      sync.Mutex
	pending map[string]*trackerLogin
}

type trackerLogin struct {
	paste  chan string
	done   chan trackerResult
	cancel context.CancelFunc
}

type trackerResult struct {
	user metadata.User
	err  error
}

// start starts the login of the user's tracker (a started one is dropped) and is its
// authorize url.
func (t *trackerLogins) start(ctx context.Context, user, source string) (string, error) {
	login := map[string]func(context.Context, string, oauth.Manual) (metadata.User, error){
		"anilist":     anilist.LoginFor,
		"myanimelist": myanimelist.LoginFor,
	}[source]
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trackerLoginFor)
	l := &trackerLogin{paste: make(chan string), done: make(chan trackerResult, 1), cancel: cancel}
	t.mu.Lock()
	if t.pending == nil {
		t.pending = map[string]*trackerLogin{}
	}
	if old := t.pending[user+"/"+source]; old != nil {
		old.cancel()
	}
	t.pending[user+"/"+source] = l
	t.mu.Unlock()

	url := make(chan string, 1)
	go func() {
		defer cancel()
		u, err := login(lctx, user, oauth.Manual{Show: func(u string) { url <- u }, Pasted: l.paste})
		l.done <- trackerResult{u, err}
	}()
	select {
	case u := <-url:
		return u, nil
	case r := <-l.done:
		t.drop(user, source, l)
		return "", unprocessable(r.err)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// finish sends the paste to the user's started login and is how it ended.
func (t *trackerLogins) finish(ctx context.Context, user, source, paste string) (metadata.User, error) {
	t.mu.Lock()
	l := t.pending[user+"/"+source]
	t.mu.Unlock()
	if l == nil {
		return nil, badRequest("no login of %s started (or it waited too long): start it again", source)
	}
	defer t.drop(user, source, l)
	select {
	case l.paste <- paste:
	case r := <-l.done:
		return nil, unprocessable(r.err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-l.done:
		if r.err != nil {
			return nil, unprocessable(r.err)
		}
		return r.user, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *trackerLogins) drop(user, source string, l *trackerLogin) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending[user+"/"+source] == l {
		delete(t.pending, user+"/"+source)
	}
	l.cancel()
}

// trackerUser is the mangal user whose trackers the request has; a token of nobody's has
// none.
func trackerUser(ctx context.Context) (string, error) {
	id, _ := identityOf(ctx)
	if id.Kind == KindToken && id.Owner == "" {
		return "", newAPIError(http.StatusForbidden, CodeForbidden, "an API token of no user has no tracker accounts")
	}
	return core.HistoryUser(ctx), nil
}

// ListMyAccounts implements apiv1.StrictServerInterface.
func (a *API) ListMyAccounts(ctx context.Context, _ apiv1.ListMyAccountsRequestObject) (apiv1.ListMyAccountsResponseObject, error) {
	user, err := trackerUser(ctx)
	if err != nil {
		return nil, err
	}
	list, err := client.Trackers(user)
	if err != nil {
		return nil, err
	}
	out := apiv1.ListMyAccounts200JSONResponse{}
	for _, t := range list {
		o := apiv1.TrackerAccount{Source: t.Source, Name: t.Name, Login: apiv1.TrackerAccountLogin(t.Login), Ready: t.Ready}
		if t.Account != "" {
			o.Account, o.Expired = &t.Account, new(t.Expired())
		}
		if !t.Expires.IsZero() {
			o.Expires = &t.Expires
		}
		out = append(out, o)
	}
	return out, nil
}

// LoginMyAccount implements apiv1.StrictServerInterface.
func (a *API) LoginMyAccount(ctx context.Context, request apiv1.LoginMyAccountRequestObject) (apiv1.LoginMyAccountResponseObject, error) {
	user, err := trackerUser(ctx)
	if err != nil {
		return nil, err
	}
	source := string(request.Source)
	b := deref(request.Body)
	var u metadata.User
	switch {
	case source == "kitsu":
		u, err = kitsu.LoginPasswordFor(ctx, user, deref(b.Name), deref(b.Password))
		if errors.Is(err, metadata.ErrUnauthorized) {
			return nil, unprocessable(errors.New("kitsu: wrong name or password"))
		}
		if err != nil {
			return nil, unprocessable(err)
		}
	case deref(b.Paste) == "":
		url, err := a.trackers.start(ctx, user, source)
		if err != nil {
			return nil, err
		}
		return apiv1.LoginMyAccount200JSONResponse{Url: &url}, nil
	default:
		// its errors are the API's already
		if u, err = a.trackers.finish(ctx, user, source, deref(b.Paste)); err != nil {
			return nil, err
		}
	}
	name := u.Name()
	a.Log.Infof("%s logged into %s as %q", user, source, name)
	return apiv1.LoginMyAccount200JSONResponse{Account: &name}, nil
}

// LogoutMyAccount implements apiv1.StrictServerInterface.
func (a *API) LogoutMyAccount(ctx context.Context, request apiv1.LogoutMyAccountRequestObject) (apiv1.LogoutMyAccountResponseObject, error) {
	user, err := trackerUser(ctx)
	if err != nil {
		return nil, err
	}
	logout := map[apiv1.LogoutMyAccountParamsSource]func(context.Context, string) error{
		"anilist": anilist.LogoutFor, "kitsu": kitsu.LogoutFor, "myanimelist": myanimelist.LogoutFor,
	}[request.Source]
	if err := logout(ctx, user); err != nil {
		return nil, err
	}
	return apiv1.LogoutMyAccount204Response{}, nil
}
