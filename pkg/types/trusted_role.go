// pkg/types/trusted_role.go

package types

type TrustedRole string

const (
	Member        TrustedRole = "member"
	Administrator TrustedRole = "administrator"
	Creator       TrustedRole = "creator"
)

var TrustedRoles = []TrustedRole{
	Member,
	Administrator,
	Creator,
}
