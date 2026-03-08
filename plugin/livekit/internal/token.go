package internal

import (
	"time"

	"github.com/livekit/protocol/auth"
)

// MakeToken generates a LiveKit access JWT for the given room and identity.
func MakeToken(apiKey, apiSecret, room, identity string, canPublish, canSubscribe bool) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	at.SetVideoGrant(&auth.VideoGrant{
		RoomJoin:     true,
		Room:         room,
		CanPublish:   &canPublish,
		CanSubscribe: &canSubscribe,
	}).
		SetIdentity(identity).
		SetValidFor(24 * time.Hour)
	return at.ToJWT()
}
