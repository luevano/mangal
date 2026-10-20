package serve

import (
	"context"
	"net/http"

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

// needAdmin refuses who hasn't every permission: only an admin manages the users.
func needAdmin(ctx context.Context) error {
	id, ok := identityOf(ctx)
	if !ok {
		return newAPIError(http.StatusUnauthorized, CodeUnauthorized, "not logged in")
	}
	if !id.admin() {
		return newAPIError(http.StatusForbidden, CodeForbidden, "this needs every permission (an admin), %s hasn't", id)
	}
	return nil
}

func userOut(u core.User) apiv1.User {
	out := apiv1.User{Name: u.Name, Perms: u.Perms, Admin: u.Admin, Created: u.Created}
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
	u, err := a.users().Add(ctx, b.Name, b.Password, deref(b.Perms))
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
	if b.Password == nil && b.Perms == nil {
		return nil, badRequest("nothing to change (password, perms)")
	}
	if b.Perms != nil {
		if err := a.users().SetPerms(ctx, request.Name, *b.Perms); err != nil {
			return nil, err
		}
	}
	if b.Password != nil {
		if err := a.users().SetPassword(ctx, request.Name, *b.Password); err != nil {
			return nil, err
		}
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
	return apiv1.Token{Name: t.Name, Scopes: t.Scopes, Created: t.Created}
}

// ListTokens implements apiv1.StrictServerInterface.
func (a *API) ListTokens(ctx context.Context, _ apiv1.ListTokensRequestObject) (apiv1.ListTokensResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	list, err := a.users().Tokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make(apiv1.ListTokens200JSONResponse, len(list))
	for i, t := range list {
		out[i] = tokenOut(t)
	}
	return out, nil
}

// AddToken implements apiv1.StrictServerInterface.
func (a *API) AddToken(ctx context.Context, request apiv1.AddTokenRequestObject) (apiv1.AddTokenResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	t, secret, err := a.users().AddToken(ctx, request.Body.Name, deref(request.Body.Scopes))
	if err != nil {
		return nil, err
	}
	a.Auth.forgetTokens()
	return apiv1.AddToken201JSONResponse{Token: tokenOut(t), Secret: secret}, nil
}

// RemoveToken implements apiv1.StrictServerInterface.
func (a *API) RemoveToken(ctx context.Context, request apiv1.RemoveTokenRequestObject) (apiv1.RemoveTokenResponseObject, error) {
	if err := needAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.users().RemoveToken(ctx, request.Name); err != nil {
		return nil, err
	}
	a.Auth.forgetTokens()
	return apiv1.RemoveToken204Response{}, nil
}
