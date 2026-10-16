package web

import (
	"net/http"

	"github.com/luevano/mangal/v5/provider/loader"
)

// imageClient is the image proxy's client, guarded as the providers' (loader.Guard) so a
// page can't use it to reach the LAN (the router, FlareSolverr) or the host itself.
func imageClient() *http.Client {
	return loader.ProviderHTTPClient()
}
