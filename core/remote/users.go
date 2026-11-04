package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// done is the error of an answer without a body.
func done(status int, body []byte, e *apiv1.Error) error {
	if status < http.StatusMultipleChoices {
		return nil
	}
	return errorOf(status, body, e)
}

// remoteUsers are the server's users and API tokens (core.Users).
type remoteUsers struct{ c *Client }

func userOf(u apiv1.User) core.User {
	return core.User{Name: u.Name, Perms: u.Perms, Admin: u.Admin, Role: u.Role, Password: u.Password, Local: deref(u.Local),
		Created: u.Created, LastLogin: deref(u.LastLogin)}
}

func tokenOf(t apiv1.Token) core.Token {
	return core.Token{Name: t.Name, Scopes: t.Scopes, Owner: deref(t.Owner), Created: t.Created, LastUsed: deref(t.LastUsed), LastAddress: deref(t.LastAddress)}
}

func (u remoteUsers) List(ctx context.Context) ([]core.User, error) {
	r, err := u.c.API.ListUsersWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.User, len(list))
	for i, e := range list {
		out[i] = userOf(e)
	}
	return out, nil
}

func (u remoteUsers) Add(ctx context.Context, name, password string, perms []string) (core.User, error) {
	if err := CheckPasswordAddress(u.c.URL); err != nil {
		return core.User{}, err
	}
	r, err := u.c.API.AddUserWithResponse(ctx, apiv1.NewUser{Name: name, Password: password, Perms: optList(perms)})
	if err != nil {
		return core.User{}, fmt.Errorf("mangal serve: %w", err)
	}
	added, err := answer(r.StatusCode(), r.Body, r.JSON201, r.JSONDefault)
	return userOf(added), err
}

