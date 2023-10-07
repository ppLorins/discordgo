package discordgo

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ////////////////////////////////////////////////////////////////////////////
// //////////////////////////////////////////////////// VARS NEEDED FOR TESTING
var (
	dg    *Session // Stores a global discordgo user session
	dgBot *Session // Stores a global discordgo bot session

	envOAuth2Token  = os.Getenv("DG_OAUTH2_TOKEN")  // Token to use when authenticating using OAuth2 token
	envBotToken     = os.Getenv("DGB_TOKEN")        // Token to use when authenticating the bot account
	envGuild        = os.Getenv("DG_GUILD")         // Guild ID to use for tests
	envChannel      = os.Getenv("DG_CHANNEL")       // Channel ID to use for tests
	envVoiceChannel = os.Getenv("DG_VOICE_CHANNEL") // Channel ID to use for tests
	envAdmin        = os.Getenv("DG_ADMIN")         // User ID of admin user to use for tests
)

func TestMain(m *testing.M) {
	fmt.Println("Init is being called.")
	if envBotToken != "" {
		if d, err := New(envBotToken); err == nil {
			dgBot = d
		}
	}

	if envOAuth2Token == "" {
		envOAuth2Token = os.Getenv("DGU_TOKEN")
	}

	if envOAuth2Token != "" {
		if d, err := New(envOAuth2Token); err == nil {
			dg = d
		}
	}

	os.Exit(m.Run())
}

//////////////////////////////////////////////////////////////////////////////
/////////////////////////////////////////////////////////////// START OF TESTS

// TestNewToken tests the New() function with a Token.
func TestNewToken(t *testing.T) {

	if envOAuth2Token == "" {
		t.Skip("Skipping New(token), DGU_TOKEN not set")
	}

	d, err := New(envOAuth2Token)
	if err != nil {
		t.Fatalf("New(envToken) returned error: %+v", err)
	}

	if d == nil {
		t.Fatal("New(envToken), d is nil, should be Session{}")
	}

	if d.Token == "" {
		t.Fatal("New(envToken), d.Token is empty, should be a valid Token.")
	}
}

func TestOpenClose(t *testing.T) {
	if envOAuth2Token == "" {
		t.Skip("Skipping TestClose, DGU_TOKEN not set")
	}

	d, err := New(envOAuth2Token)
	if err != nil {
		t.Fatalf("TestClose, New(envToken) returned error: %+v", err)
	}

	if err = d.Open(); err != nil {
		t.Fatalf("TestClose, d.Open failed: %+v", err)
	}

	// We need a better way to know the session is ready for use,
	// this is totally gross.
	start := time.Now()
	for {
		d.RLock()
		if d.DataReady {
			d.RUnlock()
			break
		}
		d.RUnlock()

		if time.Since(start) > 10*time.Second {
			t.Fatal("DataReady never became true.yy")
		}
		runtime.Gosched()
	}

	// TODO find a better way
	// Add a small sleep here to make sure heartbeat and other events
	// have enough time to get fired.  Need a way to actually check
	// those events.
	time.Sleep(2 * time.Second)

	// UpdateStatus - maybe we move this into wsapi_test.go but the websocket
	// created here is needed.  This helps tests that the websocket was setup
	// and it is working.
	if err = d.UpdateGameStatus(0, time.Now().String()); err != nil {
		t.Errorf("UpdateStatus error: %+v", err)
	}

	if err = d.Close(); err != nil {
		t.Fatalf("TestClose, d.Close failed: %+v", err)
	}
}

