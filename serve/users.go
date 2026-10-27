package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// users are the users and API tokens the admin page manages.
func (a *API) users() core.Users {
	if a.Users != nil {
		return a.Users
	}
	return core.NewUsers()
}

// needAdmin refuses who isn't an admin.
func needAdmin(ctx context.Context) error {
	id, ok := identityOf(ctx)
	if !ok {
		return newAPIError(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
	}
	if !id.admin() {
		return newAPIError(http.StatusForbidden, CodeForbidden, "this needs the admin permission, %s hasn't it", id)
	}
	return nil
}

func userOut(u core.User) apiv1.User {
	out := apiv1.User{Name: u.Name, Perms: u.Perms, Role: u.Role, Password: u.Password, Admin: u.Admin, Created: u.Created}
	if !u.LastLogin.IsZero() {
		out.LastLogin = &u.LastLogin
	}
	return out
}

// ListUsers implements apiv1.StrictServerInterface.
func (a *API) ListUsers(ctx context.Context, _ apiv1.ListUsersRequestObject) (apiv1.ListUsersResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	list, err := a.users().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(apiv1.ListUsers200JSONResponse, len(list))
	for i, u := range list {
		out[i] = userOut(u)
	}
	return out, nil
}

// AddUser implements apiv1.StrictServerInterface.
func (a *API) AddUser(ctx context.Context, request apiv1.AddUserRequestObject) (apiv1.AddUserResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	b := request.Body
	perms, err := core.WithRole(deref(b.Role), deref(b.Perms))
	if err != nil {
		return nil, badRequest("%w", err)
	}
	u, err := a.users().Add(ctx, b.Name, b.Password, perms)
	if err != nil {
		return nil, err
	}
	return apiv1.AddUser201JSONResponse(userOut(u)), nil
}

// UpdateUser implements apiv1.StrictServerInterface.
func (a *API) UpdateUser(ctx context.Context, request apiv1.UpdateUserRequestObject) (apiv1.UpdateUserResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	b := request.Body
	if b.Password == nil && b.Perms == nil && b.Role == nil && b.Name == nil {
		return nil, badRequest("nothing to change (name, password, role, perms)")
	}
	if b.Perms != nil || b.Role != nil {
		perms, err := core.WithRole(deref(b.Role), deref(b.Perms))
		if err != nil {
			return nil, badRequest("%w", err)
		}
		if err := a.users().SetPerms(ctx, request.Name, perms); err != nil {
			return nil, err
		}
	}
	if b.Password != nil {
		if err := a.users().SetPassword(ctx, request.Name, *b.Password); err != nil {
			return nil, err
		}
	}
	if b.Name != nil {
		if err := a.users().Rename(ctx, request.Name, *b.Name); err != nil {
			return nil, err
		}
		a.Auth.forget(*b.Name)
	}
	a.Auth.forget(request.Name)
	return apiv1.UpdateUser204Response{}, nil
}

// RemoveUser implements apiv1.StrictServerInterface.
func (a *API) RemoveUser(ctx context.Context, request apiv1.RemoveUserRequestObject) (apiv1.RemoveUserResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.users().Remove(ctx, request.Name); err != nil {
		return nil, err
	}
	a.Auth.forget(request.Name)
	return apiv1.RemoveUser204Response{}, nil
}

// EndUserLogins implements apiv1.StrictServerInterface.
func (a *API) EndUserLogins(ctx context.Context, request apiv1.EndUserLoginsRequestObject) (apiv1.EndUserLoginsResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.users().EndLogins(ctx, request.Name); err != nil {
		return nil, err
	}
	a.Auth.forget(request.Name)
	return apiv1.EndUserLogins204Response{}, nil
}

func tokenOut(t core.Token) apiv1.Token {
	out := apiv1.Token{Name: t.Name, Scopes: t.Scopes, Role: core.RoleOf(t.Scopes), Created: t.Created}
	if t.Owner != "" {
		out.Owner = &t.Owner
	}
	return out
}

// tokenOwner is whose tokens the request manages: "" for an admin (every token, a new one
// is the server's), else its user's (the tokens permission: only their own).
func tokenOwner(ctx context.Context) string {
	id, _ := identityOf(ctx)
	if id.admin() {
		return ""
	}
	return id.user()
}

// ListTokens implements apiv1.StrictServerInterface.
func (a *API) ListTokens(ctx context.Context, _ apiv1.ListTokensRequestObject) (apiv1.ListTokensResponseObject, error) {
	list, err := a.users().Tokens(ctx)
	if err != nil {
		return nil, err
	}
	owner := tokenOwner(ctx)
	out := apiv1.ListTokens200JSONResponse{}
	for _, t := range list {
		if owner == "" || t.Owner == owner {
			out = append(out, tokenOut(t))
		}
	}
	return out, nil
}

// AddToken implements apiv1.StrictServerInterface.
func (a *API) AddToken(ctx context.Context, request apiv1.AddTokenRequestObject) (apiv1.AddTokenResponseObject, error) {
	b := request.Body
	scopes, err := core.WithRole(deref(b.Role), deref(b.Scopes))
	if err != nil {
		return nil, badRequest("%w", err)
	}
	owner := tokenOwner(ctx)
	if strings.HasPrefix(owner, "token:") {
		return nil, newAPIError(http.StatusForbidden, CodeForbidden, "only a user's or an admin's tokens make tokens")
	}
	if owner != "" {
		id, _ := identityOf(ctx)
		want, err := core.NormalizeScopes(scopes)
		if err != nil {
			return nil, badRequest("%w", err)
		}
		if more := slices.DeleteFunc(want, id.has); len(more) != 0 {
			return nil, newAPIError(http.StatusForbidden, CodeForbidden, "a token can't have more than %s: %s", id, strings.Join(more, ", "))
		}
	}
	t, secret, err := a.users().AddToken(ctx, b.Name, owner, scopes)
	if err != nil {
		return nil, err
	}
	a.Auth.forgetTokens()
	return apiv1.AddToken201JSONResponse{Token: tokenOut(t), Secret: secret}, nil
}

// RemoveToken implements apiv1.StrictServerInterface.
func (a *API) RemoveToken(ctx context.Context, request apiv1.RemoveTokenRequestObject) (apiv1.RemoveTokenResponseObject, error) {
	if owner := tokenOwner(ctx); owner != "" {
		list, err := a.users().Tokens(ctx)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(list, func(t core.Token) bool { return t.Name == request.Name && t.Owner == owner }) {
			return nil, &core.NotFoundError{What: fmt.Sprintf("no token %q of yours", request.Name)}
		}
	}
	if err := a.users().RemoveToken(ctx, request.Name); err != nil {
		return nil, err
	}
	a.Auth.forgetTokens()
	return apiv1.RemoveToken204Response{}, nil
}

// ListPermissions implements apiv1.StrictServerInterface.
func (a *API) ListPermissions(context.Context, apiv1.ListPermissionsRequestObject) (apiv1.ListPermissionsResponseObject, error) {
	var out apiv1.ListPermissions200JSONResponse
	b, err := json.Marshal(map[string]any{"permissions": core.Permissions, "roles": core.Roles})
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(b, &out)
}
