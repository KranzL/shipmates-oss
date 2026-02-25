package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/bot/config"
	"github.com/KranzL/shipmates-oss/bot/conversation"
	"github.com/KranzL/shipmates-oss/bot/delegation"
	"github.com/KranzL/shipmates-oss/bot/discord"
	"github.com/KranzL/shipmates-oss/bot/issue_pipeline"
	"github.com/KranzL/shipmates-oss/bot/orchestrator"
	"github.com/KranzL/shipmates-oss/bot/scheduler"
	"github.com/KranzL/shipmates-oss/bot/slack"
	"github.com/KranzL/shipmates-oss/bot/voice"
)

type healthCache struct {
	mu         sync.RWMutex
	status     string
	lastCheck  time.Time
	cacheValid bool
}

var dbHealthCache = &healthCache{}

type workspaceChecker struct {
	db *goshared.DB
}

func (w *workspaceChecker) WorkspaceExists(ctx context.Context, platform, platformID string) (bool, error) {
	workspace, err := w.db.GetWorkspaceByPlatform(ctx, goshared.Platform(platform), platformID)
	if err != nil {
		return false, err
	}
	return workspace != nil, nil
}

type messageHandlerAdapter struct {
	manager *conversation.Manager
}

func (m *messageHandlerAdapter) HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn discord.LogFunc, threadID string) (string, error) {
	return m.manager.HandleMessage(ctx, text, workspaceID, userID, platform, logFn, threadID)
}

type slackMessageHandlerAdapter struct {
	manager *conversation.Manager
}

func (m *slackMessageHandlerAdapter) HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn slack.LogFunc, threadID string) (string, error) {
	return m.manager.HandleMessage(ctx, text, workspaceID, userID, platform, logFn, threadID)
}

func maskSensitiveURL(dbURL string) string {
	re := regexp.MustCompile(`(postgres(?:ql)?://[^:]+:)([^@]+)(@.+)`)
	return re.ReplaceAllString(dbURL, "${1}***${3}")
}

func buildOrchRequest(userID, agentID, task, repo, provider, apiKey, githubToken, baseURL, modelID, platformUserID string, agentScope interface{}) orchestrator.WorkRequest {
	return orchestrator.WorkRequest{
		UserID:         userID,
		AgentID:        agentID,
		Task:           task,
		Repo:           repo,
		Provider:       provider,
		APIKey:         apiKey,
		GithubToken:    githubToken,
		AgentScope:     agentScope,
		BaseURL:        baseURL,
		ModelID:        modelID,
		PlatformUserID: platformUserID,
	}
}