func TestAddHandler(t *testing.T) {

	testHandlerCalled := int32(0)
	testHandler := func(s *Session, m *MessageCreate) {
		atomic.AddInt32(&testHandlerCalled, 1)
	}

	interfaceHandlerCalled := int32(0)
	interfaceHandler := func(s *Session, i interface{}) {
		atomic.AddInt32(&interfaceHandlerCalled, 1)
	}

	bogusHandlerCalled := int32(0)
	bogusHandler := func(s *Session, se *Session) {
		atomic.AddInt32(&bogusHandlerCalled, 1)
	}

	d := Session{}
	d.AddHandler(testHandler)
	d.AddHandler(testHandler)

	d.AddHandler(interfaceHandler)
	d.AddHandler(bogusHandler)

	d.handleEvent(MessageCreateEventType, &MessageCreate{})
	d.handleEvent(MessageDeleteEventType, &MessageDelete{})

	<-time.After(500 * time.Millisecond)

	// testHandler will be called twice because it was added twice.
	if atomic.LoadInt32(&testHandlerCalled) != 2 {
		t.Fatalf("testHandler was not called twice.")
	}

	// interfaceHandler will be called twice, once for each event.
	if atomic.LoadInt32(&interfaceHandlerCalled) != 2 {
		t.Fatalf("interfaceHandler was not called twice.")
	}

	if atomic.LoadInt32(&bogusHandlerCalled) != 0 {
		t.Fatalf("bogusHandler was called.")
	}
}

func TestRemoveHandler(t *testing.T) {

	testHandlerCalled := int32(0)
	testHandler := func(s *Session, m *MessageCreate) {
		atomic.AddInt32(&testHandlerCalled, 1)
	}

	d := Session{}
	r := d.AddHandler(testHandler)

	d.handleEvent(MessageCreateEventType, &MessageCreate{})

	r()

	d.handleEvent(MessageCreateEventType, &MessageCreate{})

	<-time.After(500 * time.Millisecond)

	// testHandler will be called once, as it was removed in between calls.
	if atomic.LoadInt32(&testHandlerCalled) != 1 {
		t.Fatalf("testHandler was not called once.")
	}
}

