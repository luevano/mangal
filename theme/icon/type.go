package icon

//go:generate go tool enumer -type=Type -trimprefix=Type -json -yaml -text
type Type uint8

const (
	TypeNerd Type = iota + 1
	TypeASCII
)

var currentType Type = TypeASCII

func SetType(iconType Type) {
	currentType = iconType
}
