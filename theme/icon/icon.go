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
	Confirm = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "?",
			TypeNerd:  "\uEB32",
		},
	}

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

	Recent = icon{
		color: color.Secondary,
		symbols: symbols{
			TypeASCII: "~",
			TypeNerd:  "\uF017",
		},
	}

	Read = icon{
		color: color.Secondary,
		symbols: symbols{
			TypeASCII: "r",
			TypeNerd:  "\U000F0447",
		},
	}

	Available = icon{
		color: color.Secondary,
		symbols: symbols{
			TypeASCII: "a",
			TypeNerd:  "\uEB28",
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

	LeftHardDivider = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: ">",
			TypeNerd:  "\uE0B0",
		},
	}

	LeftSoftDivider = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: ">",
			TypeNerd:  "\uE0B1",
		},
	}

	RightHardDivider = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "<",
			TypeNerd:  "\uE0B2",
		},
	}

	RightSoftDivider = icon{
		color: color.Accent,
		symbols: symbols{
			TypeASCII: "<",
			TypeNerd:  "\uE0B3",
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

	BreadcrumbSep = icon{
		color: color.Warning,
		symbols: symbols{
			TypeASCII: "/",
			TypeNerd:  "/", // not really nerd font
		},
	}
)
