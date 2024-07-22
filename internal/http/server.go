// internal/http/server.go

package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/models"
	"telegram_moderator/pkg/types"
	"time"
)

var deleteTimers = sync.Map{}
var sentOwnBotQuestionIds = sync.Map{}

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
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	log.Printf("Request body: %s", string(bodyBytes))

	var update models.Update
	if err := json.Unmarshal(bodyBytes, &update); err != nil {
		http.Error(w, "Error parsing update", http.StatusBadRequest)
		log.Printf("Error parsing update: %v", err)
		return
	}

	// Process the update
	if update.Message != nil {
		handleMessage(update.Message)
	} else if update.CallbackQuery != nil {
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
			return
		}

		validURLs := CheckURLsInString(message.MessageText, tlds)
		log.Printf("Valid URLs: %v", validURLs)

		if len(validURLs) > 0 {
			isUserGroupMember := isUserGroupMember(message.From.ID, message.Chat.ID, message.From.FirstName, message.From.Username)
			if !isUserGroupMember {
				botQuestionMessageId := sendVerificationMessage(message.Chat.ID, message.MessageID)
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

	if botQuestionId, ok := sentOwnBotQuestionIds.Load(userMessageId); ok {
		deleteMessage(callbackQuery.Message.Chat.ID, botQuestionId.(int64))
		sentOwnBotQuestionIds.Delete(userMessageId)
	}

	if answer == "5" {
		// Correct answer, send confirmation message
		sendMessage(callbackQuery.Message.Chat.ID, userMessageId, "Correct! You are not a spammer.")
	} else {
		// Wrong answer, delete the original user message
		deleteMessage(callbackQuery.Message.Chat.ID, userMessageId)
	}
}

func sendVerificationMessage(chatId int64, messageId int64) int64 {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	text := "Are you a spammer? If not, solve 3 + 2"
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d&reply_markup=%s",
		token, chatId, text, messageId, generateInlineKeyboardMarkup())

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending verification message: %v", err)
		return 0
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
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
		return 0
	}

	if result.Ok {
		sentOwnBotQuestionIds.Store(messageId, result.Result.MessageID)
		return result.Result.MessageID
	}

	log.Printf("Verification message response: %s", string(body))
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
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return
	}

	log.Printf("Send message response: %s", string(body))
}

func deleteMessage(chatId int64, messageId int64) {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage?chat_id=%d&message_id=%d",
		token, chatId, messageId)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error deleting message: %v", err)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return
	}

	log.Printf("Delete message response: %s", string(body))
}

func FetchTLDs(url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tlds map[string]string
	err = json.Unmarshal(body, &tlds)
	if err != nil {
		return nil, err
	}

	return tlds, nil
}

func CheckURLsInString(s string, tlds map[string]string) []string {
	regexPattern := `\b((?:https?://)?[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\b`
	re := regexp.MustCompile(regexPattern)
	matches := re.FindAllString(s, -1)

	validURLs := make([]string, 0)
	for _, match := range matches {
		tld := regexp.MustCompile(`\.([a-zA-Z]{2,})$`).FindStringSubmatch(match)
		if len(tld) > 1 {
			if _, ok := tlds[tld[1]]; ok {
				validURLs = append(validURLs, match)
			}
		}
	}

	return validURLs
}

func isUserGroupMember(userId int64, chatId int64, firstName string, username string) bool {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")

	req, err := http.NewRequest("GET", "https://api.telegram.org/bot"+token+"/getChatMember?chat_id="+strconv.FormatInt(chatId, 10)+"&user_id="+strconv.FormatInt(userId, 10), nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return false
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending request: %v", err)
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return false
	}

	log.Printf("Response: %s", string(body))

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		log.Printf("Error parsing response: %v", err)
		return false
	}

	resultResponse := response["result"].(map[string]interface{})
	status := resultResponse["status"].(string)

	return checkIfTrustedSender(status, firstName, username)
}

func checkIfTrustedSender(status string, firstName string, usernameArg string) bool {
	for _, role := range types.TrustedRoles {
		if status == string(role) {
			return true
		}
	}

	for _, name := range types.TrustedNames {
		if firstName == string(name) {
			return true
		}
	}

	for _, username := range types.TrustedUsernames {
		if usernameArg == string(username) {
			return true
		}
	}

	return false
}
