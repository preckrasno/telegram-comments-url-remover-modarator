// internal/http/server.go

package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/models"
	"time"
)

var deleteTimers = sync.Map{}
var sentOwnBotQuestionIds = sync.Map{}

var debugRepliesInChat = true

func StartServer(port string) {
	mux := http.NewServeMux()

	// Register your webhook handlers
	mux.HandleFunc("/", telegramWebhookHandler)

	// echo handler for testing
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Hello, World!"})
	})

	loggedMux := logRequest(mux)

	certPath := "certs/YOURPUBLIC.pem" // for build
	// certPath := "certs/public.pem" // for local development
	keyPath := "certs/YOURPRIVATE.key" // for build
	// keyPath := "certs/private.key" // for local development

	log.Printf("Listening on https://localhost:%s", port)
	err := http.ListenAndServeTLS(":"+port, certPath, keyPath, loggedMux)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func logRequest(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL)
		mux.ServeHTTP(w, r)
	})
}

func telegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != "telegram-moderator" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("Request body: %s", string(bodyBytes))

	var update models.Update
	if err := json.Unmarshal(bodyBytes, &update); err != nil {
		http.Error(w, "Error parsing update", http.StatusBadRequest)
		log.Printf("Error parsing update: %v", err)
		return
	}

	if update.Message != nil {
		sendDebugMessage(update.Message.Chat.ID, fmt.Sprintf("Received message: %s", update.Message.MessageText))
		handleMessage(update.Message)
	} else if update.CallbackQuery != nil {
		sendDebugMessage(update.CallbackQuery.Message.Chat.ID, fmt.Sprintf("Received callback query: %s", update.CallbackQuery.Data))
		handleCallbackQuery(update.CallbackQuery)
	}

	response := struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{
		Status:  "success",
		Message: "Update received",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error sending response: %v", err)
	}
}

func handleMessage(message *models.Message) {
	if message.From.ID != 0 && message.MessageText != "" {
		log.Printf("Message text: %s", message.MessageText)

		tldURL := "https://raw.githubusercontent.com/umpirsky/tld-list/master/data/en/tld.json"
		tlds, err := FetchTLDs(tldURL)
		if err != nil {
			log.Printf("Error fetching TLDs: %v", err)
			sendDebugMessage(message.Chat.ID, "Error fetching TLDs")
			return
		}

		validURLs := CheckURLsInString(message.MessageText, tlds)
		log.Printf("Valid URLs: %v", validURLs)

		if len(validURLs) > 0 {
			isUserGroupMember := isUserGroupMember(message.From.ID, message.Chat.ID, message.From.FirstName, message.From.Username)
			if !isUserGroupMember {
				sendDebugMessage(message.Chat.ID, "User is not a group member, user message id is "+strconv.FormatInt(message.MessageID, 10))
				botQuestionMessageId := sendBotVerificationQuestionMessage(message.Chat.ID, message.MessageID)
				if botQuestionMessageId != 0 {
					go startDeleteTimer(message.Chat.ID, message.MessageID, botQuestionMessageId)
				}
			}
		}
	}
}

func handleCallbackQuery(callbackQuery *models.CallbackQuery) {
	answer := callbackQuery.Data
	userMessageId := callbackQuery.Message.ReplyToMessage.MessageID

	// Stop the timer if it exists
	if timer, ok := deleteTimers.Load(userMessageId); ok {
		timer.(*time.Timer).Stop()
		deleteTimers.Delete(userMessageId)
	}

	sendDebugMessage(callbackQuery.Message.Chat.ID, fmt.Sprintf("Received callback query: %s", answer))

	if botQuestionId, ok := sentOwnBotQuestionIds.Load(userMessageId); ok {
		deleteMessage(callbackQuery.Message.Chat.ID, botQuestionId.(int64))
		sentOwnBotQuestionIds.Delete(userMessageId)
	}

	if answer == "5" {
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Correct answer received")
		sendMessage(callbackQuery.Message.Chat.ID, userMessageId, "Correct! You are not a spammer.")
	} else {
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Wrong answer received, deleting message.")
		deleteMessage(callbackQuery.Message.Chat.ID, userMessageId)
	}
}

func sendBotVerificationQuestionMessage(chatId int64, messageId int64) int64 {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	text := "Are you a spammer? If not, solve 3 plus 2"
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d&reply_markup=%s",
		token, chatId, text, messageId, generateInlineKeyboardMarkup())

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending verification message: %v", err)
		sendDebugMessage(chatId, "Error sending verification message")
		return 0
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		sendDebugMessage(chatId, "Error reading verification message response")
		return 0
	}

	var result struct {
		Ok     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("Error parsing response: %v", err)
		sendDebugMessage(chatId, "Error parsing verification message response")
		return 0
	}

	if result.Ok {
		sentOwnBotQuestionIds.Store(messageId, result.Result.MessageID)
		sendDebugMessage(chatId, fmt.Sprintf("Sent bot verification question message, message id is %d", result.Result.MessageID))
		return result.Result.MessageID
	}

	log.Printf("Verification message response: %s", string(body))
	sendDebugMessage(chatId, fmt.Sprintf("Verification message response: %s", string(body)))
	return 0
}

func generateInlineKeyboardMarkup() string {
	buttons := [][]map[string]string{
		{
			{"text": "5", "callback_data": "5"},
			{"text": "6", "callback_data": "6"},
		},
	}
	replyMarkup := map[string][][]map[string]string{"inline_keyboard": buttons}
	markupJSON, _ := json.Marshal(replyMarkup)
	return string(markupJSON)
}

func startDeleteTimer(chatId int64, userMessageId int64, botQuestionMessageId int64) {
	timer := time.NewTimer(30 * time.Second)
	deleteTimers.Store(userMessageId, timer)
	<-timer.C

	sendDebugMessage(chatId, "Timeout reached, deleting messages")

	deleteMessage(chatId, userMessageId)
	deleteMessage(chatId, botQuestionMessageId)
	deleteTimers.Delete(userMessageId)
	sentOwnBotQuestionIds.Delete(userMessageId)
}

func sendMessage(chatId int64, messageId int64, text string) {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d",
		token, chatId, text, messageId)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending message: %v", err)
		sendDebugMessage(chatId, "Error sending message")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		sendDebugMessage(chatId, "Error reading send message response")
		return
	}

	log.Printf("Send message response: %s", string(body))
	sendDebugMessage(chatId, fmt.Sprintf("Send message response: %s", string(body)))
}

func deleteMessage(chatId int64, messageId int64) {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage?chat_id=%d&message_id=%d",
		token, chatId, messageId)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error deleting message: %v", err)
		sendDebugMessage(chatId, "Error deleting message")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		sendDebugMessage(chatId, "Error reading delete message response")
		return
	}

	log.Printf("Delete message response: %s", string(body))
	sendDebugMessage(chatId, fmt.Sprintf("Delete message response: %s, message id is %d", string(body), messageId))
}
