//
// ephemeral.go
// Antidubl — Bot API 10.3 ephemeral messages
//
// Ephemeral messages are visible only to a specific user and the
// bot ("private on the public timeline"). The v5.5.1 library knows
// nothing about them, so they are sent/edited via raw MakeRequest
// with a JSON-serialized ephemeral_message_parameters object — the
// same pattern already used for restrictChatMember permissions.
//
// A bot that is an administrator of the chat may send an ephemeral
// message to any non-bot member at any time (no 15-second callback
// window), so the duplicate notices and the settings menu qualify.
// Delivery is not guaranteed when the user is offline; ephemeral
// messages expire on their own, so they are not registered for
// auto-deletion.
//

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ephemeralResult is the part of the sendMessage response that
// matters for ephemeral messages (message_id is 0 for them).
type ephemeralResult struct {
	EphemeralMessageID int64 `json:"ephemeral_message_id"`
}

// ephemeralParams builds the JSON-serialized
// ephemeral_message_parameters value for sendMessage.
func ephemeralParams(userID int64) string {
	return fmt.Sprintf(`{"receiver_user_id":%d}`, userID)
}

// sendEphemeral sends a message visible only to userID in the chat.
// Returns the ephemeral_message_id (for later edit/delete) or 0
// when the API did not return one.
func (d *Detector) sendEphemeral(chatID, userID int64, text string, kb *tgbotapi.InlineKeyboardMarkup) int64 {
	params := tgbotapi.Params{
		"chat_id":                      strconv.FormatInt(chatID, 10),
		"text":                         text,
		"ephemeral_message_parameters": ephemeralParams(userID),
	}
	if kb != nil {
		raw, err := json.Marshal(kb)
		if err != nil {
			log.Printf("[ephemeral] сериализация клавиатуры: %v", err)
			return 0
		}
		params["reply_markup"] = string(raw)
	}
	resp, err := d.bot.MakeRequest("sendMessage", params)
	if err != nil {
		log.Printf("[ephemeral] sendMessage (пользователь %d, чат %d): %v", userID, chatID, err)
		return 0
	}
	var r ephemeralResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		log.Printf("[ephemeral] разбор ответа: %v", err)
		return 0
	}
	if r.EphemeralMessageID == 0 {
		log.Printf("[ephemeral] ответ без ephemeral_message_id (чат %d, пользователь %d)", chatID, userID)
	}
	return r.EphemeralMessageID
}

// editEphemeralMenu re-renders an ephemeral message (settings menu).
func (d *Detector) editEphemeralMenu(chatID, userID, ephemeralID int64, text string, kb tgbotapi.InlineKeyboardMarkup) error {
	params := tgbotapi.Params{
		"chat_id":              strconv.FormatInt(chatID, 10),
		"receiver_user_id":     strconv.FormatInt(userID, 10),
		"ephemeral_message_id": strconv.FormatInt(ephemeralID, 10),
		"text":                 text,
	}
	raw, err := json.Marshal(kb)
	if err != nil {
		return fmt.Errorf("сериализация клавиатуры: %w", err)
	}
	params["reply_markup"] = string(raw)
	_, err = d.bot.MakeRequest("editEphemeralMessageText", params)
	if err != nil {
		return fmt.Errorf("editEphemeralMessageText: %w", err)
	}
	return nil
}

// deleteEphemeralMenu removes an ephemeral message (menu "Закрыть").
func (d *Detector) deleteEphemeralMenu(chatID, userID, ephemeralID int64) error {
	_, err := d.bot.MakeRequest("deleteEphemeralMessage", tgbotapi.Params{
		"chat_id":              strconv.FormatInt(chatID, 10),
		"receiver_user_id":     strconv.FormatInt(userID, 10),
		"ephemeral_message_id": strconv.FormatInt(ephemeralID, 10),
	})
	if err != nil {
		return fmt.Errorf("deleteEphemeralMessage: %w", err)
	}
	return nil
}
