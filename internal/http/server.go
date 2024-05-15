// internal/http/server.go
package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/models"
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
				isUserGroupMember := isUserGroupMember(update.Message.From.ID, update.Message.Chat.ID, update.Message.From.FirstName)

				if !isUserGroupMember {
					// reply to user "You are not a member of this group"
					sendMessageInReplyToPost(update.Message.Chat.ID, "URL was sent by non member. User ID is "+strconv.FormatInt(update.Message.From.ID, 10)+" user name is \""+update.Message.From.FirstName+"\" username is @"+update.Message.From.Username, update.Message.MessageID)
					// delete the message
					deleteMessage(update.Message.Chat.ID, update.Message.MessageID)

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

// logRequest is a middleware that logs details about each incoming request.
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request details
		start := time.Now()
		log.Printf("Received %s request for %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		log.Printf("Request headers: %v", r.Header)

		// Call the next handler in the chain, which will handle body reading and logging
		next.ServeHTTP(w, r)

		// Optionally, log the time taken to serve the request
		log.Printf("Served %s request for %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
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
	// This regex pattern is basic and might need refinement to accurately match all URLs
	regexPattern := `\b((?:https?://)?[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\b`
	re := regexp.MustCompile(regexPattern)
	matches := re.FindAllString(s, -1)

	validURLs := make([]string, 0)
	for _, match := range matches {
		// Extract the TLD from the URL
		tld := regexp.MustCompile(`\.([a-zA-Z]{2,})$`).FindStringSubmatch(match)
		if len(tld) > 1 {
			if _, ok := tlds[tld[1]]; ok {
				// The TLD is valid
				validURLs = append(validURLs, match)
			}
		}
	}

	return validURLs
}

func isUserGroupMember(userId int64, chatId int64, firstName string) bool {

	// get token from env
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")

	// create a new request
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot"+token+"/getChatMember?chat_id="+strconv.FormatInt(chatId, 10)+"&user_id="+strconv.FormatInt(userId, 10), nil)

	if err != nil {
		log.Printf("Error creating request: %v", err)
	}

	// send the request
	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error sending request: %v", err)
	}

	// read the response
	body, err := io.ReadAll(resp.Body)

	// allowed roles "member", "administrator", "creator"
	if err != nil {
		log.Printf("Error reading response: %v", err)
	}

	// log the response
	log.Printf("Response: %s", string(body))

	// parse the response
	var response map[string]interface{}

	if err := json.Unmarshal(body, &response); err != nil {
		log.Printf("Error parsing response: %v", err)
	}

	resultResponse := response["result"].(map[string]interface{})
	status := resultResponse["status"].(string)

	if status == "member" || status == "administrator" || status == "creator" || firstName == "Telegram" {
		return true
	}

	return false
}

func deleteMessage(chatId int64, messageId int64) {
	// get token from env
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")

	// create a new request
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot"+token+"/deleteMessage?chat_id="+strconv.FormatInt(chatId, 10)+"&message_id="+strconv.FormatInt(messageId, 10), nil)

	if err != nil {
		log.Printf("Error creating request: %v", err)
	}

	// send the request
	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error sending request: %v", err)
	}

	// read the response
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Printf("Error reading response: %v", err)
	}

	// log the response
	log.Printf("Response: %s", string(body))
}

func sendMessageInReplyToPost(chatId int64, text string, messageId int64) {
	// get token from env
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")

	// create a new request
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot"+token+"/sendMessage?chat_id="+strconv.FormatInt(chatId, 10)+"&text="+text+"&reply_to_message_id="+strconv.FormatInt(messageId, 10), nil)

	if err != nil {
		log.Printf("Error creating request: %v", err)
	}

	// send the request
	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error sending request: %v", err)
	}

	// read the response
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Printf("Error reading response: %v", err)
	}

	// log the response
	log.Printf("Response: %s", string(body))
}