func (u remoteUsers) update(ctx context.Context, name string, change apiv1.UserChange) error {
	r, err := u.c.API.UpdateUserWithResponse(ctx, name, change)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

func (u remoteUsers) SetPassword(ctx context.Context, name, password string) error {
	if err := CheckPasswordAddress(u.c.URL); err != nil {
		return err
	}
	return u.update(ctx, name, apiv1.UserChange{Password: &password})
}

func (u remoteUsers) SetPerms(ctx context.Context, name string, perms []string) error {
	return u.update(ctx, name, apiv1.UserChange{Perms: &perms})
}

func (u remoteUsers) Rename(ctx context.Context, name, to string) error {
	return u.update(ctx, name, apiv1.UserChange{Name: &to})
}

func (u remoteUsers) UseLocal(ctx context.Context, name string) error {
	return u.update(ctx, name, apiv1.UserChange{Local: new(true)})
}

func (u remoteUsers) EndLogins(ctx context.Context, name string) error {
	r, err := u.c.API.EndUserLoginsWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

func (u remoteUsers) Remove(ctx context.Context, name string) error {
	r, err := u.c.API.RemoveUserWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

func (u remoteUsers) Tokens(ctx context.Context) ([]core.Token, error) {
	r, err := u.c.API.ListTokensWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.Token, len(list))
	for i, t := range list {
		out[i] = tokenOf(t)
	}
	return out, nil
}

// AddToken makes the token; its owner is the server's call (whoever asks, nobody for an
// admin).
func (u remoteUsers) AddToken(ctx context.Context, name, _ string, scopes []string) (core.Token, string, error) {
	r, err := u.c.API.AddTokenWithResponse(ctx, apiv1.NewToken{Name: name, Scopes: optList(scopes)})
	if err != nil {
		return core.Token{}, "", fmt.Errorf("mangal serve: %w", err)
	}
	added, err := answer(r.StatusCode(), r.Body, r.JSON201, r.JSONDefault)
	return tokenOf(added.Token), added.Secret, err
}

func (u remoteUsers) SetTokenScopes(ctx context.Context, name string, scopes []string) error {
	r, err := u.c.API.UpdateTokenWithResponse(ctx, name, apiv1.TokenChange{Scopes: &scopes})
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

func (u remoteUsers) RemoveToken(ctx context.Context, name string) error {
	r, err := u.c.API.RemoveTokenWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

// Permissions are the server's roles and permissions.
func (c *Client) Permissions(ctx context.Context) ([]core.Role, []core.Permission, error) {
	r, err := c.API.ListPermissionsWithResponse(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("mangal serve: %w", err)
	}
	if _, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault); err != nil {
		return nil, nil, err
	}
	var out struct {
		Roles       []core.Role       `json:"roles"`
		Permissions []core.Permission `json:"permissions"`
	}
	return out.Roles, out.Permissions, json.Unmarshal(r.Body, &out)
}

// trackers are the trackers the API has logins for, by the name in its paths.
var trackers = map[string]metadata.IDSource{
	"anilist": metadata.IDSourceAnilist, "kitsu": metadata.IDSourceKitsu, "myanimelist": metadata.IDSourceMyAnimeList,
}

// remoteAccounts are the accounts this token may see (core.Accounts): its user's trackers
// (the logins of a remote TUI) and, for an admin, the server's other accounts as it last
// checked them. Nothing is asked that it can't get.
type remoteAccounts struct{ c *Client }

func (a remoteAccounts) List(ctx context.Context, _ core.AccountsOptions) ([]core.Account, error) {
	me, err := a.c.Me(ctx)
	if err != nil {
		return nil, err
	}
	var out []core.Account
	if slices.Contains(me.Scopes, core.ScopeProgress) {
		if me.Kind == apiv1.MeKindToken && deref(me.Owner) == "" {
			if !me.Admin {
				out = append(out, core.Account{Account: metadata.Account{Service: "Trackers",
					Details: []string{"an API token of no user has no tracker accounts"}}})
			}
		} else if out, err = a.trackers(ctx); err != nil {
			return nil, err
		}
	}
	if !me.Admin {
		return out, nil
	}
	r, err := a.c.API.ListAccountsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		// the server's own login of a tracker row above: the user's is what they change
		if slices.ContainsFunc(out, func(t core.Account) bool { return strings.EqualFold(t.Service, e.Service) }) {
			continue
		}
		out = append(out, core.Account{Account: metadata.Account{Service: e.Service, User: deref(e.User), URL: deref(e.Url), Status: e.Status,
			Expires: deref(e.Expires), Details: deref(e.Details)}, Provider: deref(e.Provider), Checked: deref(e.Checked), Error: deref(e.Error)})
	}
	return out, nil
}

func (a remoteAccounts) trackers(ctx context.Context) ([]core.Account, error) {
	r, err := a.c.API.ListMyAccountsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.Account, len(list))
	for i, t := range list {
		acc := metadata.Account{Service: t.Name, User: deref(t.Account), Expires: deref(t.Expires), Status: "logged out"}
		switch {
		case !t.Ready:
			acc.Status, acc.Details = "not set up", []string{"needs the server's API client, an admin sets it"}
		case deref(t.Expired):
			acc.Status = "expired"
		case t.Account != nil:
			acc.Status = "logged in"
		}
		out[i] = core.Account{Account: acc, Metadata: trackers[t.Source]}
	}
	return out, nil
}

// TrackerSource is the API's name of the tracker.
func TrackerSource(source metadata.IDSource) string {
	for name, s := range trackers {
		if s == source {
			return name
		}
	}
	return ""
}

// TrackerLogin logs the token's user into the tracker: AniList and MyAnimeList without
// Paste answer the address to open, then Paste is what the page ends on; Kitsu takes Name
// and Password (not kept).
func (c *Client) TrackerLogin(ctx context.Context, source string, login apiv1.TrackerLogin) (authorize string, err error) {
	r, err := c.API.LoginMyAccountWithResponse(ctx, apiv1.LoginMyAccountParamsSource(source), login)
	if err != nil {
		return "", fmt.Errorf("mangal serve: %w", err)
	}
	answered, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return deref(answered.Url), err
}

// TrackerLogout logs the token's user out of the tracker.
func (c *Client) TrackerLogout(ctx context.Context, source string) error {
	r, err := c.API.LogoutMyAccountWithResponse(ctx, apiv1.LogoutMyAccountParamsSource(source))
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	return done(r.StatusCode(), r.Body, r.JSONDefault)
}

func (a remoteAccounts) APIClients(ctx context.Context) ([]core.APIClient, error) {
	r, err := a.c.API.ListAPIClientsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]core.APIClient, len(list))
	for i, c := range list {
		out[i] = core.APIClient{Source: string(c.Source), Name: c.Name, ID: deref(c.Id), Secret: c.Secret,
			Pin: deref(c.Redirect) == apiv1.APIClientRedirectPin, RedirectURL: c.RedirectUrl, RegisterURL: c.RegisterUrl}
	}
	return out, nil
}

func (a remoteAccounts) SetAPIClient(ctx context.Context, source, id string, secret *string) error {
	if id == "" {
		r, err := a.c.API.RemoveAPIClientWithResponse(ctx, source)
		if err != nil {
			return fmt.Errorf("mangal serve: %w", err)
		}
		return done(r.StatusCode(), r.Body, r.JSONDefault)
	}
	r, err := a.c.API.SetAPIClientWithResponse(ctx, source, apiv1.APIClientSet{Id: id, Secret: secret})
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	_, err = answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return err
}
