package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func TestJobWebSocketSendsSnapshotAndLiveUpdate(t *testing.T) {
	natsURL := os.Getenv("TEST_NATS_URL")
	if natsURL == "" {
		t.Skip("TEST_NATS_URL is not set; run `make go-test-integration`")
	}
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect NATS: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("create JetStream client: %v", err)
	}
	if err := shared.EnsureJetStream(ctx, js); err != nil {
		t.Fatalf("ensure JetStream: %v", err)
	}
	broker, err := newJobBroker(ctx, nc, s.log)
	if err != nil {
		t.Fatalf("start job broker: %v", err)
	}
	s.ctx, s.nats, s.js, s.broker = ctx, nc, js, broker

	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)
	job := createJobForTest(t, s, project.ID)
	server := httptest.NewServer(s.newRouter(func(context.Context) error { return nil }))
	t.Cleanup(server.Close)

	token, _, err := s.mintAccessToken(user.ID, shared.NewID())
	if err != nil {
		t.Fatalf("mint access token: %v", err)
	}
	connection := dialTestWebSocket(t, server.URL)
	t.Cleanup(func() { _ = connection.CloseNow() })
	writeCtx, writeCancel := context.WithTimeout(ctx, 3*time.Second)
	defer writeCancel()
	if err := wsjson.Write(writeCtx, connection, websocketAuthRequest{
		Type: "authenticate", AccessToken: token, ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("authenticate websocket: %v", err)
	}

	var snapshot websocketEnvelope
	if err := wsjson.Read(writeCtx, connection, &snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot.Type != "snapshot" || len(snapshot.Jobs) != 1 || snapshot.Jobs[0].ID != job.ID {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}

	progress := float32(0.42)
	job.Status, job.Progress, job.Attempt = "running", &progress, 1
	job.UpdatedAt.Time = time.Now()
	shared.PublishJobEvent(nc, job, s.log)
	if err := nc.Flush(); err != nil {
		t.Fatalf("flush job event: %v", err)
	}
	var update websocketEnvelope
	if err := wsjson.Read(writeCtx, connection, &update); err != nil {
		t.Fatalf("read live update: %v", err)
	}
	if update.Type != "job_updated" || update.Job == nil ||
		update.Job.ID != job.ID || update.Job.Progress == nil || *update.Job.Progress != progress {
		t.Fatalf("unexpected live update: %+v", update)
	}
}

func TestLiveWebSocketUpgradeThroughNginx(t *testing.T) {
	endpoint := os.Getenv("TEST_LIVE_WS_URL")
	if endpoint == "" {
		t.Skip("TEST_LIVE_WS_URL is not set")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse live websocket URL: %v", err)
	}
	origin := "http://" + parsed.Host
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{origin}},
	})
	if err != nil {
		t.Fatalf("upgrade through nginx: %v", err)
	}
	defer connection.CloseNow()
	if err := wsjson.Write(ctx, connection, websocketAuthRequest{
		Type: "authenticate", AccessToken: "invalid", ProjectID: shared.NewID(),
	}); err != nil {
		t.Fatalf("write rejected auth: %v", err)
	}
	_, _, err = connection.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v, want policy violation; error=%v", websocket.CloseStatus(err), err)
	}
}

func dialTestWebSocket(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	endpoint := "ws" + strings.TrimPrefix(serverURL, "http") + "/v1/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{serverURL}},
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return connection
}
