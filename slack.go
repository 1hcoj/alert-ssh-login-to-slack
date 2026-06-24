package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type slackNotifier struct {
	token     string
	channelID string
	endpoint  string
	client    *http.Client
}

type slackMessage struct {
	Channel string       `json:"channel"`
	Text    string       `json:"text"`
	Blocks  []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type   string      `json:"type"`
	Text   *slackText  `json:"text,omitempty"`
	Fields []slackText `json:"fields,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackAPIResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

func newSlackNotifier(token, channelID string) *slackNotifier {
	return &slackNotifier{
		token:     token,
		channelID: channelID,
		endpoint:  "https://slack.com/api/chat.postMessage",
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (notifier *slackNotifier) Notify(ctx context.Context, item login) error {
	message := slackMessage{
		Channel: notifier.channelID,
		Text:    fmt.Sprintf("SSH login: %s from %s on %s", item.Username, item.ClientIP, item.Hostname),
		Blocks: []slackBlock{
			{
				Type: "header",
				Text: &slackText{Type: "plain_text", Text: "SSH Login Detected"},
			},
			{
				Type: "section",
				Fields: []slackText{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Host:*\n%s", item.Hostname)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Account:*\n%s (UID %d)", item.Username, item.UID)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Source:*\n%s:%s", item.ClientIP, item.ClientPort)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Server:*\n%s:%s", item.ServerIP, item.ServerPort)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Datetime:*\n%s", item.OccurredAt.Format(time.RFC3339))},
					{Type: "mrkdwn", Text: fmt.Sprintf("*sshd PID:*\n%d", item.PID)},
				},
			},
		},
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, notifier.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+notifier.token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	response, err := notifier.client.Do(request)
	if err != nil {
		return fmt.Errorf("call chat.postMessage: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Slack response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Slack returned HTTP %s: %s", response.Status, string(responseBody))
	}

	var result slackAPIResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return fmt.Errorf("decode Slack response: %w", err)
	}
	if !result.OK {
		if result.Error == "" {
			result.Error = "unknown_error"
		}
		return fmt.Errorf("Slack chat.postMessage failed: %s", result.Error)
	}
	return nil
}
