// pkg/types/trusted_username.go

package types

type TrustedUsername string

const (
	GroupAnonymousBot TrustedUsername = "GroupAnonymousBot"
	ChannelBot        TrustedUsername = "Channel_Bot"
)

var TrustedUsernames = []TrustedUsername{
	GroupAnonymousBot,
	ChannelBot,
}