func main() {
	log.Println("shipmates bot service starting")

	databaseURL := os.Getenv("DATABASE_URL")
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	relayURL := os.Getenv("RELAY_URL")
	if relayURL == "" {
		relayURL = "http://localhost:8080"
		log.Println("WARNING: RELAY_URL not set, defaulting to http://localhost:8080 (local dev only)")
	}
	relaySecret := os.Getenv("RELAY_SECRET")
	discordToken := os.Getenv("DISCORD_TOKEN")
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	slackAppToken := os.Getenv("SLACK_APP_TOKEN")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	if encryptionKey == "" {
		log.Fatal("ENCRYPTION_KEY is required")
	}
	if relaySecret == "" {
		log.Fatal("RELAY_SECRET is required")
	}

	startDB := time.Now()
	log.Println("connecting to database")
	db, err := goshared.NewDB(databaseURL)
	if err != nil {
		maskedURL := maskSensitiveURL(databaseURL)
		log.Fatalf("failed to connect to database %s: %v", maskedURL, err)
	}
	log.Printf("database connected (took %dms)", time.Since(startDB).Milliseconds())

	configCtx, configCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := config.Load(configCtx, db); err != nil {
		configCancel()
		db.Close()
		log.Fatalf("failed to load config: %v", err)
	}
	configCancel()

	startOrch := time.Now()
	log.Println("initializing orchestrator")
	orch, err := orchestrator.NewOrchestrator(db)
	if err != nil {
		db.Close()
		log.Fatalf("failed to create orchestrator: %v", err)
	}
	log.Printf("orchestrator initialized (took %dms)", time.Since(startOrch).Milliseconds())

	executeFn := func(ctx context.Context, req scheduler.WorkRequest) error {
		orchReq := buildOrchRequest(
			req.UserID, req.AgentID, req.Task, req.Repo, req.Provider,
			req.APIKey, req.GithubToken, req.BaseURL, req.ModelID, "", req.AgentScope,
		)
		return orch.ExecuteInContainer(ctx, orchReq)
	}

	pipelineExecuteFn := func(ctx context.Context, req issue_pipeline.WorkRequest) error {
		orchReq := buildOrchRequest(
			req.UserID, req.AgentID, req.Task, req.Repo, req.Provider,
			req.APIKey, req.GithubToken, req.BaseURL, req.ModelID, "", req.AgentScope,
		)
		return orch.ExecuteInContainer(ctx, orchReq)
	}

	delegationExecuteFn := func(ctx context.Context, req delegation.WorkRequest) error {
		orchReq := buildOrchRequest(
			req.UserID, req.AgentID, req.Task, req.Repo, req.Provider,
			req.APIKey, req.GithubToken, req.BaseURL, req.ModelID, req.PlatformUserID, req.AgentScope,
		)
		return orch.ExecuteInContainer(ctx, orchReq)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startSched := time.Now()
	log.Println("starting scheduler")
	sched := scheduler.NewScheduler(db, executeFn)
	sched.Start(ctx)
	log.Printf("scheduler started (took %dms)", time.Since(startSched).Milliseconds())

	startPipeline := time.Now()
	log.Println("starting issue pipeline")
	pipeline := issue_pipeline.NewPipelineService(db, pipelineExecuteFn)
	pipeline.Start(ctx)
	log.Printf("issue pipeline started (took %dms)", time.Since(startPipeline).Milliseconds())

	startWorker := time.Now()
	log.Println("starting delegation worker")
	worker := delegation.NewWorker(db, delegationExecuteFn)
	worker.Start(ctx)
	log.Printf("delegation worker started (took %dms)", time.Since(startWorker).Milliseconds())

	var conversationMgr *conversation.Manager
	var discordBot *discord.Bot
	var slackBot *slack.Bot
	var voiceHandler *voice.Handler
	if discordToken != "" {
		startDiscord := time.Now()
		log.Println("initializing Discord bot")

		discordClientID := os.Getenv("DISCORD_CLIENT_ID")
		if discordClientID == "" {
			log.Println("WARNING: DISCORD_CLIENT_ID not set, command registration will be skipped")
		}

		conversationMgr = conversation.NewManager(db)
		checker := &workspaceChecker{db: db}
		msgHandler := &messageHandlerAdapter{manager: conversationMgr}

		var speechHandler discord.SpeechHandler
		deepgramKey := os.Getenv("DEEPGRAM_API_KEY")
		elevenlabsKey := os.Getenv("ELEVENLABS_API_KEY")
		if deepgramKey != "" && elevenlabsKey != "" {
			sttClient, err := voice.NewSTTClient(deepgramKey)
			if err != nil {
				log.Printf("WARNING: failed to create STT client: %v", err)
			} else {
				ttsClient, err := voice.NewTTSClient(elevenlabsKey)
				if err != nil {
					log.Printf("WARNING: failed to create TTS client: %v", err)
					sttClient.Close()
				} else {
					vh, err := voice.NewHandler(sttClient, ttsClient, db, msgHandler)
					if err != nil {
						log.Printf("WARNING: failed to create voice handler: %v", err)
						sttClient.Close()
						ttsClient.Close()
					} else {
						voiceHandler = vh
						speechHandler = vh
						log.Println("voice support enabled (Deepgram + ElevenLabs)")
					}
				}
			}
		} else {
			log.Println("voice support disabled (missing API keys)")
		}

		botCfg := discord.BotConfig{
			Token:            discordToken,
			ClientID:         discordClientID,
			MessageHandler:   msgHandler,
			SpeechHandler:    speechHandler,
			WorkspaceChecker: checker,
		}

		var err error
		discordBot, err = discord.NewBot(ctx, botCfg)
		if err != nil {
			log.Fatalf("failed to create Discord bot: %v", err)
		}

		err = discordBot.Open()
		if err != nil {
			log.Fatalf("failed to connect Discord bot: %v", err)
		}

		if discordClientID != "" {
			err = discordBot.RegisterCommands()
			if err != nil {
				log.Printf("WARNING: failed to register Discord commands: %v", err)
			} else {
				log.Println("Discord slash commands registered")
			}
		}

		log.Printf("Discord bot connected (took %dms)", time.Since(startDiscord).Milliseconds())
	}

	if slackBotToken != "" && slackAppToken != "" {
		startSlack := time.Now()
		log.Println("initializing Slack bot")

		if conversationMgr == nil {
			conversationMgr = conversation.NewManager(db)
		}
		checker := &workspaceChecker{db: db}
		slackMsgHandler := &slackMessageHandlerAdapter{manager: conversationMgr}

		slackCfg := slack.BotConfig{
			BotToken:         slackBotToken,
			AppToken:         slackAppToken,
			MessageHandler:   slackMsgHandler,
			WorkspaceChecker: checker,
		}

		var err error
		slackBot, err = slack.NewBot(ctx, slackCfg)
		if err != nil {
			log.Printf("WARNING: failed to create Slack bot: %v", err)
		} else {
			go func() {
				if err := slackBot.Run(ctx); err != nil {
					log.Printf("Slack bot error: %v", err)
				}
			}()
			log.Printf("Slack bot connected (took %dms)", time.Since(startSlack).Milliseconds())
		}
	} else {
		log.Println("Slack bot disabled (missing SLACK_BOT_TOKEN or SLACK_APP_TOKEN)")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		dbHealthCache.mu.RLock()
		cacheValid := dbHealthCache.cacheValid && time.Since(dbHealthCache.lastCheck) < 5*time.Second
		cachedStatus := dbHealthCache.status
		dbHealthCache.mu.RUnlock()

		dbStatus := "ok"
		if cacheValid {
			dbStatus = cachedStatus
		} else {
			healthCtx, healthCancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer healthCancel()

			if err := db.Health(healthCtx); err != nil {
				dbStatus = "unhealthy"
			}

			dbHealthCache.mu.Lock()
			dbHealthCache.status = dbStatus
			dbHealthCache.lastCheck = time.Now()
			dbHealthCache.cacheValid = true
			dbHealthCache.mu.Unlock()
		}

		overallStatus := "ok"
		if dbStatus != "ok" {
			overallStatus = "degraded"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  overallStatus,
			"service": "bot",
			"components": map[string]string{
				"database": dbStatus,
			},
		})
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("health check listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server error: %v", err)
		}
	}()

	log.Println("bot service ready")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigChan
	log.Printf("received signal %s, shutting down", sig)
	signal.Stop(sigChan)

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)

		if discordBot != nil {
			discordBot.SetShuttingDown()
		}

		cancel()

		if discordBot != nil {
			log.Println("closing Discord bot")
			if err := discordBot.Close(); err != nil {
				log.Printf("Discord bot close error: %v", err)
			}
		}

		if slackBot != nil {
			log.Println("closing Slack bot")
			slackBot.Close()
		}

		if voiceHandler != nil {
			log.Println("closing voice handler")
			voiceHandler.Close()
		}

		if conversationMgr != nil {
			log.Println("closing conversation manager")
			conversationMgr.Close()
		}

		httpCtx, httpCancel := context.WithTimeout(context.Background(), 15*time.Second)
		log.Println("shutting down HTTP server")
		if err := server.Shutdown(httpCtx); err != nil {
			log.Printf("health server shutdown error: %v", err)
		}
		httpCancel()

		workerDone := make(chan struct{})
		go func() {
			log.Println("stopping delegation worker")
			worker.Stop()
			close(workerDone)
		}()
		select {
		case <-workerDone:
		case <-time.After(15 * time.Second):
			log.Println("delegation worker shutdown timed out after 15s")
		}

		pipelineDone := make(chan struct{})
		go func() {
			log.Println("stopping issue pipeline")
			pipeline.Stop()
			close(pipelineDone)
		}()
		select {
		case <-pipelineDone:
		case <-time.After(15 * time.Second):
			log.Println("issue pipeline shutdown timed out after 15s")
		}

		schedDone := make(chan struct{})
		go func() {
			log.Println("stopping scheduler")
			sched.Stop()
			close(schedDone)
		}()
		select {
		case <-schedDone:
		case <-time.After(15 * time.Second):
			log.Println("scheduler shutdown timed out after 15s")
		}

		log.Println("closing orchestrator")
		orch.Close()
		log.Println("closing database")
		db.Close()
	}()

	select {
	case <-shutdownDone:
		log.Println("shutdown complete")
	case <-time.After(60 * time.Second):
		log.Println("shutdown timed out after 60s, forcing exit")
		os.Exit(1)
	}
}
