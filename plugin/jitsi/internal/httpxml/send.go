package httpxml

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"call.zip/pkg/aoflog"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/xml"
)

// MinifyXML uses tdewolff/minify to minify XML, fixing a bug where Jicofo throws an exception during IQ/Jingle process
func MinifyXML(input string) (string, error) {
	m := minify.New() // TODO: reuse
	m.AddFunc("application/xml", xml.Minify)

	result, err := m.String("application/xml", input)
	if err != nil {
		return "", fmt.Errorf("minify error: %w", err)
	}

	return result, nil
}

type BOSHSender struct {
	aof aoflog.Client
}

func NewBOSHSender(aof aoflog.Client) BOSHSender {
	return BOSHSender{aof}
}

// Send sends an XMPP BOSH XML minified body to a BOSH endpoint
// boshURL: e.g. "https://10.99.0.210/http-bind?room=asdf"
// xmlBody: your <body>...</body> string
// Returns: response XML as string or error
func (s *BOSHSender) Send(boshURL string, xmlBody string) (string, error) {
	xmlBody, err := MinifyXML(xmlBody)
	if err != nil {
		return "", fmt.Errorf("minify failed: %w", err)
	}

	var callerFuncName string
	if s.aof != nil {
		pc, _, _, ok := runtime.Caller(1)
		callerFuncName = "unknown"
		if ok {
			callerFuncName = runtime.FuncForPC(pc).Name()
		}
	}

	if s.aof != nil {
		s.aof.Printf("===== [DEBUG(%s)] SENT =====\n", callerFuncName)
		s.aof.Println(time.Now().Format(time.RFC3339Nano))
		s.aof.Println(xmlBody)
		s.aof.Println("========================")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("POST", boshURL, bytes.NewBufferString(xmlBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("performing POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-200 status: %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if s.aof != nil {
		s.aof.Printf("===== [DEBUG(%s)] RECEIVED =====\n", callerFuncName)
		s.aof.Println(time.Now().Format(time.RFC3339Nano))
		s.aof.Println(string(bodyBytes))
		s.aof.Println("============================")
	}

	return string(bodyBytes), nil
}
