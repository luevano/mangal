package prompt

import (
	luadoc "github.com/luevano/gopher-luadoc"
	"github.com/luevano/mangal/script/lib/prompt/fzf"
)

const libName = "prompt"

func Lib() *luadoc.Lib {
	return &luadoc.Lib{
		Name:        libName,
		Description: "Various prompts for interracting with the user",
		Libs: []*luadoc.Lib{
			fzf.Lib(),
		},
	}
}
