package icon

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/style"
)

type icon struct {
	color   lipgloss.TerminalColor
	symbols symbols
}

type symbols map[Type]string

func (i icon) String() string {
	return i.Raw() // by default return the uncolored version
}

func (i icon) Colored() string {
	return style.Bold.Base.Foreground(i.color).Render(i.symbols[currentType])
}

func (i icon) Raw() string {
	return i.symbols[currentType]
}

var (
	Progress = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "@",
			TypeNerd:  "\U000F0997",
		},
	}

	Timer = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "¿",
			TypeNerd:  "\U000F13AB",
		},
	}

	Mark = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "*",
			TypeNerd:  "\U000F0F22",
		},
	}

	Download = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "#",
			TypeNerd:  "\uF019",
		},
	}

	Check = icon{
		color: color.Success,
		symbols: symbols{
			TypeASCII: "~",
			TypeNerd:  "\uF05D",
		},
	}

	Cross = icon{
		color: color.Error,
		symbols: symbols{
			TypeASCII: "x",
			TypeNerd:  "\uF05C",
		},
	}

	Search = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: ">",
			TypeNerd:  "\uF002",
		},
	}

	Available = icon{
		color: color.Secondary,
		symbols: symbols{
			TypeASCII: "a",
			TypeNerd:  "\uEB28",
		},
	}

	Read = icon{
		color: color.Success,
		symbols: symbols{
			TypeASCII: "r",
			TypeNerd:  "\uF06E",
		},
	}

	Filter = icon{
		color: color.Warning,
		symbols: symbols{
			TypeASCII: "f",
			TypeNerd:  "\uF0B0",
		},
	}

	Item = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: ">",
			TypeNerd:  "\uF101",
		},
	}

	SubItem = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "-",
			TypeNerd:  "\uF105",
		},
	}

	Ellipsis = icon{
		color: lipgloss.NoColor{},
		symbols: symbols{
			TypeASCII: "_",
			TypeNerd:  "…", // not really nerd font
		},
	}

	Separator = icon{
		color: color.Warning,
		symbols: symbols{
			TypeASCII: "-",
			TypeNerd:  "•", // not really nerd font
		},
	}
)
