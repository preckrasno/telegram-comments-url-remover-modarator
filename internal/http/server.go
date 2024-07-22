// internal/http/server.go

package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/models"
	"time"
)

// example of map: deleteTimers.Store(userMessageId, timer)
var deleteTimers = sync.Map{}

// example of map: sentOwnBotQuestionIds.Store(userMessageId, botQuestionMessageId)
var sentOwnBotQuestionIds = sync.Map{}

var userSentMessageInPostList []map[string]string

// example of map: userNeededAnswersList.Store(userMessageId string, neededAnswer int)
var userNeededAnswersList = sync.Map{}

var debugRepliesInChat = false

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
		handleCallbackQuery(update.CallbackQuery, update.CallbackQuery.Message.MessageID)
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
				// save user id, username and first name, post id where user sent message in order to send message in reply to post
				userSentMessageInPost := map[string]string{
					"userMessageId": strconv.FormatInt(message.MessageID, 10),
					"userId":        strconv.FormatInt(message.From.ID, 10),
					"username":      message.From.Username,
					"firstName":     message.From.FirstName,
					"postMessageId": strconv.FormatInt(message.ReplyToMessage.MessageID, 10),
				}
				userSentMessageInPostList = append(userSentMessageInPostList, userSentMessageInPost)
				sendDebugMessage(message.Chat.ID, "User is not a group member, user message id is "+strconv.FormatInt(message.MessageID, 10))
				botQuestionMessageId := sendBotVerificationQuestionMessage(message.Chat.ID, message.MessageID)
				if botQuestionMessageId != 0 {
					go startDeleteTimer(message.Chat.ID, message.MessageID, botQuestionMessageId)
				}
			}
		}
	}
}

func handleCallbackQuery(callbackQuery *models.CallbackQuery, botQuestionMessageId int64) {
	answer := callbackQuery.Data
	var userMessageId int64 = callbackQuery.Message.ReplyToMessage.MessageID
	var userMessageIdString string = strconv.FormatInt(userMessageId, 10)
	var userId string
	var userIdInt int64
	var username string
	var firstName string
	var postMessageId string
	var postMessageIdInt int64

	// find userSentMessageInPost in userSentMessageInPostList
	for _, userSentMessageInPost := range userSentMessageInPostList {
		if userSentMessageInPost["userMessageId"] == userMessageIdString {
			userId = userSentMessageInPost["userId"]
			username = userSentMessageInPost["username"]
			firstName = userSentMessageInPost["firstName"]
			postMessageId = userSentMessageInPost["postMessageId"]

			break
		}
	}

	userIdInt, _ = strconv.ParseInt(userId, 10, 64)

	postMessageIdInt, _ = strconv.ParseInt(postMessageId, 10, 64)

	var deletionText string = "Message was sent by non group member. User ID is " + userId + " user name is \"" + firstName + "\" username is @" + username

	sendDebugMessage(callbackQuery.Message.Chat.ID, "Prepared deletion text: "+deletionText)

	// check if callbackQuery user id is the same as user id in userSentMessageInPost
	if callbackQuery.From.ID != userIdInt {
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Callback query user id is not the same as user id in userSentMessageInPost, ignoring.")
		return
	}

	// Stop the timer if it exists
	if timer, ok := deleteTimers.Load(userMessageId); ok {
		timer.(*time.Timer).Stop()
		deleteTimers.Delete(userMessageId)
	}

	sendDebugMessage(callbackQuery.Message.Chat.ID, fmt.Sprintf("Received callback query: %s", answer))

	if botQuestionId, ok := sentOwnBotQuestionIds.Load(userMessageId); ok {
		// delete bot question message
		deleteMessage(callbackQuery.Message.Chat.ID, botQuestionId.(int64))
		sentOwnBotQuestionIds.Delete(userMessageId)
	}

	// get needed sum from userNeededAnswersList
	neededSum, ok := userNeededAnswersList.Load(userMessageId)
	if !ok {
		log.Printf("Error getting needed sum from userNeededAnswersList")
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Error getting needed sum from userNeededAnswersList")
		return
	}

	var neededAnswerString string = strconv.Itoa(neededSum.(int))

	if answer == neededAnswerString {
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Correct answer received")
		deleteMessage(callbackQuery.Message.Chat.ID, botQuestionMessageId)
		userNeededAnswersList.Delete(userMessageId)
	} else {
		sendDebugMessage(callbackQuery.Message.Chat.ID, "Wrong answer received, deleting message.")
		deleteMessage(callbackQuery.Message.Chat.ID, userMessageId)
		// send report message in reply to post that message was sent by non group member, user id, username and first name
		sendDebugMessage(callbackQuery.Message.Chat.ID, "After user answered wrong, after deleting their message, sending message in reply to post with report text.")
		// sendMessageInReplyToPost(callbackQuery.Message.Chat.ID, deletionText, postMessageIdInt)

		res, err := sendMessage(callbackQuery.Message.Chat.ID, postMessageIdInt, deletionText)

		if err != nil {
			log.Printf("Error sending message: %v", err)
			sendDebugMessage(callbackQuery.Message.Chat.ID, "Error sending message")
			return
		}

		if res == 0 {
			log.Printf("Error sending message: %v", err)
			sendDebugMessage(callbackQuery.Message.Chat.ID, "Error sending message")
			return
		}

		// delete userSentMessageInPost from userSentMessageInPostList
		for i, userSentMessageInPost := range userSentMessageInPostList {
			if userSentMessageInPost["userMessageId"] == userMessageIdString {
				userSentMessageInPostList = append(userSentMessageInPostList[:i], userSentMessageInPostList[i+1:]...)

				break
			}
		}

	}
}

