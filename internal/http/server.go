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
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/models"
	"telegram_moderator/pkg/types"
	"time"
)

func StartServer(port string) {
	mux := http.NewServeMux()

	// Register your webhook handler
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

func telegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// “X-Telegram-Bot-Api-Secret-Token” header != "telegram-moderator"
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != "telegram-moderator" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Read the body once into bytes
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {

		log.Printf("Failed to read request body: %v", err)
		return
	}
	// Replace the request body for future reads
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Log the body
	log.Printf("Request body: %s", string(bodyBytes))

	var update models.Update
	if err := json.Unmarshal(bodyBytes, &update); err != nil {
		http.Error(w, "Error parsing update", http.StatusBadRequest)
		log.Printf("Error parsing update: %v", err)
		return
	}

	if update.Message.From.ID != 0 {
		// check if message has link/url
		if update.Message.MessageText != "" {
			log.Printf("Message text: %s", update.Message.MessageText)

			tldURL := "https://raw.githubusercontent.com/umpirsky/tld-list/master/data/en/tld.json"
			tlds, err := FetchTLDs(tldURL)

			if err != nil {
				log.Printf("Error fetching TLDs: %v", err)
			}

			validURLs := CheckURLsInString(update.Message.MessageText, tlds)
			log.Printf("Valid URLs: %v", validURLs)

			if len(validURLs) > 0 {
				// check if user is already in group calling getChatMember
				isUserGroupMember := isUserGroupMember(update.Message.From.ID, update.Message.Chat.ID, update.Message.From.FirstName, update.Message.From.Username)

				if !isUserGroupMember {
					sendVerificationMessage(update.Message.Chat.ID, update.Message.From.ID, update.Message.MessageID)
					go startDeleteTimer(update.Message.Chat.ID, update.Message.MessageID)
				}

			}

		}
	}

	// Respond to the request indicating success
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

func sendVerificationMessage(chatId int64, userId int64, messageId int64) {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	text := "Are you a spammer? If not, solve 3 + 2"
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d&reply_markup=%s",
		token, chatId, text, messageId, generateInlineKeyboardMarkup())

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending verification message: %v", err)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		return
	}

	log.Printf("Verification message response: %s", string(body))
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

func startDeleteTimer(chatId int64, messageId int64) {
	time.Sleep(30 * time.Second)
	deleteMessage(chatId, messageId)
}

func handleCallbackQuery(w http.ResponseWriter, r *http.Request) {
	var update models.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Error parsing callback query", http.StatusBadRequest)
		return
	}

	callbackQuery := update.CallbackQuery
	if callbackQuery != nil {
		answer := callbackQuery.Data
		if answer == "5" {
			// Correct answer, do nothing
			return
		} else {
			// Wrong answer, delete the original message
			deleteMessage(callbackQuery.Message.Chat.ID, callbackQuery.Message.MessageID)
		}
	}
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

	// get token from env
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")

	// create a new request
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot"+token+"/getChatMember?chat_id="+strconv.FormatInt(chatId, 10)+"&user_id="+strconv.FormatInt(userId, 10), nil)

	if err != nil {
		log.Printf("Error creating request: %v", err)
		// return false
	}

	// send the request
	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error sending request: %v", err)
		// return false
	}
	defer resp.Body.Close()

	// read the response
	body, err := io.ReadAll(resp.Body)

	// allowed roles "member", "administrator", "creator"
	if err != nil {
		log.Printf("Error reading response: %v", err)
		// return false
	}

	// log the response
	log.Printf("Response: %s", string(body))

	// parse the response
	var response map[string]interface{}

	if err := json.Unmarshal(body, &response); err != nil {
		log.Printf("Error parsing response: %v", err)
		// return false
	}

	resultResponse := response["result"].(map[string]interface{})
	status := resultResponse["status"].(string)

	isTrustedSender := checkIfTrustedSender(status, firstName, username)

	return isTrustedSender
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
