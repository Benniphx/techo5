package api

import (
	"strings"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// A device without microphones (the Skylight Calendar) cannot hear, so nothing that is about
// listening is offered to Home Assistant there: no voice assistant (Home Assistant would make an
// assist satellite with its pipeline, wake word and end-of-speech selects), and none of the
// device's own entities for the microphones, wake words, wake clips, the room's sound, the mute,
// calls and the intercom (which need a microphone to talk back) or recordings of turns. What it
// says still reaches it: tts.speak to its media player, alarms, timers and everything on the screen.
// The features keep running; their entities are only not listed, so their updates go nowhere.

// listeningObjects are those entities, by object id, and listeningPrefixes the families of them
// numbered per assistant.
var listeningObjects = map[string]bool{
	// feature/microphone
	"microphone_mixing": true, "microphone_leveling": true, "microphone_noise_reduction": true,
	"microphone_cancel_echo": true, "microphone_echo_canceller": true, "microphone_end_of_speech": true,
	"microphone_gain": true, "microphone_sensitivity": true,
	// feature/detect
	"confirm_threshold": true, "confirm_wake_with_home_assistant": true, "wake_word_on_home_assistant": true,
	"keep_wake_clips": true, "last_wake_score": true, "stop_word_sensitivity": true,
	"wake_clips": true, "wake_clips_by_kind": true,
	// feature/activity
	"last_heard": true, "last_reply": true, "last_wake_word": true,
	// feature/room and diag's room readings
	"room_reaction": true, "room_level": true, "room_floor": true,
	// feature/mute
	"mic_mute": true, "mute_led_brightness": true, "ring_muted": true,
	// feature/wakeword
	"cancel_request": true,
	// feature/phone: calls and the intercom
	"phone": true, "phone_answer": true, "phone_hangup": true, "phone_peer": true, "phone_ring_sound": true,
	"intercom_drop_in": true, "intercom_do_not_disturb": true, "screen_call_button": true,
}

// listeningActions are the actions that need a microphone: signing the phone in, and calling.
var listeningActions = map[string]bool{
	"phone_account": true, "phone_call": true, "phone_contacts": true, "intercom_call": true,
	"phone_answer": true, "phone_hangup": true,
}

var listeningPrefixes = []string{
	"wake_assistant_", "wake_threshold_", "wake_tone_", "wake_effect_", "thinking_effect_",
	"replying_effect_", "reply_delivery_", "reply_buffer_", "keep_recordings_",
}

// listening is whether an entity is about listening.
func listening(object string) bool {
	if listeningObjects[object] {
		return true
	}
	for _, p := range listeningPrefixes {
		if strings.HasPrefix(object, p) {
			return true
		}
	}
	return false
}

// heard is the entities to list: all of them on a device that can hear, those not about listening
// on one that cannot.
func heard(ents []esphome.Entity) []esphome.Entity {
	if layout.HasMicrophone() {
		return ents
	}
	return dropListening(ents)
}

func dropListening(ents []esphome.Entity) []esphome.Entity {
	out := ents[:0:0]
	for _, e := range ents {
		if !listening(e.Object()) {
			out = append(out, e)
		}
	}
	return out
}

// heardActions is the actions to offer: all of them on a device that can hear, those not about
// calls on one that cannot.
func heardActions(acts []*esphome.Action) []*esphome.Action {
	if layout.HasMicrophone() {
		return acts
	}
	return dropCallActions(acts)
}

func dropCallActions(acts []*esphome.Action) []*esphome.Action {
	out := acts[:0:0]
	for _, a := range acts {
		if !listeningActions[a.Name] {
			out = append(out, a)
		}
	}
	return out
}

// voiceFeatures is what the device says it can do as a voice assistant: nothing, where it cannot
// hear.
func voiceFeatures() esphome.VoiceFeature {
	if !layout.HasMicrophone() {
		return 0
	}
	return voice.Features
}