func TestScheduledEvents(t *testing.T) {
	if dgBot == nil {
		t.Skip("Skipping, dgBot not set.")
	}

	beginAt := time.Now().Add(1 * time.Hour)
	endAt := time.Now().Add(2 * time.Hour)
	event, err := dgBot.GuildScheduledEventCreate(envGuild, &GuildScheduledEventParams{
		Name:               "Test Event",
		PrivacyLevel:       GuildScheduledEventPrivacyLevelGuildOnly,
		ScheduledStartTime: &beginAt,
		ScheduledEndTime:   &endAt,
		Description:        "Awesome Test Event created on livestream",
		EntityType:         GuildScheduledEventEntityTypeExternal,
		EntityMetadata: &GuildScheduledEventEntityMetadata{
			Location: "https://discord.com",
		},
	})
	defer dgBot.GuildScheduledEventDelete(envGuild, event.ID)

	if err != nil || event.Name != "Test Event" {
		t.Fatal(err)
	}

	events, err := dgBot.GuildScheduledEvents(envGuild, true)
	if err != nil {
		t.Fatal(err)
	}

	var foundEvent *GuildScheduledEvent
	for _, e := range events {
		if e.ID == event.ID {
			foundEvent = e
			break
		}
	}
	if foundEvent.Name != event.Name {
		t.Fatal("err on GuildScheduledEvents endpoint. Missing Scheduled Event")
	}

	getEvent, err := dgBot.GuildScheduledEvent(envGuild, event.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if getEvent.Name != event.Name {
		t.Fatal("err on GuildScheduledEvent endpoint. Mismatched Scheduled Event")
	}

	eventUpdated, err := dgBot.GuildScheduledEventEdit(envGuild, event.ID, &GuildScheduledEventParams{Name: "Test Event Updated"})
	if err != nil {
		t.Fatal(err)
	}

	if eventUpdated.Name != "Test Event Updated" {
		t.Fatal("err on GuildScheduledEventUpdate endpoint. Scheduled Event Name mismatch")
	}

	// Usage of 1 and 1 is just the pseudo data with the purpose to run all branches in the function without crashes.
	// see https://github.com/bwmarrin/discordgo/pull/1032#discussion_r815438303 for more details.
	users, err := dgBot.GuildScheduledEventUsers(envGuild, event.ID, 1, true, "1", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatal("err on GuildScheduledEventUsers. Mismatch of event maybe occured")
	}

	err = dgBot.GuildScheduledEventDelete(envGuild, event.ID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestComplexScheduledEvents(t *testing.T) {
	if dgBot == nil {
		t.Skip("Skipping, dgBot not set.")
	}

	beginAt := time.Now().Add(1 * time.Hour)
	endAt := time.Now().Add(2 * time.Hour)
	event, err := dgBot.GuildScheduledEventCreate(envGuild, &GuildScheduledEventParams{
		Name:               "Test Voice Event",
		PrivacyLevel:       GuildScheduledEventPrivacyLevelGuildOnly,
		ScheduledStartTime: &beginAt,
		ScheduledEndTime:   &endAt,
		Description:        "Test event on voice channel",
		EntityType:         GuildScheduledEventEntityTypeVoice,
		ChannelID:          envVoiceChannel,
	})
	if err != nil || event.Name != "Test Voice Event" {
		t.Fatal(err)
	}
	defer dgBot.GuildScheduledEventDelete(envGuild, event.ID)

	_, err = dgBot.GuildScheduledEventEdit(envGuild, event.ID, &GuildScheduledEventParams{
		EntityType: GuildScheduledEventEntityTypeExternal,
		EntityMetadata: &GuildScheduledEventEntityMetadata{
			Location: "https://discord.com",
		},
	})

	if err != nil {
		t.Fatal("err on GuildScheduledEventEdit. Change of entity type to external failed")
	}

	_, err = dgBot.GuildScheduledEventEdit(envGuild, event.ID, &GuildScheduledEventParams{
		ChannelID:      envVoiceChannel,
		EntityType:     GuildScheduledEventEntityTypeVoice,
		EntityMetadata: nil,
	})

	if err != nil {
		t.Fatal("err on GuildScheduledEventEdit. Change of entity type to voice failed")
	}
}

func proxyByDomain(req *http.Request) (*url.URL, error) {
	pm := map[string]string{
		"gateway.discord.gg":   "socks5://localhost:1080",
		"discord.com":          "http://localhost:9080",
		"cdn.discordapp.com":   "socks5://localhost:1080",
		"media.discordapp.net": "socks5://localhost:1080",
	}
	for k, v := range pm {
		if strings.Contains(req.URL.Hostname(), k) {
			u, e := url.Parse(v)
			if e != nil {
				panic(e)
			}
			return u, nil
		}
	}

	return nil, nil
}

func defaultTransportDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return dialer.DialContext
}

func TestBug(t *testing.T) {
	token := "MTA4NzI1MzUzNzU1MTA0MDU3NA.GjiUo5.Ej6Y6CvR8J-XztobaoJ6lUwTenqPlujc2ZS0Eo"
	s, err := NewWithProxy("Bot "+token, nil)
	if err != nil {
		panic(err)
	}
	s.Dialer = &websocket.Dialer{
		Proxy:            proxyByDomain,
		HandshakeTimeout: 45 * time.Second,
	}
	CustomTransport := &http.Transport{
		Proxy: proxyByDomain,
		DialContext: defaultTransportDialContext(&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	s.Client = &http.Client{Timeout: (20 * time.Second), Transport: CustomTransport}
	s.LogLevel = 1

	// Register ready as a callback for the ready events.
	//s.AddHandler(ready)

	// Register messageCreate as a callback for the messageCreate events.
	s.AddHandler(messageCreate)

	s.AddHandler(messageUpdate)

	s.AddHandler(messageDelete)

	//s.Identify.Intents = IntentGuildMessages | IntentMessageContent
	s.Identify.Intents = IntentsAll

	err = s.Open()
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Hour)
}

func messageCreate(s *Session, m *MessageCreate) {
	fmt.Printf("message_create,content:%+v\n", m.Content)
}

func messageUpdate(s *Session, m *MessageUpdate) {
	fmt.Printf("message_update,content:%+v\n", m.Content)
}

func messageDelete(s *Session, m *MessageDelete) {
	fmt.Printf("message_delete,content:%+v\n", m.Content)
}
