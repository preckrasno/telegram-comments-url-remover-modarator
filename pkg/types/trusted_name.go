// pkg/types/trusted_name.go

package types

type TrustedName string

const (
	Telegram TrustedName = "Telegram"
)

var TrustedNames = []TrustedName{
	Telegram,
}
