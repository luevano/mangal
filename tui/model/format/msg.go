package format

import "github.com/luevano/libmangal"

type BackMsg struct{}

// AskServerMsg asks to set the server's download format (and this machine's read format if
// Both): it's for everyone on the server.
type AskServerMsg struct {
	Format libmangal.Format
	Both   bool
}

// SetServerMsg is AskServerMsg confirmed.
type SetServerMsg AskServerMsg
