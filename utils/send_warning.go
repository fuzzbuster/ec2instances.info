package utils

import (
	"fmt"
	"github.com/imroc/req/v3"
	"log"
	"os"
)

var slackWebhookUrl = os.Getenv("SLACK_WEBHOOK_URL")
var slackHTTPClient = req.C().DisableAutoDecode()

func sendSlackMessage(message string) {
	jsonData := map[string]string{
		"text": message,
	}
	response, err := slackHTTPClient.R().
		SetBodyJsonMarshal(jsonData).
		Post(slackWebhookUrl)
	if err != nil {
		log.Default().Println("Error sending Slack message:", err)
		return
	}
	if response.StatusCode != 200 {
		log.Default().Println("Error sending Slack message:", response.Status)
		return
	}
}

// SendWarning sends a warning to the console, and if a webhook is set,
// to Slack.
func SendWarning(warningData ...any) {
	warningData = append([]any{"WARNING:"}, warningData...)
	x := fmt.Sprintln(warningData...)
	log.Default().Println(x)

	if slackWebhookUrl != "" {
		sendSlackMessage(x)
	}
}
