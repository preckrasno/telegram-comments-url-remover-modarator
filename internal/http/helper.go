// internal/http/helper.go

package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"telegram_moderator/internal/config"
	"telegram_moderator/pkg/types"
)

func sendDebugMessage(chatId int64, text string) {
	var chatIdString string = strconv.FormatInt(chatId, 10)
	var debugChatId string = config.GetEnv("DEBUG_CHAT_ID", "default")

	var updatedText string = "Debug message: " + text

	if !debugRepliesInChat {
		return
	}

	if debugChatId != chatIdString {
		return
	}

	// var updatedText string = text + " (debug chat id: " + debugChatId + " chat id: " + chatIdString + ")"

	token := config.GetEnv("TELEGRAM_BOT_API_TOKEN", "default")
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage?chat_id=%d&text=%s", token, chatId, updatedText)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error sending debug message: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading debug message response: %v", err)
		return
	}

	log.Printf("Debug message response: %s", string(body))
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
