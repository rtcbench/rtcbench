package bench

import (
	"context"
	"strings"
	"testing"
)

type janusRemote struct {
	replies  []string
	commands []string
}

func (r *janusRemote) Run(_ context.Context, _ string, command string) (string, error) {
	r.commands = append(r.commands, command)
	if len(r.replies) == 0 {
		return `{"janus":"success"}`, nil
	}
	reply := r.replies[0]
	r.replies = r.replies[1:]
	return reply, nil
}
func (*janusRemote) Write(context.Context, string, string, []byte) error { panic("unexpected write") }

func TestJanusRequiresConfirmedRoomCreation(t *testing.T) {
	for _, tc := range []struct {
		reply string
		fail  bool
	}{
		{`{"janus":"success","plugindata":{"data":{"videoroom":"created"}}}`, false},
		{`{"janus":"success","plugindata":{"data":{"error_code":427,"error":"exists"}}}`, false},
		{`{"janus":"error","error":{"reason":"unauthorized"}}`, true},
		{`{"janus":"success"}`, true},
		{
			`{"janus":"success","plugindata":{"data":{"error_code":426,"error":"room failed"}}}`,
			true,
		},
	} {
		remote := &janusRemote{replies: []string{
			`{"janus":"success","data":{"id":9007199254740993}}`,
			`{"janus":"success","data":{"id":2}}`,
			tc.reply,
		}}
		err := prepareRooms(context.Background(), remote, "server", 8088, 2)
		if (err != nil) != tc.fail {
			t.Fatalf("%s: %v", tc.reply, err)
		}
		if !strings.Contains(remote.commands[1], "/9007199254740993") {
			t.Fatal("session ID lost precision")
		}
		if !strings.Contains(remote.commands[len(remote.commands)-1], "destroy") {
			t.Fatal("session leaked")
		}
	}
}