func sendBotVerificationQuestionMessage(chatId int64, messageId int64) int64 {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	// generate two numbers between 1 and 10
	num1 := rand.Intn(10) + 1
	num2 := rand.Intn(10) + 1
	neededSum := num1 + num2

	userNeededAnswersList.Store(messageId, neededSum)

	text := fmt.Sprintf("Are you a spammer? If not, solve %d plus %d.", num1, num2)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d&reply_markup=%s",
		token, chatId, text, messageId, generateInlineKeyboardMarkup(neededSum))

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

func generateInlineKeyboardMarkup(neededAnswer int) string {
	// generate number in range 0 to 1 (inclusive) in order dynamically put buttons
	randomNumber := rand.Intn(2)

	// generate random number in range 1 to 10 (inclusive) in order display spoofed answer
	spoofedAnswer := rand.Intn(10) + 1
	var spoofedAnswerString string = strconv.Itoa(spoofedAnswer)

	var neededAnswerString string = strconv.Itoa(neededAnswer)

	buttons0 := [][]map[string]string{
		{
			{"text": neededAnswerString, "callback_data": neededAnswerString},
			{"text": spoofedAnswerString, "callback_data": spoofedAnswerString},
		},
	}

	buttons1 := [][]map[string]string{
		{
			{"text": spoofedAnswerString, "callback_data": spoofedAnswerString},
			{"text": neededAnswerString, "callback_data": neededAnswerString},
		},
	}

	var replyMarkup map[string][][]map[string]string
	if randomNumber == 0 {
		replyMarkup = map[string][][]map[string]string{"inline_keyboard": buttons0}
	} else {
		replyMarkup = map[string][][]map[string]string{"inline_keyboard": buttons1}
	}

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
	userNeededAnswersList.Delete(userMessageId)

	sendDebugMessage(chatId, "after timer, after deleting messages, sending message in reply to post with report text.")

	var userMessageIdString string = strconv.FormatInt(userMessageId, 10)
	for _, userSentMessageInPost := range userSentMessageInPostList {
		if userSentMessageInPost["userMessageId"] == userMessageIdString {
			userId := userSentMessageInPost["userId"]
			username := userSentMessageInPost["username"]
			firstName := userSentMessageInPost["firstName"]
			postMessageId := userSentMessageInPost["postMessageId"]
			postMessageIdInt, _ := strconv.ParseInt(postMessageId, 10, 64)

			var deletionText string = "Message was sent by non group member. User ID is " + userId + " user name is \"" + firstName + "\" username is @" + username

			sendDebugMessage(chatId, "report text: "+deletionText)

			res, err := sendMessage(chatId, postMessageIdInt, deletionText)

			if err != nil {
				log.Printf("Error sending message: %v", err)
				sendDebugMessage(chatId, "Error sending message")
				return
			}

			if res == 0 {
				log.Printf("Error sending message: %v", err)
				sendDebugMessage(chatId, "Error sending message")
				return
			}

			break
		}

	}

	// delete userSentMessageInPost from userSentMessageInPostList
	for i, userSentMessageInPost := range userSentMessageInPostList {
		if userSentMessageInPost["userMessageId"] == userMessageIdString {
			userSentMessageInPostList = append(userSentMessageInPostList[:i], userSentMessageInPostList[i+1:]...)

			break
		}
	}
}

func sendMessage(chatId int64, messageId int64, text string) (int64, error) {
	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	sendDebugMessage(chatId, "Trying to send message. Text: "+text)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s&reply_to_message_id=%d",
		token, chatId, text, messageId)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending message: %v", err)
		sendDebugMessage(chatId, "Error sending message")
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v", err)
		sendDebugMessage(chatId, "Error reading send message response")
		return 0, err
	}

	log.Printf("Send message response: %s", string(body))
	sendDebugMessage(chatId, fmt.Sprintf("Send message response: %s", string(body)))

	return 1, nil
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
