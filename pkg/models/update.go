// pkg/models/update.go

package models

type Update struct {
	UpdateID      int64             `json:"update_id"`
	Message       *Message          `json:"message,omitempty"`
	MyChatMember  *ChatMemberUpdate `json:"my_chat_member,omitempty"`
	CallbackQuery *CallbackQuery    `json:"callback_query,omitempty"`
}

type ChatMemberUpdate struct {
	Chat          Chat       `json:"chat"`
	From          User       `json:"from"`
	Date          int64      `json:"date"`
	OldChatMember ChatMember `json:"old_chat_member"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

type CallbackQuery struct {
	ID            int64   `json:"id"`
	From          User    `json:"from"`
	Message       Message `json:"message"`
	Data          string  `json:"data"`
	GameShortName string  `json:"game_short_name"`
}

type ChatMember struct {
	User   User   `json:"user"`
	Status string `json:"status"`
}

type Message struct {
	MessageID      int64          `json:"message_id"`
	From           User           `json:"from"`
	Chat           Chat           `json:"chat"`
	Date           int64          `json:"date"`
	NewChatMember  *User          `json:"new_chat_member,omitempty"`
	NewChatMembers []User         `json:"new_chat_members,omitempty"`
	LeftChatMember *User          `json:"left_chat_member,omitempty"`
	MessageText    string         `json:"text"`
	SenderChat     SenderChat     `json:"sender_chat"`
	ReplyToMessage ReplyToMessage `json:"reply_to_message"`
}

type ReplyToMessage struct {
	MessageID int64 `json:"message_id"`
}

type SenderChat struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Type     string `json:"type"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}
